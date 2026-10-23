package checks

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

const (
	actionBack keys.ID = keys.Own + iota
	actionLogs
	actionLogsBack
)

const separated = " " + glyph.Separator + " "

// check is one check as the listing reported it.
type check struct {
	providerID string
	checkID    string
	name       string
	state      pullrequestv1.StatusCheckState
	conclusion string
	passed     bool
	startedAt  time.Time
	finishedAt time.Time
	steps      []check
	log        *pullrequestv1.CheckLog
}

// commit is the checks read on the pull request's newest head.
type commit struct {
	headSHA     string
	firstReadAt time.Time
	items       []check
}

// Deps is what the status checks screens are given at launch.
type Deps struct {
	Keys    config.Keybindings
	Glyphs  glyph.Set
	Clock   screen.Clock
	Clients api.Clients
	Top     *keys.Top
}

// Screen lists the status checks on the newest head of the pull request opened from one agent's branch.
type Screen struct {
	deps Deps
	list.Cursor
	agentID           string
	openedFrom        screen.ID
	commit            commit
	listedPullRequest string
	listedHead        string
	listed            bool
	listing           bool
	err               error
	scope             scope
	scopeChosen       bool
	scopeFocus        bool
	logs              logs
}

// New is the status checks screen built from what it is given at launch.
func New(deps Deps) Screen {
	return Screen{deps: deps}
}

type checkRow struct {
	item check
}

func (r checkRow) Key() string {
	return "status-check:" + r.item.providerID
}

// Enter lists the status checks, starting afresh when the screen opens for an agent.
func (c *Screen) Enter(in screen.Inputs, target screen.Target) screen.Request {
	if target.AgentID != "" {
		*c = Screen{
			deps:       c.deps,
			agentID:    target.AgentID,
			openedFrom: target.OpenedFromScreen,
			logs:       c.logs,
		}
	}

	return screen.RunCmd(c.listCmd(in))
}

// Update sends a key to the screen's table.
func (c *Screen) Update(in screen.Inputs, msg tea.Msg) screen.Request {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	return c.listActions(in).DispatchTop(c.deps.Top, c, c.deps.Keys, key.String())
}

// Receive takes the status checks as they are listed, and lists them again once the listing moves.
func (c *Screen) Receive(in screen.Inputs, msg tea.Msg) (screen.Request, bool) {
	switch msg := msg.(type) {
	case listedMsg:
		return c.applyListed(in, msg), true
	case screen.ListingChanged:
		return screen.RunCmd(c.listCmd(in)), true
	}

	return nil, false
}

func (c *Screen) list() keys.ScreenList[checkRow] {
	return keys.ScreenList[checkRow]{
		Keys:   c.deps.Keys,
		Cursor: func() *list.Cursor { return &c.Cursor },
		Rows:   c.rows,
		Name:   checkRowName,
		Moved:  func(screen.Inputs) screen.Request { return screen.Settle{} },
	}
}

func (c *Screen) rows(in screen.Inputs) []checkRow {
	return list.Narrowed(in.Room.Pattern, list.NarrowedParams[checkRow]{
		Rows: xslice.Map(xslice.Filter(c.commit.items, c.shownScope().holds), func(item check) checkRow {
			return checkRow{item: item}
		}),
		Depth: func(checkRow) int { return 0 },
		Name:  checkRowName,
	})
}

func checkRowName(row checkRow) string {
	return row.item.name
}

// ListParams is the list of status checks as the root renders it.
func (c *Screen) ListParams(in screen.Inputs) list.Params {
	return list.Params{
		Tally:    c.commit.tally(c.deps.Clock.Now()),
		Help:     c.help(in),
		Empty:    c.emptyLine(in),
		Rows:     len(c.rows(in)),
		Snapshot: func(pane list.Pane) view.Snapshot { return c.snapshot(in, pane) },
	}
}

// scope is which of the status checks the status checks screen lists.
type scope int

const (
	scopeAll scope = iota
	scopeNotPassing
	scopeActive
	scopeFailing
)

var scopeLabels = []string{"all", "not passing", "active", "failing"}

// holds reports whether the scope lists the status check.
func (s scope) holds(item check) bool {
	switch s {
	case scopeNotPassing:
		return !item.passed
	case scopeActive:
		return isInFlight(item)
	case scopeFailing:
		return isFinished(item) && !item.passed
	case scopeAll:
	}

	return true
}

