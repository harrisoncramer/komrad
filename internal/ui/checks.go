package ui

import (
	"slices"
	"strings"

	"komrad/config"
	"komrad/internal/errui"
	"komrad/internal/ui/cursor"
	"komrad/internal/view"
	"komrad/lib/xslice"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	tea "charm.land/bubbletea/v2"
)

// checkItem is one check as the listing reported it.
type checkItem struct {
	id         string
	name       string
	state      pullrequestv1.CheckState
	conclusion string
}

// checksScreen lists the checks on the head of the pull request opened from one agent's branch.
type checksScreen struct {
	rowCursor
	agentID           string
	from              screen
	items             []checkItem
	listedPullRequest string
	listedHead        string
	listed            bool
	listing           bool
	err               error
}

type checkRow struct {
	item checkItem
}

func (r checkRow) key() string {
	return "check:" + r.item.id
}

func (c *checksScreen) enter(m *model, target screenTarget) tea.Cmd {
	*c = checksScreen{
		agentID: target.agentID,
		from:    target.from,
	}

	return m.readChecksCmd()
}

func (c *checksScreen) update(m *model, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	return c.list().update(m, key, c.listActions())
}

func (c *checksScreen) list() screenList[checkRow] {
	return screenList[checkRow]{
		cursor: func() *rowCursor { return &c.rowCursor },
		rows:   c.rows,
		name:   checkRowName,
		moved:  func(m *model) tea.Cmd { return m.waitToStage() },
		quit:   func(k config.Keybindings) []string { return k.Checks.Quit },
		back:   func(k config.Keybindings) []string { return k.Checks.Back },
	}
}

func (c *checksScreen) rows(m *model) []checkRow {
	return narrowed(m.filter, narrowedParams[checkRow]{
		rows: xslice.Map(c.items, func(item checkItem) checkRow {
			return checkRow{item: item}
		}),
		depth: func(checkRow) int { return 0 },
		name:  checkRowName,
	})
}

func checkRowName(row checkRow) string {
	return row.item.name
}

func (c *checksScreen) view(m *model) string {
	return m.columnListView(screenListParams{
		help:     c.help,
		empty:    c.emptyLine(m),
		rows:     len(c.rows(m)),
		snapshot: func(pane listPane) view.Snapshot { return c.snapshot(m, pane) },
	})
}

func (c *checksScreen) emptyLine(m *model) string {
	switch {
	case c.err != nil:
		return "could not list: " + errui.GetReason(c.err)
	case m.checksPullRequestID(c.agentID) == "":
		return "no pull request"
	case !c.listed:
		return "reading…"
	}

	return "no checks yet"
}

// checkColumns is the gutter of the checks list: where each check stands.
func checkColumns() view.Columns {
	return view.Columns{{Kind: view.ColumnText, Width: checkStandingColumns}}
}

const checkStandingColumns = 10

func (c *checksScreen) snapshot(m *model, pane listPane) view.Snapshot {
	rows := c.rows(m)

	return view.Snapshot{
		Rows: xslice.Map(rows, func(r checkRow) view.Row {
			return view.Row{
				Key:     r.key(),
				Kind:    view.KindResult,
				Name:    r.item.name,
				Matches: matchesIn(m.filter.pattern, r.item.name),
				Process: view.ProcessNone,
				Cells:   []view.Cell{mutedCell(checkStanding(r.item))},
			}
		}),
		Caret:      pane.caret,
		BlinkedOut: pane.blinkedOut,
		Focused:    rowKeyAt(rows, c.cursor),
		Pane:       pane.width,
		Columns:    checkColumns(),
	}
}

// checkStanding is the word drawn beside a check: how it ended once it has, and how far along it
// is until then.
func checkStanding(item checkItem) string {
	switch item.state {
	case pullrequestv1.CheckState_CHECK_STATE_COMPLETED:
		if item.conclusion != "" {
			return strings.ToLower(item.conclusion)
		}

		return "completed"
	case pullrequestv1.CheckState_CHECK_STATE_QUEUED:
		return "queued"
	case pullrequestv1.CheckState_CHECK_STATE_RUNNING:
		return "running"
	case pullrequestv1.CheckState_CHECK_STATE_WAITING:
		return "waiting"
	case pullrequestv1.CheckState_CHECK_STATE_UNSPECIFIED:
	}

	return ""
}

