package ui

import (
	"slices"
	"strings"
	"time"

	"komrad/config"
	"komrad/internal/glyph"
	"komrad/internal/ui/api"
	"komrad/internal/ui/column"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/legend"
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/internal/ui/tabs"
	"komrad/internal/view"
	"komrad/lib/dates"
	"komrad/lib/xsha"
	"komrad/lib/xslice"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	tea "charm.land/bubbletea/v2"
)

// statusCheckItem is one check as the listing reported it.
type statusCheckItem struct {
	providerID string
	checkID    string
	name       string
	state      pullrequestv1.StatusCheckState
	conclusion string
	passed     bool
	startedAt  time.Time
	finishedAt time.Time
	steps      []statusCheckItem
	log        *pullrequestv1.CheckLog
}

// statusCheckCommit is the checks read on the pull request's newest head.
type statusCheckCommit struct {
	headSHA     string
	firstReadAt time.Time
	items       []statusCheckItem
}

// statusChecksDeps is what the status checks screens are given at launch.
type statusChecksDeps struct {
	keys    config.Keybindings
	glyphs  glyph.Set
	clock   screen.Clock
	clients api.Clients
	top     *keys.Top
}

// statusChecksScreen lists the status checks on the newest head of the pull request opened from one agent's branch.
type statusChecksScreen struct {
	deps statusChecksDeps
	list.Cursor
	agentID           string
	openedFrom        screen.ID
	commit            statusCheckCommit
	listedPullRequest string
	listedHead        string
	listed            bool
	listing           bool
	err               error
	scope             statusCheckScope
	scopeChosen       bool
	scopeFocus        bool
	logs              statusCheckLogs
}

type statusCheckRow struct {
	item statusCheckItem
}

func (r statusCheckRow) Key() string {
	return "status-check:" + r.item.providerID
}

func (c *statusChecksScreen) Enter(in screen.Inputs, target screen.Target) screen.Request {
	if target.AgentID != "" {
		*c = statusChecksScreen{
			deps:       c.deps,
			agentID:    target.AgentID,
			openedFrom: target.OpenedFrom,
			logs:       c.logs,
		}
	}

	return screen.Cmd(c.readStatusChecksCmd(in))
}

func (c *statusChecksScreen) Update(in screen.Inputs, msg tea.Msg) screen.Request {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	return c.listActions(in).DispatchTop(c.deps.top, c, c.deps.keys, key.String())
}

func (c *statusChecksScreen) Receive(in screen.Inputs, msg tea.Msg) (screen.Request, bool) {
	listed, ok := msg.(statusChecksListedMsg)
	if !ok {
		return nil, false
	}

	return c.applyListed(in, listed), true
}

func (c *statusChecksScreen) list() keys.ScreenList[statusCheckRow] {
	return keys.ScreenList[statusCheckRow]{
		Keys:   c.deps.keys,
		Cursor: func() *list.Cursor { return &c.Cursor },
		Rows:   c.rows,
		Name:   statusCheckRowName,
		Moved:  func(screen.Inputs) screen.Request { return screen.Settle{} },
	}
}

func (c *statusChecksScreen) rows(in screen.Inputs) []statusCheckRow {
	return list.Narrowed(in.Room.Pattern, list.NarrowedParams[statusCheckRow]{
		Rows: xslice.Map(xslice.Filter(c.commit.items, c.shownScope().holds), func(item statusCheckItem) statusCheckRow {
			return statusCheckRow{item: item}
		}),
		Depth: func(statusCheckRow) int { return 0 },
		Name:  statusCheckRowName,
	})
}

func statusCheckRowName(row statusCheckRow) string {
	return row.item.name
}

func (c *statusChecksScreen) ListParams(in screen.Inputs) list.Params {
	return list.Params{
		Tally:    c.commit.tally(c.deps.clock.Now()),
		Help:     c.help(in),
		Empty:    c.emptyLine(in),
		Rows:     len(c.rows(in)),
		Snapshot: func(pane list.Pane) view.Snapshot { return c.snapshot(in, pane) },
	}
}

// statusCheckScope is which of the status checks the status checks screen lists.
type statusCheckScope int

const (
	statusCheckScopeAll statusCheckScope = iota
	statusCheckScopeNotPassing
	statusCheckScopeActive
	statusCheckScopeFailing
)

var statusCheckScopeLabels = []string{"all", "not passing", "active", "failing"}

// holds reports whether the scope lists the status check.
func (s statusCheckScope) holds(item statusCheckItem) bool {
	switch s {
	case statusCheckScopeNotPassing:
		return !item.passed
	case statusCheckScopeActive:
		return isStatusCheckInFlight(item)
	case statusCheckScopeFailing:
		return isStatusCheckFinished(item) && !item.passed
	case statusCheckScopeAll:
	}

	return true
}

func (c *statusChecksScreen) shownScope() statusCheckScope {
	switch {
	case c.scopeChosen:
		return c.scope
	case slices.ContainsFunc(c.commit.items, statusCheckScopeNotPassing.holds):
		return statusCheckScopeNotPassing
	}

	return statusCheckScopeAll
}