func (c *Screen) shownScope() scope {
	switch {
	case c.scopeChosen:
		return c.scope
	case slices.ContainsFunc(c.commit.items, scopeNotPassing.holds):
		return scopeNotPassing
	}

	return scopeAll
}

// Kinds is the row of scopes the status checks are listed under.
func (c *Screen) Kinds() tabs.Stops {
	return tabs.Stops{
		Labels:      scopeLabels,
		FocusedStop: tabs.NoStop,
		ChosenStop:  int(c.shownScope()),
	}
}

// KindsFocused is whether the row of scopes holds the keys.
func (c *Screen) KindsFocused() *bool {
	return &c.scopeFocus
}

// KindsSwitch reports that the scope may always change.
func (c *Screen) KindsSwitch() bool {
	return true
}

// StepKind moves the listing to another scope, keeping the cursor on its row when the new scope lists it.
func (c *Screen) StepKind(in screen.Inputs, delta int) screen.Request {
	hovered := c.list().Hovered(in)
	c.scope = scope(tabs.StepStop(int(c.shownScope()), delta, len(scopeLabels)))
	c.scopeChosen = true

	return screen.Batch{c.list().Place(in, hovered), screen.Settle{}}
}

func (c *Screen) listActions(in screen.Inputs) keys.Spec[*Screen] {
	return keys.Spec[*Screen]{
		Table: append(keys.ListActions[*Screen](c.list(), in),
			keys.Action[*Screen]{
				ID:   actionBack,
				Keys: keys.FixedBack,
				Nav: func(*Screen, string) screen.Request {
					return screen.Open{Screen: c.openedFrom, Target: screen.Target{AgentID: c.agentID}}
				},
			},
			keys.Action[*Screen]{
				ID:   actionLogs,
				Keys: keys.FixedOpen,
				Nav: func(*Screen, string) screen.Request {
					return screen.Open{Screen: screen.StatusCheckLogs, Target: screen.Target{AgentID: c.agentID, OpenedFromScreen: screen.StatusChecks}}
				},
			},
		),
		Legend: slices.Concat(keys.NavLegend[*Screen](), []keys.LegendRow[*Screen]{
			{
				ActionIDs: []keys.ID{keys.FilterOpen},
				Desc:      keys.FilterDescOf[*Screen](in),
			},
			{
				ActionIDs: []keys.ID{keys.FilterClear},
				Desc:      keys.FilterClearDescOf[*Screen](in),
				Strip:     true,
			},
			{
				ActionIDs: []keys.ID{keys.FilterMatch},
				Desc:      func(*Screen) string { return "next/prev match" },
				Strip:     true,
			},
			{
				ActionIDs: []keys.ID{actionBack},
				Desc:      func(*Screen) string { return "back" },
				Strip:     true,
			},
			{
				ActionIDs: []keys.ID{actionLogs},
				Desc:      func(*Screen) string { return "logs" },
				Strip:     true,
			},
		}),
	}
}

func (c *Screen) help(in screen.Inputs) legend.Help {
	return c.listActions(in).Help(c, c.deps.Keys)
}

// Parent is the screen the status checks were opened from.
func (c *Screen) Parent() screen.ID {
	return c.openedFrom
}

// Crumbs is the trail down to the agent's status checks.
func (c *Screen) Crumbs(in screen.Inputs) screen.Crumbs {
	name := c.agentID
	if item, ok := in.Listing.AnyAgent(c.agentID); ok {
		name = in.Listing.AgentName(item.Agent)
	}

	return screen.Crumbs{Trail: []string{name, "status checks"}}
}

// DoesRenderFromListing reports that the screen names the agent from the listing.
func (c *Screen) DoesRenderFromListing() bool {
	return true
}

// TakesEveryKey reports that nothing on the screen takes every key.
func (c *Screen) TakesEveryKey() bool {
	return false
}

// Column is the hovered status check's steps rendered in the block beside the list.
func (c *Screen) Column(in screen.Inputs) column.Spec {
	return column.Spec{
		Floor:   column.Narrowest,
		DocRows: func(width int) []string { return c.docRows(in, width) },
	}
}

// StageResting stages nothing for the row the cursor rests on.
func (c *Screen) StageResting(screen.Inputs) screen.Request {
	return nil
}

// Dispatches is the screen's one table.
func (c *Screen) Dispatches() []keys.Table {
	return []keys.Table{c.listActions(screen.Inputs{})}
}