func (c *checksScreen) listActions() screenActionSpec {
	return screenActionSpec{
		table: append(c.list().actions(),
			screenAction{
				id:   actionChecksBack,
				keys: func(k config.Keybindings) []string { return k.Checks.Back },
				nav: func(m *model, _ string) tea.Cmd {
					return m.enter(c.from, screenTarget{agentID: c.agentID})
				},
			},
		),
		legend: slices.Concat(screenNavLegend(), []screenLegendRow{
			{
				of:    []actionID{actionChecksBack},
				desc:  func(*model) string { return "back" },
				strip: true,
			},
		}),
	}
}

func (c *checksScreen) help(m *model) keyMap {
	return c.listActions().help(m)
}

func (c *checksScreen) parent() screen {
	return c.from
}

func (c *checksScreen) crumbs(m *model) crumbs {
	name := c.agentID
	if item, ok := m.listing.anyAgent(c.agentID); ok {
		name = m.listing.agentName(item.Agent)
	}

	return crumbs{trail: []string{name, "checks"}}
}

func (c *checksScreen) doesDrawFromListing() bool {
	return true
}

func (c *checksScreen) takesEveryKey() bool {
	return false
}

func (c *checksScreen) column() columnSpec {
	return columnSpec{
		floor:   columnFloor,
		docRows: func(*model, int) []string { return nil },
	}
}

func (c *checksScreen) stageResting(*model) tea.Cmd {
	return nil
}

func (c *checksScreen) dispatches(*model) []dispatching {
	return []dispatching{c.listActions()}
}

var (
	_ listFilterer = (*checksScreen)(nil)
	_ crumbDrawer  = (*checksScreen)(nil)
)

func (c *checksScreen) hoveredKey(m *model) string {
	return c.list().hovered(m)
}

func (c *checksScreen) placeAt(m *model, key string) {
	c.list().place(m, key)
}

func (c *checksScreen) filterRows(m *model) []filterRow {
	return c.list().filterRows(m)
}

// checksListedMsg is the checks on one pull request's head come back.
type checksListedMsg struct {
	agentID       string
	pullRequestID string
	headSHA       string
	items         []checkItem
	err           error
}

// checksPullRequestID is the pull request the agent's row stands for, empty when it has none.
func (m *model) checksPullRequestID(agentID string) string {
	item, ok := m.listing.anyAgent(agentID)
	if !ok || len(item.PullRequests) == 0 {
		return ""
	}

	return item.PullRequests[0].ID
}

// readChecksCmd lists the checks again when the agent's pull request or its head has moved since
// they were last listed, once at a time.
func (m *model) readChecksCmd() tea.Cmd {
	if m.screen != screenChecks {
		return nil
	}

	c := &m.checks
	pullRequestID := m.checksPullRequestID(c.agentID)
	if pullRequestID == "" || c.listing {
		return nil
	}

	item, _ := m.listing.anyAgent(c.agentID)
	head := item.PullRequest.HeadSHA
	if c.listed && c.listedPullRequest == pullRequestID && c.listedHead == head {
		return nil
	}

	c.listing = true
	agentID, clients := c.agentID, m.api

	return func() tea.Msg {
		listed, err := clients.listChecks(pullRequestID)
		if err != nil {
			return checksListedMsg{
				agentID:       agentID,
				pullRequestID: pullRequestID,
				headSHA:       head,
				err:           err,
			}
		}

		return checksListedMsg{
			agentID:       agentID,
			pullRequestID: pullRequestID,
			headSHA:       head,
			items: xslice.Map(listed.GetChecks(), func(check *pullrequestv1.Check) checkItem {
				return checkItem{
					id:         check.GetProviderId(),
					name:       check.GetName(),
					state:      check.GetState(),
					conclusion: check.GetConclusion(),
				}
			}),
		}
	}
}

func (m *model) applyChecksListed(msg checksListedMsg) tea.Cmd {
	c := &m.checks
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
	c.items, c.err = msg.items, nil
	rows := c.rows(m)
	c.cursor = cursor.Place(rowIndexOfKey(rows, hovered), c.cursor, len(rows))

	return m.readChecksCmd()
}
