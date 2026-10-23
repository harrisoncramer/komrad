package ui

import (
	"strconv"
	"strings"

	"komrad/config"
	"komrad/internal/ui/cursor"
	"komrad/internal/ui/filter"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/legend"
	"komrad/internal/ui/list"
	"komrad/internal/ui/record"
	"komrad/internal/ui/screen"

	tea "charm.land/bubbletea/v2"
)

const detailsChromeLines = 2

const legendRuleFloor = 2

type detailsState struct {
	focused bool
	list.Cursor
	held    string
	flashed int
}

// detailsRow is one field as the record renders it, keyed by its place in the whole record.
type detailsRow struct {
	id    string
	field record.Field
	// text is what a search reads the row by, which for a row under a label is that label and
	// the row's own value.
	text string
}

func (r detailsRow) Key() string {
	return r.id
}

func (m *model) hoveredDetails(in screen.Inputs) record.Details {
	d, ok := m.active().(recordRenderer)
	if !ok {
		return record.Details{}
	}

	return d.Details(in)
}

// detailsRows is the record's fields as its search leaves them.
func (m *model) detailsRows(held record.Details) []detailsRow {
	rows := make([]detailsRow, 0, len(held.Fields))
	pattern := m.filter.Pattern()
	group := ""
	for i, field := range held.Fields {
		if field.Label != "" {
			group = field.Label
		}

		text := strings.TrimSpace(group + " " + field.Value)
		if pattern.DoesNarrow(list.SectionRecord) && !pattern.DoesMatch(text) {
			continue
		}

		rows = append(rows, detailsRow{
			id:    strconv.Itoa(i),
			field: field,
			text:  text,
		})
	}

	return rows
}

func (m *model) detailsCursor(held record.Details) int {
	if m.details.held != held.ID {
		return 0
	}

	return cursor.Clamp(m.details.Row, len(m.detailsRows(held)))
}

func (m *model) detailsList() keys.ScreenList[detailsRow] {
	return keys.ScreenList[detailsRow]{
		Keys:   m.keys,
		Cursor: m.detailsRowCursor,
		Rows:   func(in screen.Inputs) []detailsRow { return m.detailsRows(m.hoveredDetails(in)) },
		Name:   func(row detailsRow) string { return row.text },
	}
}

func (m *model) detailsRowCursor() *list.Cursor {
	if held := m.hoveredDetails(m.inputs()); m.details.held != held.ID {
		m.details.Cursor = list.Cursor{}
		m.details.held = held.ID
	}

	return &m.details.Cursor
}

func (m *model) isDetailsRendered(held record.Details) bool {
	if len(held.Fields) == 0 || m.detailsLines() == 0 {
		return false
	}

	if _, ok := m.active().(recordRenderer); !ok {
		return true
	}

	return m.detailsFits.Read(m.shownScreen(), m.detailsFit)
}

// detailsFit is whether the record's share fits under a list, judged with the keys on the
// list.
func (m *model) detailsFit() bool {
	l, ok := m.active().(listRenderer)
	if !ok {
		return false
	}

	probe := *m
	probe.details.focused = false
	params := l.ListParams(probe.inputs())

	header := probe.tabbedHeader(params.Title, params.Tally)
	legend := probe.Help(probe.listHelp(params.Help), header)

	// Only the list's legend is measured with the search; a focused record shrinks to the room
	// its own legend leaves.
	probe.filter = filter.State{}
	if focused := probe.Help(probe.detailsHelp(), header); strings.Count(focused, "\n") > strings.Count(legend, "\n") {
		legend = focused
	}

	return probe.spareLines(header, legend) > probe.detailsLines()+detailsChromeLines
}

// listRecordRoom is the lines a list screen has under its header and the legend it is rendering.
func (m *model) listRecordRoom(params list.Params) int {
	header := m.tabbedHeader(params.Title, params.Tally)

	return m.spareLines(header, m.Help(m.listHelp(params.Help), header))
}

func (m *model) isDetailsFocused(in screen.Inputs) bool {
	return m.details.focused && m.isDetailsRendered(m.hoveredDetails(in))
}

// focusDetails gives the record the keys, reading its directory again on the way in.
func (m *model) focusDetails() tea.Cmd {
	in := m.inputs()
	if !m.isDetailsRendered(m.hoveredDetails(in)) {
		return nil
	}

	m.details.focused = true
	m.blurTabs()
	m.blurKinds()
	m.detailsRowCursor()

	return tea.Batch(m.apply(m.rereadTaskWorktree()), m.wakeCwdSyncCmd())
}