func (c *statusChecksScreen) Kinds() tabs.Stops {
	return tabs.Stops{
		Labels: statusCheckScopeLabels,
		Focus:  tabs.NoStop,
		Lit:    int(c.shownScope()),
	}
}

func (c *statusChecksScreen) KindsFocused() *bool {
	return &c.scopeFocus
}

func (c *statusChecksScreen) KindsSwitch() bool {
	return true
}

// StepKind moves the listing to another scope, keeping the cursor on its row when the new scope lists it.
func (c *statusChecksScreen) StepKind(in screen.Inputs, delta int) screen.Request {
	hovered := c.list().Hovered(in)
	c.scope = statusCheckScope(tabs.StepStop(int(c.shownScope()), delta, len(statusCheckScopeLabels)))
	c.scopeChosen = true

	return screen.Batch{c.list().Place(in, hovered), screen.Settle{}}
}

func (c *statusChecksScreen) listActions(in screen.Inputs) keys.Spec[*statusChecksScreen] {
	return keys.Spec[*statusChecksScreen]{
		Table: append(keys.ListActions[*statusChecksScreen](c.list(), in),
			keys.Action[*statusChecksScreen]{
				ID:   actionStatusChecksBack,
				Keys: keys.FixedBack,
				Nav: func(*statusChecksScreen, string) screen.Request {
					return screen.Open{Screen: c.openedFrom, Target: screen.Target{AgentID: c.agentID}}
				},
			},
			keys.Action[*statusChecksScreen]{
				ID:   actionStatusChecksLogs,
				Keys: keys.FixedOpen,
				Nav: func(*statusChecksScreen, string) screen.Request {
					return screen.Open{Screen: screen.StatusCheckLogs, Target: screen.Target{AgentID: c.agentID, OpenedFrom: screen.StatusChecks}}
				},
			},
		),
		Legend: slices.Concat(keys.NavLegend[*statusChecksScreen](), []keys.LegendRow[*statusChecksScreen]{
			{
				Of:   []keys.ID{keys.FilterOpen},
				Desc: keys.FilterDescOf[*statusChecksScreen](in),
			},
			{
				Of:    []keys.ID{keys.FilterClear},
				Desc:  keys.FilterClearDescOf[*statusChecksScreen](in),
				Strip: true,
			},
			{
				Of:    []keys.ID{keys.FilterMatch},
				Desc:  func(*statusChecksScreen) string { return "next/prev match" },
				Strip: true,
			},
			{
				Of:    []keys.ID{actionStatusChecksBack},
				Desc:  func(*statusChecksScreen) string { return "back" },
				Strip: true,
			},
			{
				Of:    []keys.ID{actionStatusChecksLogs},
				Desc:  func(*statusChecksScreen) string { return "logs" },
				Strip: true,
			},
		}),
	}
}

func (c *statusChecksScreen) help(in screen.Inputs) legend.Help {
	return c.listActions(in).Help(c, c.deps.keys)
}

func (c *statusChecksScreen) Parent() screen.ID {
	return c.openedFrom
}

func (c *statusChecksScreen) Crumbs(in screen.Inputs) screen.Crumbs {
	name := c.agentID
	if item, ok := in.Listing.AnyAgent(c.agentID); ok {
		name = in.Listing.AgentName(item.Agent)
	}

	return screen.Crumbs{Trail: []string{name, "status checks"}}
}

func (c *statusChecksScreen) DoesRenderFromListing() bool {
	return true
}

func (c *statusChecksScreen) TakesEveryKey() bool {
	return false
}

func (c *statusChecksScreen) Column(in screen.Inputs) column.Spec {
	return column.Spec{
		Floor:   column.Narrowest,
		DocRows: func(width int) []string { return c.docRows(in, width) },
	}
}

func (c *statusChecksScreen) StageResting(screen.Inputs) screen.Request {
	return nil
}

func (c *statusChecksScreen) Dispatches() []keys.Table {
	return []keys.Table{c.listActions(screen.Inputs{})}
}

var (
	_ kindSwitcher    = (*statusChecksScreen)(nil)
	_ activeLister    = (*statusChecksScreen)(nil)
	_ crumbRenderer   = (*statusChecksScreen)(nil)
	_ loadingReporter = (*statusChecksScreen)(nil)
)

func (c *statusChecksScreen) ActiveList() keys.KeyedList {
	return c.list()
}

// statusChecksListedMsg is the status checks on one pull request's newest head come back.
type statusChecksListedMsg struct {
	agentID       string
	pullRequestID string
	headSHA       string
	headRead      bool
	commit        statusCheckCommit
	err           error
}

// statusChecksPullRequestID is the pull request the agent's row stands for, empty when it has none.
func statusChecksPullRequestID(listing panel.Listing, agentID string) string {
	item, ok := listing.AnyAgent(agentID)
	if !ok {
		return ""
	}

	pullRequests := listing.Tasks.FindForAgent(item.Agent).PullRequests
	if len(pullRequests) == 0 {
		return ""
	}

	return pullRequests[0].ID
}

