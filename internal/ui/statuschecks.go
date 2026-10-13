package ui

import (
	"slices"
	"strings"
	"time"

	"komrad/internal/ui/paint/label"
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
	startedAt  time.Time
	finishedAt time.Time
	steps      []statusCheckItem
	run        statusCheckRun
	log        *pullrequestv1.CheckLog
}

// statusCheckRun is the run a check ran in, by one workflow or app.
type statusCheckRun struct {
	source    string
	event     string
	startedAt time.Time
}

// statusCheckCommit is the checks read on the pull request's newest head.
type statusCheckCommit struct {
	headSHA     string
	firstReadAt time.Time
	items       []statusCheckItem
}

// statusCheckKey is one check across its reruns.
type statusCheckKey struct {
	source string
	event  string
	name   string
}

// statusChecksScreen lists the status checks on the newest head of the pull request opened from one agent's branch.
type statusChecksScreen struct {
	rowCursor
	agentID           string
	openedFrom        screen
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

func (r statusCheckRow) key() string {
	return "status-check:" + r.item.providerID
}

func (c *statusChecksScreen) enter(m *model, target screenTarget) tea.Cmd {
	if target.agentID != "" {
		*c = statusChecksScreen{
			agentID:    target.agentID,
			openedFrom: target.openedFrom,
		}
	}

	return m.readStatusChecksCmd()
}

func (c *statusChecksScreen) update(m *model, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	return c.list().update(m, key, c.listActions())
}

func (c *statusChecksScreen) list() screenList[statusCheckRow] {
	return screenList[statusCheckRow]{
		cursor: func() *rowCursor { return &c.rowCursor },
		rows:   c.rows,
		name:   statusCheckRowName,
		moved:  func(m *model) tea.Cmd { return m.waitToStage() },
	}
}

func (c *statusChecksScreen) rows(m *model) []statusCheckRow {
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

func (c *statusChecksScreen) listParams(m *model) screenListParams {
	return screenListParams{
		tally:    c.commit.tally(),
		help:     c.help,
		empty:    c.emptyLine(m),
		rows:     len(c.rows(m)),
		snapshot: func(pane listPane) view.Snapshot { return c.snapshot(m, pane) },
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
		return !isStatusCheckClean(item)
	case statusCheckScopeActive:
		return isStatusCheckInFlight(item)
	case statusCheckScopeFailing:
		return isStatusCheckFinished(item) && !isStatusCheckClean(item)
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

func (c *statusChecksScreen) kindsFit(m *model) bool {
	return m.listFitsTabs(c.listParams(m))
}

func (c *statusChecksScreen) kindsFocused(*model) *bool {
	return &c.scopeFocus
}

func (c *statusChecksScreen) kindsSwitch(*model) bool {
	return true
}

// stepKind moves the listing to another scope, keeping the cursor on its row when the new scope lists it.
func (c *statusChecksScreen) stepKind(m *model, delta int) tea.Cmd {
	hovered := c.list().hovered(m)
	c.scope = statusCheckScope(stepStop(int(c.shownScope()), delta, len(statusCheckScopeLabels)))
	c.scopeChosen = true
	c.list().place(m, hovered)

	return m.waitToStage()
}

func (c *statusChecksScreen) listActions() screenActionSpec {
	return screenActionSpec{
		table: append(c.list().actions(),
			screenAction{
				id:   actionStatusChecksBack,
				keys: fixedBack,
				nav: func(m *model, _ string) tea.Cmd {
					return m.enter(c.openedFrom, screenTarget{agentID: c.agentID})
				},
			},
			screenAction{
				id:   actionStatusChecksLogs,
				keys: fixedOpen,
				nav: func(m *model, _ string) tea.Cmd {
					return m.enter(screenStatusCheckLogs, screenTarget{agentID: c.agentID, openedFrom: screenStatusChecks})
				},
			},
		),
		legend: slices.Concat(screenNavLegend(), []screenLegendRow{
			{
				of:    []actionID{actionStatusChecksBack},
				desc:  func(*model) string { return "back" },
				strip: true,
			},
			{
				of:    []actionID{actionStatusChecksLogs},
				desc:  func(*model) string { return "logs" },
				strip: true,
			},
		}),
	}
}

func (c *statusChecksScreen) help(m *model) keyMap {
	return c.listActions().help(m)
}

func (c *statusChecksScreen) parent() screen {
	return c.openedFrom
}

func (c *statusChecksScreen) crumbs(m *model) crumbs {
	name := c.agentID
	if item, ok := m.listing.anyAgent(c.agentID); ok {
		name = m.listing.agentName(item.Agent)
	}

	return crumbs{trail: []string{name, "status checks"}}
}

func (c *statusChecksScreen) doesRenderFromListing() bool {
	return true
}

func (c *statusChecksScreen) takesEveryKey() bool {
	return false
}

func (c *statusChecksScreen) column() columnSpec {
	return columnSpec{
		floor:   columnFloor,
		docRows: func(m *model, width int) []string { return m.statusChecks.docRows(m, width) },
	}
}

func (c *statusChecksScreen) stageResting(*model) tea.Cmd {
	return nil
}

func (c *statusChecksScreen) dispatches(*model) []dispatching {
	return []dispatching{c.listActions()}
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
	item, ok := m.listing.anyAgent(agentID)
	if !ok {
		return ""
	}

	pullRequests := m.listing.tasks.findForAgent(item.Agent).PullRequests
	if len(pullRequests) == 0 {
		return ""
	}

	return pullRequests[0].ID
}

// readStatusChecksCmd lists the status checks again when the agent's pull request or its head has moved since
// they were last listed, once at a time.
func (m *model) readStatusChecksCmd() tea.Cmd {
	if m.screen != screenStatusChecks && m.screen != screenStatusCheckLogs {
		return nil
	}

	c := &m.statusChecks
	pullRequestID := m.statusChecksPullRequestID(c.agentID)
	if pullRequestID == "" || c.listing {
		return nil
	}

	item, _ := m.listing.anyAgent(c.agentID)
	head := m.listing.tasks.findForAgent(item.Agent).PullRequest.HeadSHA
	if c.listed && c.listedPullRequest == pullRequestID && c.listedHead == head {
		return nil
	}

	c.listing = true
	agentID, clients := c.agentID, m.api

	return func() tea.Msg {
		listed, err := clients.listStatusChecks(pullRequestID)
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

func (m *model) applyStatusChecksListed(msg statusChecksListedMsg) tea.Cmd {
	c := &m.statusChecks
	if msg.agentID != c.agentID {
		return nil
	}

	c.listing = false
	c.listedPullRequest, c.listedHead, c.listed = msg.pullRequestID, msg.headSHA, true
	if msg.err != nil {
		c.err = msg.err

		return nil
	}

	hovered := c.list().hovered(m)
	c.commit, c.err = msg.commit, nil
	c.list().place(m, hovered)
	m.statusCheckLogs.reanchor(m)
	reads := m.statusCheckLogs.readsCmd(m)
	if !msg.headRead {
		c.listedHead = ""

		return reads
	}

	return tea.Batch(reads, m.readStatusChecksCmd())
}

// skippedConclusions are the conclusions a finished check ends with when it neither passed nor failed.
var skippedConclusions = []string{"neutral", "skipped"}

func isStatusCheckFinished(item statusCheckItem) bool {
	return item.state == pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED
}

func isStatusCheckInFlight(item statusCheckItem) bool {
	return !isStatusCheckFinished(item) && !isStatusCheckUnknown(item)
}

func isStatusCheckUnknown(item statusCheckItem) bool {
	return item.state == pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_UNKNOWN
}

func (c *statusChecksScreen) isLoading(*model) bool {
	return slices.ContainsFunc(c.commit.items, isStatusCheckInFlight)
}

func newStatusCheckItem(check *pullrequestv1.StatusCheck) statusCheckItem {
	return statusCheckItem{
		providerID: check.GetProviderId(),
		checkID:    check.GetId(),
		name:       check.GetName(),
		state:      check.GetState(),
		conclusion: check.GetConclusion(),
		startedAt:  dates.Parse(check.GetStartedAt()),
		finishedAt: dates.Parse(check.GetFinishedAt()),
		run: statusCheckRun{
			source:    check.GetSource(),
			event:     check.GetRunEvent(),
			startedAt: dates.Parse(check.GetRunStartedAt()),
		},
		log: check.GetLog(),
		steps: xslice.Map(check.GetSteps(), func(step *pullrequestv1.StatusCheckStep) statusCheckItem {
			return statusCheckItem{
				name:       step.GetName(),
				state:      step.GetState(),
				conclusion: step.GetConclusion(),
				startedAt:  dates.Parse(step.GetStartedAt()),
				finishedAt: dates.Parse(step.GetFinishedAt()),
			}
		}),
	}
}

func isStatusCheckClean(item statusCheckItem) bool {
	return isStatusCheckFinished(item) &&
		(strings.EqualFold(item.conclusion, "success") || slices.Contains(skippedConclusions, strings.ToLower(item.conclusion)))
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

// newStatusCheckCommit is one head's reading, holding only the newest run of each check.
func newStatusCheckCommit(reading *pullrequestv1.StatusCheckReading) statusCheckCommit {
	items := xslice.Map(reading.GetStatusChecks(), newStatusCheckItem)
	newest := map[statusCheckKey]statusCheckItem{}
	for _, item := range items {
		if held, ok := newest[item.key()]; !ok || compareStatusCheckItems(held, item) < 0 {
			newest[item.key()] = item
		}
	}

	return statusCheckCommit{
		headSHA:     reading.GetHeadSha(),
		firstReadAt: dates.Parse(reading.GetFirstReadAt()),
		items: xslice.Filter(items, func(item statusCheckItem) bool {
			return newest[item.key()].providerID == item.providerID
		}),
	}
}

func (i statusCheckItem) key() statusCheckKey {
	return statusCheckKey{
		source: i.run.source,
		event:  i.run.event,
		name:   i.name,
	}
}

func compareStatusCheckItems(a, b statusCheckItem) int {
	if order := a.run.startedAt.Compare(b.run.startedAt); order != 0 {
		return order
	}
	if order := a.startedAt.Compare(b.startedAt); order != 0 {
		return order
	}

	return strings.Compare(a.providerID, b.providerID)
}

// tally is the commit's short hash and how long ago its checks were first read, empty when no commit was read.
func (c statusCheckCommit) tally() string {
	var parts []string
	if c.headSHA != "" {
		parts = append(parts, xsha.Short(c.headSHA))
	}
	if age := label.Age(c.firstReadAt); age != "" {
		parts = append(parts, age+" ago")
	}

	return strings.Join(parts, separated)
}