// ActiveList is the list of status checks the filter narrows.
func (c *Screen) ActiveList() keys.KeyedList {
	return c.list()
}

// listedMsg is the status checks on one pull request's newest head come back.
type listedMsg struct {
	agentID       string
	pullRequestID string
	headSHA       string
	headRead      bool
	commit        commit
	err           error
}

// pullRequestOf is the pull request the agent's row stands for, empty when it has none.
func pullRequestOf(listing panel.Listing, agentID string) string {
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

// listCmd lists the status checks again when the agent's pull request or its head has moved since
// they were last listed, once at a time.
func (c *Screen) listCmd(in screen.Inputs) tea.Cmd {
	if !in.IsActive {
		return nil
	}

	pullRequestID := pullRequestOf(in.Listing, c.agentID)
	if pullRequestID == "" || c.listing {
		return nil
	}

	item, _ := in.Listing.AnyAgent(c.agentID)
	head := in.Listing.Tasks.FindForAgent(item.Agent).PullRequest.HeadSHA
	if c.listed && c.listedPullRequest == pullRequestID && c.listedHead == head {
		return nil
	}

	c.listing = true
	agentID, clients := c.agentID, c.deps.Clients

	return func() tea.Msg {
		listed, err := clients.ListStatusChecks(pullRequestID)
		if err != nil {
			return listedMsg{
				agentID:       agentID,
				pullRequestID: pullRequestID,
				headSHA:       head,
				err:           err,
			}
		}

		read, headRead := headCommit(listed.GetReadings(), head)

		return listedMsg{
			agentID:       agentID,
			pullRequestID: pullRequestID,
			headSHA:       head,
			headRead:      headRead,
			commit:        read,
		}
	}
}

func (c *Screen) applyListed(in screen.Inputs, msg listedMsg) screen.Request {
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

	return append(requests, screen.RunCmd(c.listCmd(in)))
}

func isFinished(item check) bool {
	return item.state == pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED
}

func isInFlight(item check) bool {
	return !isFinished(item) && !isUnknown(item)
}

func isUnknown(item check) bool {
	return item.state == pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_UNKNOWN
}

// IsLoading reports whether any status check is still running.
func (c *Screen) IsLoading(screen.Inputs) bool {
	return slices.ContainsFunc(c.commit.items, isInFlight)
}

func newCheck(status *pullrequestv1.StatusCheck) check {
	return check{
		providerID: status.GetProviderId(),
		checkID:    status.GetId(),
		name:       status.GetName(),
		state:      status.GetState(),
		conclusion: status.GetConclusion(),
		passed:     status.GetPassed(),
		startedAt:  dates.Read(status.GetStartedAt()),
		finishedAt: dates.Read(status.GetFinishedAt()),
		log:        status.GetLog(),
		steps: xslice.Map(status.GetSteps(), func(step *pullrequestv1.StatusCheckStep) check {
			return check{
				name:       step.GetName(),
				state:      step.GetState(),
				conclusion: step.GetConclusion(),
				passed:     step.GetPassed(),
				startedAt:  dates.Read(step.GetStartedAt()),
				finishedAt: dates.Read(step.GetFinishedAt()),
			}
		}),
	}
}

// headCommit is the reading of the given head, and false when that head has not been read yet.
func headCommit(readings []*pullrequestv1.StatusCheckReading, head string) (commit, bool) {
	i := slices.IndexFunc(readings, func(reading *pullrequestv1.StatusCheckReading) bool {
		return reading.GetHeadSha() == head
	})
	if i < 0 {
		return commit{}, false
	}

	return newCommit(readings[i]), true
}

// newCommit is one head's reading.
func newCommit(reading *pullrequestv1.StatusCheckReading) commit {
	return commit{
		headSHA:     reading.GetHeadSha(),
		firstReadAt: dates.Read(reading.GetFirstReadAt()),
		items:       xslice.Map(reading.GetStatusChecks(), newCheck),
	}
}

// tally is the commit's short hash and how long ago its checks were first read, empty when no commit was read.
func (c commit) tally(now time.Time) string {
	var parts []string
	if c.headSHA != "" {
		parts = append(parts, xsha.Short(c.headSHA))
	}
	if age := label.AgeAt(c.firstReadAt, now); age != "" {
		parts = append(parts, age+" ago")
	}

	return strings.Join(parts, separated)
}