// readStatusChecksCmd lists the status checks again when the agent's pull request or its head has moved since
// they were last listed, once at a time.
func (c *statusChecksScreen) readStatusChecksCmd(in screen.Inputs) tea.Cmd {
	if !in.Up {
		return nil
	}

	pullRequestID := statusChecksPullRequestID(in.Listing, c.agentID)
	if pullRequestID == "" || c.listing {
		return nil
	}

	item, _ := in.Listing.AnyAgent(c.agentID)
	head := in.Listing.Tasks.FindForAgent(item.Agent).PullRequest.HeadSHA
	if c.listed && c.listedPullRequest == pullRequestID && c.listedHead == head {
		return nil
	}

	c.listing = true
	agentID, clients := c.agentID, c.deps.clients

	return func() tea.Msg {
		listed, err := clients.ListStatusChecks(pullRequestID)
		if err != nil {
			return statusChecksListedMsg{
				agentID:       agentID,
				pullRequestID: pullRequestID,
				headSHA:       head,
				err:           err,
			}
		}

		commit, headRead := headStatusCheckCommit(listed.GetReadings(), head)

		return statusChecksListedMsg{
			agentID:       agentID,
			pullRequestID: pullRequestID,
			headSHA:       head,
			headRead:      headRead,
			commit:        commit,
		}
	}
}

func (c *statusChecksScreen) applyListed(in screen.Inputs, msg statusChecksListedMsg) screen.Request {
	if msg.agentID != c.agentID {
		return nil
	}

	c.listing = false
	c.listedPullRequest, c.listedHead, c.listed = msg.pullRequestID, msg.headSHA, true
	if msg.err != nil {
		c.err = msg.err

		return nil
	}

	hovered := c.list().Hovered(in)
	c.commit, c.err = msg.commit, nil
	requests := screen.Batch{c.list().Place(in, hovered)}
	if !msg.headRead {
		c.listedHead = ""

		return requests
	}

	return append(requests, screen.Cmd(c.readStatusChecksCmd(in)))
}

func isStatusCheckFinished(item statusCheckItem) bool {
	return item.state == pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED
}

func isStatusCheckInFlight(item statusCheckItem) bool {
	return !isStatusCheckFinished(item) && !isStatusCheckUnknown(item)
}

func isStatusCheckUnknown(item statusCheckItem) bool {
	return item.state == pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_UNKNOWN
}

func (c *statusChecksScreen) IsLoading(screen.Inputs) bool {
	return slices.ContainsFunc(c.commit.items, isStatusCheckInFlight)
}

func newStatusCheckItem(check *pullrequestv1.StatusCheck) statusCheckItem {
	return statusCheckItem{
		providerID: check.GetProviderId(),
		checkID:    check.GetId(),
		name:       check.GetName(),
		state:      check.GetState(),
		conclusion: check.GetConclusion(),
		passed:     check.GetPassed(),
		startedAt:  dates.Parse(check.GetStartedAt()),
		finishedAt: dates.Parse(check.GetFinishedAt()),
		log:        check.GetLog(),
		steps: xslice.Map(check.GetSteps(), func(step *pullrequestv1.StatusCheckStep) statusCheckItem {
			return statusCheckItem{
				name:       step.GetName(),
				state:      step.GetState(),
				conclusion: step.GetConclusion(),
				passed:     step.GetPassed(),
				startedAt:  dates.Parse(step.GetStartedAt()),
				finishedAt: dates.Parse(step.GetFinishedAt()),
			}
		}),
	}
}

// headStatusCheckCommit is the reading of the given head, and false when that head has not been read yet.
func headStatusCheckCommit(readings []*pullrequestv1.StatusCheckReading, head string) (statusCheckCommit, bool) {
	i := slices.IndexFunc(readings, func(reading *pullrequestv1.StatusCheckReading) bool {
		return reading.GetHeadSha() == head
	})
	if i < 0 {
		return statusCheckCommit{}, false
	}

	return newStatusCheckCommit(readings[i]), true
}

// newStatusCheckCommit is one head's reading.
func newStatusCheckCommit(reading *pullrequestv1.StatusCheckReading) statusCheckCommit {
	return statusCheckCommit{
		headSHA:     reading.GetHeadSha(),
		firstReadAt: dates.Parse(reading.GetFirstReadAt()),
		items:       xslice.Map(reading.GetStatusChecks(), newStatusCheckItem),
	}
}

// tally is the commit's short hash and how long ago its checks were first read, empty when no commit was read.
func (c statusCheckCommit) tally(now time.Time) string {
	var parts []string
	if c.headSHA != "" {
		parts = append(parts, xsha.Short(c.headSHA))
	}
	if age := label.AgeAt(c.firstReadAt, now); age != "" {
		parts = append(parts, age+" ago")
	}

	return strings.Join(parts, separated)
}
