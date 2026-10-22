package ui

import (
	"slices"
	"strconv"
	"strings"

	"komrad/config"
	"komrad/internal/ui/cursor"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/list"

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

type details struct {
	id      string
	notices []notice
	fields  []recordField
	tags    []string
}

// detailsRow is one field as the record renders it, keyed by its place in the whole record.
type detailsRow struct {
	id    string
	field recordField
	// text is what a search reads the row by, which for a row under a label is that label and
	// the row's own value.
	text string
}

func (r detailsRow) Key() string {
	return r.id
}

func (m *model) hoveredDetails() details {
	d, ok := m.active().(recordRenderer)
	if !ok {
		return details{}
	}

	return d.details(m)
}

func (m *model) isDetailsScreen() bool {
	_, ok := m.active().(recordRenderer)

	return ok
}

// detailsRows is the record's fields as its search leaves them.
func (m *model) detailsRows(held details) []detailsRow {
	rows := make([]detailsRow, 0, len(held.fields))
	group := ""
	for i, field := range held.fields {
		if field.label != "" {
			group = field.label
		}

		text := strings.TrimSpace(group + " " + field.value)
		if m.filter.doesNarrow(sectionRecord) && !m.filter.doesMatch(text) {
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

func (m *model) detailsCursor(held details) int {
	if m.details.held != held.id {
		return 0
	}

	return cursor.Clamp(m.details.Row, len(m.detailsRows(held)))
}

func (m *model) detailsList() screenList[detailsRow] {
	return screenList[detailsRow]{
		cursor: m.detailsRowCursor,
		rows:   func(m *model) []detailsRow { return m.detailsRows(m.hoveredDetails()) },
		name:   func(row detailsRow) string { return row.text },
	}
}

func (m *model) detailsRowCursor() *list.Cursor {
	if held := m.hoveredDetails(); m.details.held != held.id {
		m.details.Cursor = list.Cursor{}
		m.details.held = held.id
	}

	return &m.details.Cursor
}

func (m *model) isDetailsRendered(held details) bool {
	if len(held.fields) == 0 || m.detailsLines() == 0 {
		return false
	}

	d, ok := m.active().(recordRenderer)
	if !ok {
		return true
	}

	return m.detailsFits.read(m.shownScreen(), func() bool { return d.detailsFit(m) })
}

// listFitsDetails is whether the record's share fits under a list, judged with the keys on the
// list.
func (m *model) listFitsDetails(params screenListParams) bool {
	probe := *m
	probe.details.focused = false
	header := probe.tabbedHeader(params.title, params.tally)
	legend := probe.renderHelp(probe.listHelpOf(params), header)

	// Only the list's legend is measured with the search; a focused record shrinks to the room
	// its own legend leaves.
	probe.filter = filterState{}
	if focused := probe.renderHelp(probe.detailsHelp(), header); strings.Count(focused, "\n") > strings.Count(legend, "\n") {
		legend = focused
	}

	return probe.spareLines(header, legend) > probe.detailsLines()+detailsChromeLines
}

// listRecordRoom is the lines a list screen has under its header and the legend it is rendering.
func (m *model) listRecordRoom(params screenListParams) int {
	header := m.tabbedHeader(params.title, params.tally)

	return m.spareLines(header, m.renderHelp(m.listHelpOf(params), header))
}

func (m *model) isDetailsFocused() bool {
	return m.details.focused && m.isDetailsRendered(m.hoveredDetails())
}

// focusDetails gives the record the keys, reading its directory again on the way in.
func (m *model) focusDetails() tea.Cmd {
	held := m.hoveredDetails()
	if !m.isDetailsRendered(held) {
		return nil
	}

	m.details.focused = true
	m.blurTabs()
	m.blurKinds()
	m.detailsRowCursor()

	return tea.Batch(m.rereadTaskWorktreeCmd(), m.wakeCwdSyncCmd())
}

// releaseHiddenDetails hands the keys back from a record the pane no longer renders.
func (m *model) releaseHiddenDetails() {
	if !m.details.focused || m.isDetailsRendered(m.hoveredDetails()) {
		return
	}

	m.blurDetails()
}

// blurDetails hands the keys back to the list.
func (m *model) blurDetails() {
	m.details.focused = false
}

func (m *model) updateDetails(key tea.KeyPressMsg) tea.Cmd {
	return m.detailsActions().Dispatch(m, m.keys, key.String())
}

func (m *model) detailsActions() keys.Spec[*model] {
	return keys.Spec[*model]{
		Table: append(m.detailsList().actions(), []keys.Action[*model]{
			{
				ID:   keys.Details,
				Keys: keys.FixedBack,
				Nav: func(*model, string) tea.Cmd {
					m.details.focused = false

					return nil
				},
			},
			{
				ID:   keys.DetailsCopy,
				Keys: func(k config.Keybindings) []string { return k.Details.Copy },
				Nav:  func(*model, string) tea.Cmd { return m.copyDetailsField() },
			},
		}...),
		Legend: []keys.LegendRow[*model]{
			{
				Of:    []keys.ID{keys.DetailsCopy},
				Desc:  func(*model) string { return "copy field" },
				Strip: true,
			},
			{
				Of:    []keys.ID{keys.Details},
				Desc:  func(*model) string { return "back to the list" },
				Strip: true,
			},
			{
				Of:   []keys.ID{keys.FilterOpen},
				Desc: func(*model) string { return m.filterDesc() },
			},
			{
				Of:   []keys.ID{keys.FilterMatch},
				Desc: func(*model) string { return "next/prev match" },
			},
			{
				Of:   []keys.ID{keys.FilterClear},
				Desc: func(*model) string { return m.filterClearDesc() },
			},
			{
				Of:    []keys.ID{keys.Move},
				Desc:  func(*model) string { return "move" },
				Label: keys.MoveLabel,
			},
			{
				Of:    []keys.ID{keys.Jump},
				Desc:  func(*model) string { return "top/bottom" },
				Label: func(k config.Keybindings) string { return keys.Label(k.Nav.Top) + "/" + keys.Label(k.Nav.Bottom) },
			},
			{
				Of:    []keys.ID{keys.NavPage},
				Desc:  func(*model) string { return "page" },
				Label: keys.NavPageLabel,
			},
			{
				Of:    []keys.ID{keys.ColumnScroll},
				Desc:  func(*model) string { return "scroll" },
				Label: keys.ColumnScrollLabel,
			},
		},
	}
}

func (m *model) detailsHelp() keyMap {
	own := m.helpOf(m.detailsActions())

	return keyMap{
		short: slices.Concat(own.short, m.globalHelp()),
		full:  slices.Concat(own.full, m.fullGlobalHelp()),
	}
}

func (m *model) copyDetailsField() tea.Cmd {
	held := m.hoveredDetails()
	rows := m.detailsRows(held)
	if len(rows) == 0 {
		return nil
	}

	cursor := m.detailsCursor(held)
	m.details.flashed = cursor + 1

	return tea.Batch(m.copyCmd(rows[cursor].field.value), m.flashRevertTick())
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
func (m *model) grownDetailsLines(held details) int {
	lines := max(m.detailsLines(), m.detailsWanted(held))

	d, ok := m.active().(recordRenderer)
	if !ok {
		return lines
	}

	return max(min(lines, d.detailsRoom(m)-detailsChromeLines-1), 0)
}

// detailsWanted is the lines the whole record takes: its notices, a blank under them, and every
// field its search leaves.
func (m *model) detailsWanted(held details) int {
	wanted := len(m.detailsRows(held))
	if notices := m.detailsNotices(held, m.term.height); len(notices) > 0 {
		wanted += len(notices) + 1
	}

	return wanted
}
