package ui

import (
	"slices"
	"strconv"
	"strings"

	"komrad/config"
	"komrad/internal/ui/cursor"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
)

const detailsChromeLines = 2

const legendRuleFloor = 2

type detailsState struct {
	focused bool
	cursor  int
	row     string
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

func (r detailsRow) key() string {
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
	if m.details.row != held.id {
		return 0
	}

	return cursor.Clamp(m.details.cursor, len(m.detailsRows(held)))
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
	if m.details.row != held.id {
		m.details.cursor = 0
	}
	m.details.row = held.id

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
	k := key.String()

	if isBound(k, fixedKeys().Quit) {
		return m.hide()
	}

	return m.detailsActions().dispatch(m, k)
}

func (m *model) detailsActions() screenActionSpec {
	standing := func(*model) bool { return m.filter.isStanding() }

	return screenActionSpec{
		table: []screenAction{
			{
				id:   actionFilterClear,
				keys: fixedBack,
				when: standing,
				nav: func(*model, string) tea.Cmd {
					m.clearFilter()

					return nil
				},
			},
			{
				id:   actionDetails,
				keys: fixedBack,
				nav: func(*model, string) tea.Cmd {
					m.details.focused = false

					return nil
				},
			},
			{
				id:   actionMove,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.Up, k.Nav.Down) },
				nav: func(_ *model, pressed string) tea.Cmd {
					m.moveDetailsCursor(pressed)

					return nil
				},
			},
			{
				id:   actionJump,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.Top, k.Nav.Bottom) },
				nav: func(_ *model, pressed string) tea.Cmd {
					m.jumpDetailsCursor(pressed)

					return nil
				},
			},
			{
				id:   actionNavPage,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.PageUp, k.Nav.PageDown) },
				nav: func(_ *model, pressed string) tea.Cmd {
					m.pageDetailsCursor(pressed)

					return nil
				},
			},
			{
				id:   actionColumnScroll,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Column.ScrollDown, k.Column.ScrollUp) },
				nav: func(_ *model, pressed string) tea.Cmd {
					m.moveDetailsCursor(pressed)

					return nil
				},
			},
			{
				id:   actionFilterOpen,
				keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
				nav: func(*model, string) tea.Cmd {
					m.beginFilter()

					return nil
				},
			},
			{
				id:   actionFilterMatch,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Filter.NextMatch, k.Filter.PrevMatch) },
				when: standing,
				nav:  func(_ *model, pressed string) tea.Cmd { return m.jumpToFilterMatch(pressed) },
			},
			{
				id:   actionDetailsCopy,
				keys: func(k config.Keybindings) []string { return k.Details.Copy },
				nav:  func(*model, string) tea.Cmd { return m.copyDetailsField() },
			},
		},
		legend: []screenLegendRow{
			{
				of:    []actionID{actionDetailsCopy},
				desc:  func(*model) string { return "copy field" },
				strip: true,
			},
			{
				of:    []actionID{actionDetails},
				desc:  func(*model) string { return "back to the list" },
				strip: true,
			},
			{
				of:   []actionID{actionFilterOpen},
				desc: func(*model) string { return m.filterDesc() },
			},
			{
				of:   []actionID{actionFilterMatch},
				desc: func(*model) string { return "next/prev match" },
			},
			{
				of:   []actionID{actionFilterClear},
				desc: func(*model) string { return m.filterClearDesc() },
			},
			{
				of:    []actionID{actionMove},
				desc:  func(*model) string { return "move" },
				label: func(k config.Keybindings) string { return keyLabel(k.Nav.Up) + "/" + keyLabel(k.Nav.Down) },
			},
			{
				of:    []actionID{actionJump},
				desc:  func(*model) string { return "top/bottom" },
				label: func(k config.Keybindings) string { return keyLabel(k.Nav.Top) + "/" + keyLabel(k.Nav.Bottom) },
			},
			{
				of:    []actionID{actionNavPage},
				desc:  func(*model) string { return "page" },
				label: navPageLabel,
			},
			{
				of:    []actionID{actionColumnScroll},
				desc:  func(*model) string { return "scroll" },
				label: columnScrollLabel,
			},
		},
	}
}

func (m *model) detailsHelp() keyMap {
	own := m.detailsActions().help(m)

	return keyMap{
		short: slices.Concat(own.short, m.globalHelp()),
		full:  slices.Concat(own.full, m.fullGlobalHelp()),
	}
}

// hoveredDetailsKey is the field the record's cursor is on, read before the search's pattern
// changes.
func (m *model) hoveredDetailsKey() string {
	held := m.hoveredDetails()

	return rowKeyAt(m.detailsRows(held), m.detailsCursor(held))
}

// placeDetailsAt puts the record's cursor back on a field, or as near to where it was as the
// narrowed record allows.
func (m *model) placeDetailsAt(key string) {
	held := m.hoveredDetails()
	rows := m.detailsRows(held)
	m.details.cursor = cursor.Place(rowIndexOfKey(rows, key), m.detailsCursor(held), len(rows))
	m.details.row = held.id
}

// detailsFilterRows is the record's fields as the search drives them.
func (m *model) detailsFilterRows() []filterRow {
	return xslice.Map(m.detailsRows(m.hoveredDetails()), func(row detailsRow) filterRow {
		return filterRow{key: row.id, name: row.text}
	})
}

// moveDetailsCursor steps the field cursor.
func (m *model) moveDetailsCursor(pressed string) {
	held := m.hoveredDetails()
	cursor := m.detailsCursor(held)
	if isBound(pressed, m.keys.Nav.Down) || isBound(pressed, m.keys.Column.ScrollDown) {
		m.setDetailsCursor(held, cursor+1)

		return
	}

	m.setDetailsCursor(held, cursor-1)
}

func (m *model) jumpDetailsCursor(pressed string) {
	held := m.hoveredDetails()
	if isBound(pressed, m.keys.Nav.Bottom) {
		m.setDetailsCursor(held, len(m.detailsRows(held))-1)

		return
	}

	m.setDetailsCursor(held, 0)
}

func (m *model) pageDetailsCursor(pressed string) {
	held := m.hoveredDetails()
	m.setDetailsCursor(held, m.detailsCursor(held)+m.navPageDelta(pressed))
}

func (m *model) setDetailsCursor(held details, idx int) {
	m.details.cursor = cursor.Clamp(idx, len(m.detailsRows(held)))
	m.details.row = held.id
}

func (m *model) copyDetailsField() tea.Cmd {
	held := m.hoveredDetails()
	rows := m.detailsRows(held)
	if len(rows) == 0 {
		return nil
	}

	cursor := m.detailsCursor(held)
	m.details.flashed = cursor + 1

	return tea.Batch(m.copyCmd(rows[cursor].field.value), flashRevertTick())
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
