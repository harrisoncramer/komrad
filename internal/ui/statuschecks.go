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
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/screen"
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
}

type statusCheckRow struct {
	item statusCheckItem
}

func (r statusCheckRow) Key() string {
	return "status-check:" + r.item.providerID
}

func (c *statusChecksScreen) enter(m *model, in screen.Inputs, target screen.Target) screen.Request {
	if target.AgentID != "" {
		*c = statusChecksScreen{
			deps:       c.deps,
			agentID:    target.AgentID,
			openedFrom: target.OpenedFrom,
		}
	}

	return screen.Cmd(m.readStatusChecksCmd())
}

func (c *statusChecksScreen) update(m *model, in screen.Inputs, msg tea.Msg) screen.Request {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	return c.list().update(m, key, c.listActions())
}

func (c *statusChecksScreen) receive(m *model, in screen.Inputs, msg tea.Msg) (screen.Request, bool) {
	listed, ok := msg.(statusChecksListedMsg)
	if !ok {
		return nil, false
	}

	return c.applyListed(m, in, listed), true
}

func (c *statusChecksScreen) list() screenList[statusCheckRow] {
	return screenList[statusCheckRow]{
		keys:   c.deps.keys,
		top:    c.deps.top,
		cursor: func() *list.Cursor { return &c.Cursor },
		rows:   c.rows,
		name:   statusCheckRowName,
		moved:  func(*model, screen.Inputs) screen.Request { return screen.Settle{} },
	}
}

func (c *statusChecksScreen) rows(m *model, in screen.Inputs) []statusCheckRow {
	return narrowed(m.filter, narrowedParams[statusCheckRow]{
		rows: xslice.Map(xslice.Filter(c.commit.items, c.shownScope().holds), func(item statusCheckItem) statusCheckRow {
			return statusCheckRow{item: item}
		}),
		depth: func(statusCheckRow) int { return 0 },
		name:  statusCheckRowName,
	})
}

func statusCheckRowName(row statusCheckRow) string {
	return row.item.name
}