// releaseHiddenDetails hands the keys back from a record the pane no longer renders.
func (m *model) releaseHiddenDetails() {
	if in := m.inputs(); !m.details.focused || m.isDetailsRendered(m.hoveredDetails(in)) {
		return
	}

	m.blurDetails()
}

// blurDetails hands the keys back to the list.
func (m *model) blurDetails() {
	m.details.focused = false
}

func (m *model) updateDetails(key tea.KeyPressMsg) tea.Cmd {
	return m.apply(m.detailsActions().Dispatch(m, m.keys, key.String()))
}

func (m *model) detailsActions() keys.Spec[*model] {
	return keys.Spec[*model]{
		Table: append(keys.ListActions[*model](m.detailsList(), m.inputs()), []keys.Action[*model]{
			{
				ID:   keys.Details,
				Keys: keys.FixedBack,
				Nav: func(*model, string) screen.Request {
					m.details.focused = false

					return nil
				},
			},
			{
				ID:   keys.DetailsCopy,
				Keys: func(k config.Keybindings) []string { return k.Details.Copy },
				Nav:  func(*model, string) screen.Request { return screen.RunCmd(m.copyDetailsField()) },
			},
		}...),
		Legend: []keys.LegendRow[*model]{
			{
				ActionIDs:     []keys.ID{keys.DetailsCopy},
				Desc:          func(*model) string { return "copy field" },
				IsInShortHelp: true,
			},
			{
				ActionIDs:     []keys.ID{keys.Details},
				Desc:          func(*model) string { return "back to the list" },
				IsInShortHelp: true,
			},
			{
				ActionIDs: []keys.ID{keys.FilterOpen},
				Desc:      func(*model) string { return m.filter.OpenDesc() },
			},
			{
				ActionIDs: []keys.ID{keys.FilterMatch},
				Desc:      func(*model) string { return "next/prev match" },
			},
			{
				ActionIDs: []keys.ID{keys.FilterClear},
				Desc:      func(*model) string { return m.filter.ClearDesc() },
			},
			{
				ActionIDs: []keys.ID{keys.Move},
				Desc:      func(*model) string { return "move" },
				Label:     keys.MoveLabel,
			},
			{
				ActionIDs: []keys.ID{keys.Jump},
				Desc:      func(*model) string { return "top/bottom" },
				Label:     func(k config.Keybindings) string { return keys.Label(k.Nav.Top) + "/" + keys.Label(k.Nav.Bottom) },
			},
			{
				ActionIDs: []keys.ID{keys.NavPage},
				Desc:      func(*model) string { return "page" },
				Label:     keys.NavPageLabel,
			},
			{
				ActionIDs: []keys.ID{keys.ColumnScroll},
				Desc:      func(*model) string { return "scroll" },
				Label:     keys.ColumnScrollLabel,
			},
		},
	}
}

func (m *model) detailsHelp() legend.Help {
	return m.Legend(m.helpOf(m.detailsActions()))
}

func (m *model) copyDetailsField() tea.Cmd {
	held := m.hoveredDetails(m.inputs())
	rows := m.detailsRows(held)
	if len(rows) == 0 {
		return nil
	}

	cursor := m.detailsCursor(held)
	m.details.flashed = cursor + 1

	return tea.Batch(m.copyCmd(rows[cursor].field.Value), m.flashRevertTick())
}

func (m *model) detailsLines() int {
	if m.term.height <= 0 {
		return 0
	}

	lines := int(m.detailsHeight)
	if m.detailsHeight < 1 {
		lines = int(float64(m.term.height) * m.detailsHeight)
	}

	return min(lines, max(m.term.height/2-detailsChromeLines, 0))
}

// grownDetailsLines is every line the record needs once it holds the keys, as far as the rows
// left under the header and the legend allow with one kept back for the list.
func (m *model) grownDetailsLines(in screen.Inputs, held record.Details) int {
	lines := max(m.detailsLines(), m.detailsWanted(held))

	params, ok := m.activeListParams(in)
	if !ok {
		return lines
	}

	return max(min(lines, m.listRecordRoom(params)-detailsChromeLines-1), 0)
}

// detailsWanted is the lines the whole record takes: its notices, a blank under them, and every
// field its search leaves.
func (m *model) detailsWanted(held record.Details) int {
	wanted := len(m.detailsRows(held))
	if notices := m.detailsNotices(held, m.term.height); len(notices) > 0 {
		wanted += len(notices) + 1
	}

	return wanted
}
