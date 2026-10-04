package ui

import (
	"slices"
	"strings"
	"time"

	"komrad/config"
	"komrad/internal/errui"
	"komrad/internal/glyph"
	"komrad/internal/ui/cursor"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/internal/view"
	"komrad/lib/dates"
	"komrad/lib/xslice"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// statusCheckItem is one check as the listing reported it.
type statusCheckItem struct {
	id         string
	name       string
	state      pullrequestv1.StatusCheckState
	conclusion string
	startedAt  time.Time
	finishedAt time.Time
	steps      []statusCheckItem
}

// statusChecksScreen lists the status checks on the head of the pull request opened from one agent's branch.
type statusChecksScreen struct {
	rowCursor
	agentID           string
	from              screen
	items             []statusCheckItem
	listedPullRequest string
	listedHead        string
	listed            bool
	listing           bool
	err               error
}

type statusCheckRow struct {
	item statusCheckItem
}

func (r statusCheckRow) key() string {
	return "status-check:" + r.item.id
}

func (c *statusChecksScreen) enter(m *model, target screenTarget) tea.Cmd {
	*c = statusChecksScreen{
		agentID: target.agentID,
		from:    target.from,
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
		quit:   func(k config.Keybindings) []string { return k.StatusChecks.Quit },
		back:   func(k config.Keybindings) []string { return k.StatusChecks.Back },
	}
}

func (c *statusChecksScreen) rows(m *model) []statusCheckRow {
	return narrowed(m.filter, narrowedParams[statusCheckRow]{
		rows: xslice.Map(c.items, func(item statusCheckItem) statusCheckRow {
			return statusCheckRow{item: item}
		}),
		depth: func(statusCheckRow) int { return 0 },
		name:  statusCheckRowName,
	})
}

func statusCheckRowName(row statusCheckRow) string {
	return row.item.name
}

func (c *statusChecksScreen) view(m *model) string {
	return m.columnListView(screenListParams{
		help:     c.help,
		empty:    c.emptyLine(m),
		rows:     len(c.rows(m)),
		snapshot: func(pane listPane) view.Snapshot { return c.snapshot(m, pane) },
	})
}

func (c *statusChecksScreen) emptyLine(m *model) string {
	switch {
	case c.err != nil:
		return "could not list: " + errui.GetReason(c.err)
	case m.statusChecksPullRequestID(c.agentID) == "":
		return "no pull request"
	case !c.listed:
		return "reading…"
	}

	return "no status checks yet"
}

// statusCheckColumns is the gutter of the status checks list: where each status check stands, as a
// word and then as an icon.
func statusCheckColumns() view.Columns {
	return view.Columns{
		{Kind: view.ColumnText, Width: statusCheckStandingColumns},
		{Kind: view.ColumnText, Width: glyph.Columns},
	}
}

const statusCheckStandingColumns = 10

func (c *statusChecksScreen) snapshot(m *model, pane listPane) view.Snapshot {
	rows := c.rows(m)

	return view.Snapshot{
		Rows: xslice.Map(rows, func(r statusCheckRow) view.Row {
			return view.Row{
				Key:     r.key(),
				Kind:    view.KindResult,
				Name:    r.item.name,
				Matches: matchesIn(m.filter.pattern, r.item.name),
				Process: view.ProcessNone,
				Cells: []view.Cell{
					mutedCell(statusCheckStanding(r.item)),
					statusCheckIcon(r.item, m.loading.frame()),
				},
			}
		}),
		Caret:      pane.caret,
		BlinkedOut: pane.blinkedOut,
		Focused:    rowKeyAt(rows, c.cursor),
		Pane:       pane.width,
		Columns:    statusCheckColumns(),
	}
}

// statusCheckStanding is the word drawn beside a check: how it ended once it has, and how far along it
// is until then.
func statusCheckStanding(item statusCheckItem) string {
	switch item.state {
	case pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED:
		if item.conclusion != "" {
			return strings.ToLower(item.conclusion)
		}

		return "completed"
	case pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_QUEUED:
		return "queued"
	case pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_RUNNING:
		return "running"
	case pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_WAITING:
		return "waiting"
	case pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_UNSPECIFIED:
	}

	return ""
}

func (c *statusChecksScreen) listActions() screenActionSpec {
	return screenActionSpec{
		table: append(c.list().actions(),
			screenAction{
				id:   actionStatusChecksBack,
				keys: func(k config.Keybindings) []string { return k.StatusChecks.Back },
				nav: func(m *model, _ string) tea.Cmd {
					return m.enter(c.from, screenTarget{agentID: c.agentID})
				},
			},
		),
		legend: slices.Concat(screenNavLegend(), []screenLegendRow{
			{
				of:    []actionID{actionStatusChecksBack},
				desc:  func(*model) string { return "back" },
				strip: true,
			},
		}),
	}
}

func (c *statusChecksScreen) help(m *model) keyMap {
	return c.listActions().help(m)
}

func (c *statusChecksScreen) parent() screen {
	return c.from
}

func (c *statusChecksScreen) crumbs(m *model) crumbs {
	name := c.agentID
	if item, ok := m.listing.anyAgent(c.agentID); ok {
		name = m.listing.agentName(item.Agent)
	}

	return crumbs{trail: []string{name, "status checks"}}
}

func (c *statusChecksScreen) doesDrawFromListing() bool {
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
	_ listFilterer    = (*statusChecksScreen)(nil)
	_ crumbDrawer     = (*statusChecksScreen)(nil)
	_ loadingReporter = (*statusChecksScreen)(nil)
)

func (c *statusChecksScreen) hoveredKey(m *model) string {
	return c.list().hovered(m)
}

func (c *statusChecksScreen) placeAt(m *model, key string) {
	c.list().place(m, key)
}

func (c *statusChecksScreen) filterRows(m *model) []filterRow {
	return c.list().filterRows(m)
}

// statusChecksListedMsg is the status checks on one pull request's head come back.
type statusChecksListedMsg struct {
	agentID       string
	pullRequestID string
	headSHA       string
	items         []statusCheckItem
	err           error
}

// statusChecksPullRequestID is the pull request the agent's row stands for, empty when it has none.
func (m *model) statusChecksPullRequestID(agentID string) string {
	item, ok := m.listing.anyAgent(agentID)
	if !ok || len(item.PullRequests) == 0 {
		return ""
	}

	return item.PullRequests[0].ID
}

// readStatusChecksCmd lists the status checks again when the agent's pull request or its head has moved since
// they were last listed, once at a time.
func (m *model) readStatusChecksCmd() tea.Cmd {
	if m.screen != screenStatusChecks {
		return nil
	}

	c := &m.statusChecks
	pullRequestID := m.statusChecksPullRequestID(c.agentID)
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
		listed, err := clients.listStatusChecks(pullRequestID)
		if err != nil {
			return statusChecksListedMsg{
				agentID:       agentID,
				pullRequestID: pullRequestID,
				headSHA:       head,
				err:           err,
			}
		}

		return statusChecksListedMsg{
			agentID:       agentID,
			pullRequestID: pullRequestID,
			headSHA:       head,
			items:         xslice.Map(listed.GetStatusChecks(), newStatusCheckItem),
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
	c.items, c.err = msg.items, nil
	rows := c.rows(m)
	c.cursor = cursor.Place(rowIndexOfKey(rows, hovered), c.cursor, len(rows))

	return m.readStatusChecksCmd()
}

// skippedConclusions are the conclusions a finished check ends with when it neither passed nor failed.
var skippedConclusions = []string{"neutral", "skipped"}

// statusCheckIcon is the icon drawn beside a check, in the glyphs and tones the forge column of
// the sidebar draws a pull request's checks with.
func statusCheckIcon(item statusCheckItem, loaderFrame string) view.Cell {
	switch {
	case !isStatusCheckFinished(item):
		return view.Cell{Text: loaderFrame, Tone: paint.ToneWarning}
	case strings.EqualFold(item.conclusion, "success"):
		return view.Cell{Glyph: glyph.StatusCheckPassing, Tone: paint.ToneSuccess}
	case slices.Contains(skippedConclusions, strings.ToLower(item.conclusion)):
		return view.Cell{Glyph: glyph.StatusCheckSkipped, Tone: paint.ToneMuted}
	}

	return view.Cell{Glyph: glyph.StatusCheckFailing, Tone: paint.ToneDanger}
}

func isStatusCheckFinished(item statusCheckItem) bool {
	return item.state == pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED
}

func (c *statusChecksScreen) isLoading(*model) bool {
	return slices.ContainsFunc(c.items, func(item statusCheckItem) bool {
		return !isStatusCheckFinished(item)
	})
}

func newStatusCheckItem(check *pullrequestv1.StatusCheck) statusCheckItem {
	return statusCheckItem{
		id:         check.GetProviderId(),
		name:       check.GetName(),
		state:      check.GetState(),
		conclusion: check.GetConclusion(),
		startedAt:  dates.Parse(check.GetStartedAt()),
		finishedAt: dates.Parse(check.GetFinishedAt()),
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

func (c *statusChecksScreen) docRows(m *model, width int) []string {
	rows := c.rows(m)
	if c.cursor < 0 || c.cursor >= len(rows) {
		return nil
	}

	item := rows[c.cursor].item
	indent := strings.Repeat(" ", docMargin())
	icon := func(item statusCheckItem) string {
		cell := statusCheckIcon(item, m.loading.frame())
		text := cell.Text
		if text == "" {
			text = m.glyphs.Spell(cell.Glyph)
		}

		return paint.Panel.Style(cell.Tone).Render(text)
	}

	var notice []string
	if duration := dates.Between(item.startedAt, item.finishedAt); duration != "" {
		notice = append(notice, duration)
	}
	if len(item.steps) > 0 {
		notice = append(notice, label.Count(len(item.steps), "step", "steps"))
	}

	out := []string{fitRow(indent+icon(item)+" "+lipgloss.NewStyle().Bold(true).Render(item.name), width)}
	if len(notice) > 0 {
		out = append(out, fitRow(indent+dimStyle().Render(strings.Join(notice, " · ")), width))
	}
	out = append(out, "")
	if len(item.steps) == 0 {
		return append(out, fitRow(indent+dimStyle().Render("no steps reported"), width))
	}

	focal := slices.IndexFunc(item.steps, func(step statusCheckItem) bool { return !isStatusCheckClean(step) })
	for i, step := range item.steps {
		lead := indent + icon(step) + " "
		duration := dates.Between(step.startedAt, step.finishedAt)
		name := ansi.Truncate(step.name, max(width-lipgloss.Width(lead)-lipgloss.Width(duration)-docMargin()-1, 0), "…")
		if i == focal {
			name = lipgloss.NewStyle().Bold(true).Render(name)
		}

		left := lead + name
		gap := max(width-lipgloss.Width(left)-lipgloss.Width(duration)-docMargin(), 1)
		out = append(out, fitRow(left+strings.Repeat(" ", gap)+dimStyle().Render(duration), width))
	}

	return out
}