func (c *statusChecksScreen) listParams(m *model, in screen.Inputs) screenListParams {
	return screenListParams{
		tally:    c.commit.tally(c.deps.clock.Now()),
		help:     c.help,
		empty:    c.emptyLine(m),
		rows:     len(c.rows(m, in)),
		snapshot: func(pane list.Pane) view.Snapshot { return c.snapshot(m, in, pane) },
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

func (c *statusChecksScreen) kinds(*model) stops {
	return stops{
		labels: statusCheckScopeLabels,
		focus:  noStop,
		lit:    int(c.shownScope()),
	}
}

func (c *statusChecksScreen) kindsFocused(*model) *bool {
	return &c.scopeFocus
}

func (c *statusChecksScreen) kindsSwitch(*model) bool {
	return true
}

// stepKind moves the listing to another scope, keeping the cursor on its row when the new scope lists it.
func (c *statusChecksScreen) stepKind(m *model, in screen.Inputs, delta int) screen.Request {
	hovered := c.list().hovered(m, in)
	c.scope = statusCheckScope(stepStop(int(c.shownScope()), delta, len(statusCheckScopeLabels)))
	c.scopeChosen = true

	return screen.Batch{c.list().place(m, in, hovered), screen.Settle{}}
}

func (c *statusChecksScreen) listActions() keys.Spec[*model] {
	return keys.Spec[*model]{
		Table: append(c.list().actions(),
			keys.Action[*model]{
				ID:   actionStatusChecksBack,
				Keys: keys.FixedBack,
				Nav: func(m *model, _ string) screen.Request {
					return screen.Open{Screen: c.openedFrom, Target: screen.Target{AgentID: c.agentID}}
				},
			},
			keys.Action[*model]{
				ID:   actionStatusChecksLogs,
				Keys: keys.FixedOpen,
				Nav: func(m *model, _ string) screen.Request {
					return screen.Open{Screen: screen.StatusCheckLogs, Target: screen.Target{AgentID: c.agentID, OpenedFrom: screen.StatusChecks}}
				},
			},
		),
		Legend: slices.Concat(keys.NavLegend[*model](), []keys.LegendRow[*model]{
			{
				Of:   []keys.ID{keys.FilterOpen},
				Desc: filterDescOf,
			},
			{
				Of:    []keys.ID{keys.FilterClear},
				Desc:  filterClearDescOf,
				Strip: true,
			},
			{
				Of:    []keys.ID{keys.FilterMatch},
				Desc:  func(*model) string { return "next/prev match" },
				Strip: true,
			},
			{
				Of:    []keys.ID{actionStatusChecksBack},
				Desc:  func(*model) string { return "back" },
				Strip: true,
			},
			{
				Of:    []keys.ID{actionStatusChecksLogs},
				Desc:  func(*model) string { return "logs" },
				Strip: true,
			},
		}),
	}
}

func (c *statusChecksScreen) help(m *model) keyMap {
	return m.helpOf(c.listActions())
}

func (c *statusChecksScreen) parent() screen.ID {
	return c.openedFrom
}

func (c *statusChecksScreen) crumbs(_ *model, in screen.Inputs) crumbs {
	name := c.agentID
	if item, ok := in.Listing.AnyAgent(c.agentID); ok {
		name = in.Listing.AgentName(item.Agent)
	}

	return crumbs{trail: []string{name, "status checks"}}
}

func (c *statusChecksScreen) doesRenderFromListing() bool {
	return true
}

func (c *statusChecksScreen) takesEveryKey() bool {
	return false
}

func (c *statusChecksScreen) column(m *model, in screen.Inputs) column.Spec {
	return column.Spec{
		Floor:   columnFloor,
		DocRows: func(width int) []string { return m.statusChecks.docRows(m, in, width) },
	}
}

func (c *statusChecksScreen) stageResting(*model, screen.Inputs) screen.Request {
	return nil
}

func (c *statusChecksScreen) dispatches(*model) []keys.Table {
	return []keys.Table{c.listActions()}
}

var (
	_ kindSwitcher    = (*statusChecksScreen)(nil)
	_ activeLister    = (*statusChecksScreen)(nil)
	_ crumbRenderer   = (*statusChecksScreen)(nil)
	_ loadingReporter = (*statusChecksScreen)(nil)
)

func (c *statusChecksScreen) activeList() keyedList {
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
func (m *model) statusChecksPullRequestID(agentID string) string {
	item, ok := m.listing.AnyAgent(agentID)
	if !ok {
		return ""
	}

	pullRequests := m.listing.Tasks.FindForAgent(item.Agent).PullRequests
	if len(pullRequests) == 0 {
		return ""
	}

	return pullRequests[0].ID
}

// readStatusChecksCmd lists the status checks again when the agent's pull request or its head has moved since
// they were last listed, once at a time.
func (m *model) readStatusChecksCmd() tea.Cmd {
	if m.screen != screen.StatusChecks && m.screen != screen.StatusCheckLogs {
		return nil
	}

	c := &m.statusChecks
	pullRequestID := m.statusChecksPullRequestID(c.agentID)
	if pullRequestID == "" || c.listing {
		return nil
	}

	item, _ := m.listing.AnyAgent(c.agentID)
	head := m.listing.Tasks.FindForAgent(item.Agent).PullRequest.HeadSHA
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

func (c *statusChecksScreen) applyListed(m *model, in screen.Inputs, msg statusChecksListedMsg) screen.Request {
	if msg.agentID != c.agentID {
		return nil
	}

	c.listing = false
	c.listedPullRequest, c.listedHead, c.listed = msg.pullRequestID, msg.headSHA, true
	if msg.err != nil {
		c.err = msg.err

		return nil
	}

	hovered := c.list().hovered(m, in)
	c.commit, c.err = msg.commit, nil
	requests := screen.Batch{c.list().place(m, in, hovered), m.statusCheckLogs.reanchor(m, m.inputsFor(&m.statusCheckLogs)), screen.Cmd(m.statusCheckLogs.readsCmd(m))}
	if !msg.headRead {
		c.listedHead = ""

		return requests
	}

	return append(requests, screen.Cmd(m.readStatusChecksCmd()))
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

func (c *statusChecksScreen) isLoading(*model, screen.Inputs) bool {
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
