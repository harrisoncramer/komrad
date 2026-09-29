package ui

import (
	"slices"
	"strconv"
	"strings"

	"komrad/config"
	"komrad/internal/glyph"
	"komrad/internal/ui/cursor"
	"komrad/internal/ui/paint"
	"komrad/internal/view"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const detailsChromeLines = 2

const legendRuleFloor = 2

// flashFieldStyle marks a row or a field that has just been copied.
func flashFieldStyle(palette paint.Palette) lipgloss.Style {
	return lipgloss.NewStyle().Background(palette.Color(paint.ToneAccent)).Foreground(lipgloss.Color("0"))
}

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
	tag     string
}

// detailsRow is one field as the record draws it, keyed by its place in the whole record.
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

func (m model) hoveredDetails() details {
	d, ok := m.active().(detailed)
	if !ok {
		return details{}
	}

	return d.details(m.shared())
}

func (m model) isDetailsScreen() bool {
	_, ok := m.active().(detailed)

	return ok
}

// detailsRows is the record's fields as its search leaves them.
func (m model) detailsRows(held details) []detailsRow {
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

func (m model) detailsCursor(held details) int {
	if m.details.row != held.id {
		return 0
	}

	return cursor.Clamp(m.details.cursor, len(m.detailsRows(held)))
}

func (m model) isDetailsDrawn(held details) bool {
	if len(held.fields) == 0 || m.detailsLines() == 0 {
		return false
	}

	d, ok := m.active().(detailed)
	if !ok {
		return true
	}

	return d.detailsFit(m.shared())
}

// listFitsDetails is whether the record's share fits under a list, judged with the keys on the
// list.
func (m model) listFitsDetails(params screenListParams) bool {
	m.details.focused = false
	header := m.tabbedHeader(params.title, params.tally)
	legend := m.renderHelp(m.listHelpOf(params), header)

	// Only the list's legend is measured with the search; a focused record shrinks to the room
	// its own legend leaves.
	m.filter = filterState{}
	if focused := m.renderHelp(m.detailsHelp(), header); strings.Count(focused, "\n") > strings.Count(legend, "\n") {
		legend = focused
	}

	return m.spareLines(header, legend) > m.detailsLines()+detailsChromeLines
}

// listRecordRoom is the lines a list screen has under its header and the legend it is drawing.
func (m model) listRecordRoom(params screenListParams) int {
	header := m.tabbedHeader(params.title, params.tally)

	return m.spareLines(header, m.renderHelp(m.listHelpOf(params), header))
}

func (m model) isDetailsFocused() bool {
	return m.details.focused && m.isDetailsDrawn(m.hoveredDetails())
}

// focusDetails gives the record the keys, reading its directory again on the way in.
func (m model) focusDetails() (model, tea.Cmd) {
	held := m.hoveredDetails()
	if !m.isDetailsDrawn(held) {
		return m, nil
	}

	m.details.focused = true
	m.blurTabs()
	m.blurKinds()
	if m.details.row != held.id {
		m.details.cursor = 0
	}
	m.details.row = held.id

	return m, m.rereadAgentWorktreeCmd()
}

// releaseHiddenDetails hands the keys back from a record the pane no longer draws.
func (m model) releaseHiddenDetails() model {
	if !m.details.focused || m.isDetailsDrawn(m.hoveredDetails()) {
		return m
	}

	return m.blurDetails()
}

// blurDetails hands the keys back to the list.
func (m model) blurDetails() model {
	m.details.focused = false

	return m
}

func (m model) updateDetails(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	if isBound(k, m.quitKeys()) {
		return m, m.hide()
	}

	return detailsActions().dispatch(m, k)
}

func detailsActions() actionSpec {
	standing := func(m model) bool { return m.filter.isStanding() }

	return actionSpec{
		table: []action{
			{
				id:   actionFilterClear,
				keys: func(config.Keybindings) []string { return []string{"esc"} },
				when: standing,
				run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.clearSearch() },
			},
			{
				id:   actionDetails,
				keys: func(config.Keybindings) []string { return []string{"esc"} },
				run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.blurDetails(), nil },
			},
			{
				id:   actionMove,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.Up, k.Nav.Down) },
				run:  model.moveDetailsCursor,
			},
			{
				id:   actionJump,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.Top, k.Nav.Bottom) },
				run:  model.jumpDetailsCursor,
			},
			{
				id:   actionNavPage,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.PageUp, k.Nav.PageDown) },
				run:  model.pageDetailsCursor,
			},
			{
				id:   actionColumnScroll,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Column.ScrollDown, k.Column.ScrollUp) },
				run:  model.moveDetailsCursor,
			},
			{
				id:   actionFilterOpen,
				keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
				run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.openFilter() },
			},
			{
				id:   actionFilterMatch,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Filter.NextMatch, k.Filter.PrevMatch) },
				when: standing,
				run:  model.jumpFilterMatch,
			},
			{
				id:   actionDetailsCopy,
				keys: func(k config.Keybindings) []string { return k.Details.Copy },
				run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.copyDetailsField() },
			},
		},
		legend: []legendRow{
			{
				of:    []actionID{actionDetailsCopy},
				desc:  func(model) string { return "copy field" },
				strip: true,
			},
			{
				of:    []actionID{actionDetails},
				desc:  func(model) string { return "back to the list" },
				strip: true,
			},
			{
				of:   []actionID{actionFilterOpen},
				desc: model.filterDesc,
			},
			{
				of:   []actionID{actionFilterMatch},
				desc: func(model) string { return "next/prev match" },
			},
			{
				of:   []actionID{actionFilterClear},
				desc: model.filterClearDesc,
			},
			{
				of:    []actionID{actionMove},
				desc:  func(model) string { return "move" },
				label: func(k config.Keybindings) string { return keyLabel(k.Nav.Up) + "/" + keyLabel(k.Nav.Down) },
			},
			{
				of:    []actionID{actionJump},
				desc:  func(model) string { return "top/bottom" },
				label: func(k config.Keybindings) string { return keyLabel(k.Nav.Top) + "/" + keyLabel(k.Nav.Bottom) },
			},
			{
				of:    []actionID{actionNavPage},
				desc:  func(model) string { return "page" },
				label: navPageLabel,
			},
			{
				of:    []actionID{actionColumnScroll},
				desc:  func(model) string { return "scroll" },
				label: columnScrollLabel,
			},
		},
	}
}

func (m model) detailsHelp() keyMap {
	own := detailsActions().help(m)

	return keyMap{
		short: slices.Concat(own.short, m.globalHelp()),
		full:  slices.Concat(own.full, m.fullGlobalHelp()),
	}
}

// hoveredDetailsKey is the field the record's cursor is on, read before the search's pattern
// changes.
func (m model) hoveredDetailsKey() string {
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
func (m model) detailsFilterRows() []filterRow {
	return xslice.Map(m.detailsRows(m.hoveredDetails()), func(row detailsRow) filterRow {
		return filterRow{key: row.id, name: row.text}
	})
}

// moveDetailsCursor steps the field cursor.
func (m model) moveDetailsCursor(pressed string) (tea.Model, tea.Cmd) {
	held := m.hoveredDetails()
	cursor := m.detailsCursor(held)
	if isBound(pressed, m.keys.Nav.Down) || isBound(pressed, m.keys.Column.ScrollDown) {
		return m.setDetailsCursor(held, cursor+1), nil
	}

	return m.setDetailsCursor(held, cursor-1), nil
}

func (m model) jumpDetailsCursor(pressed string) (tea.Model, tea.Cmd) {
	held := m.hoveredDetails()
	if isBound(pressed, m.keys.Nav.Bottom) {
		return m.setDetailsCursor(held, len(m.detailsRows(held))-1), nil
	}

	return m.setDetailsCursor(held, 0), nil
}

func (m model) pageDetailsCursor(pressed string) (tea.Model, tea.Cmd) {
	held := m.hoveredDetails()

	return m.setDetailsCursor(held, m.detailsCursor(held)+m.navPageDelta(pressed)), nil
}

func (m model) setDetailsCursor(held details, idx int) model {
	m.details.cursor = cursor.Clamp(idx, len(m.detailsRows(held)))
	m.details.row = held.id

	return m
}

func (m model) quitKeys() []string {
	d, ok := m.active().(detailed)
	if !ok {
		return m.keys.Agents.Quit
	}

	return d.detailsQuit(m.keys)
}

func (m model) copyDetailsField() (tea.Model, tea.Cmd) {
	held := m.hoveredDetails()
	rows := m.detailsRows(held)
	if len(rows) == 0 {
		return m, nil
	}

	cursor := m.detailsCursor(held)
	m.details.flashed = cursor + 1

	return m, tea.Batch(m.copyCmd(rows[cursor].field.value), flashRevertTick())
}

func (m model) detailsLines() int {
	if m.height <= 0 {
		return 0
	}

	lines := int(m.detailsHeight)
	if m.detailsHeight < 1 {
		lines = int(float64(m.height) * m.detailsHeight)
	}

	return min(lines, max(m.height/2-detailsChromeLines, 0))
}

// grownDetailsLines is every line the record needs once it holds the keys, as far as the rows
// left under the header and the legend allow with one kept back for the list.
func (m model) grownDetailsLines(held details) int {
	lines := max(m.detailsLines(), m.detailsWanted(held))

	d, ok := m.active().(detailed)
	if !ok {
		return lines
	}

	return max(min(lines, d.detailsRoom(m.shared())-detailsChromeLines-1), 0)
}

// detailsWanted is the lines the whole record takes: its notices, a blank under them, and every
// field its search leaves.
func (m model) detailsWanted(held details) int {
	wanted := len(m.detailsRows(held))
	if notices := m.detailsNotices(held, m.height); len(notices) > 0 {
		wanted += len(notices) + 1
	}

	return wanted
}

func (m model) detailsView() string {
	held := m.hoveredDetails()
	if !m.isDetailsDrawn(held) {
		return ""
	}

	lines := m.detailsLines()
	if m.details.focused {
		lines = m.grownDetailsLines(held)
	}
	if lines == 0 {
		return ""
	}

	// Two field lines are kept back rather than one.
	notices := m.detailsNotices(held, lines-3)
	block := ""
	if len(notices) > 0 {
		block = strings.Join(notices, "\n") + "\n\n"
		lines -= len(notices) + 1
	}

	cursor := m.detailsCursor(held)
	rows := m.detailFieldRows(m.detailsRows(held), cursor)
	if len(rows) == 0 && m.filter.doesNarrow(sectionRecord) {
		rows = []string{dimStyle.Render("  nothing matches")}
	}

	return m.detailsRule(held.tag) +
		block +
		paint.PadRows(paint.RenderList(rows, cursor, lines, dimStyle), lines) +
		m.detailsRule("")
}

func (m model) drawRecord(above ...string) string {
	if record := m.detailsView(); record != "" {
		return record
	}

	rule := m.detailsRule("")
	if m.height > 0 && m.spareLines(append(slices.Clone(above), rule)...) < legendRuleFloor {
		return ""
	}

	return rule
}

func (m model) detailsRule(tag string) string {
	width := m.paneWidth()
	if width <= 0 {
		return "\n"
	}

	tagWidth := lipgloss.Width(tag)
	if tag == "" || tagWidth+4 > width {
		return dimStyle.Render(strings.Repeat(glyph.Rule, width)) + "\n"
	}

	return dimStyle.Render(strings.Repeat(glyph.Rule, width-tagWidth-3)+" ") + tag + dimStyle.Render(" "+glyph.Rule) + "\n"
}

// detailsNotices are the lines the marks on the row take above the fields, giving up a notice
// that does not fit rather than the fields under it.
func (m model) detailsNotices(held details, budget int) []string {
	if budget <= 0 {
		return nil
	}

	drawn := []string{}
	for _, n := range held.notices {
		lines := m.noticeLines(n, 0)
		if len(drawn)+len(lines) > budget {
			// The first notice is cut short rather than given up.
			if len(drawn) > 0 {
				break
			}

			lines = m.noticeLines(n, budget)
		}

		drawn = append(drawn, lines...)
	}

	return drawn
}

// noticeLines is one notice as it is drawn: the glyph the row carries, then the sentence wrapped
// under it in the column the glyph left, cut off at a number of lines when given one.
func (m model) noticeLines(n notice, most int) []string {
	const indent = "    "

	// A notice that is going to be cut keeps a cell back for the ellipsis.
	width := m.lineWidth() - len(indent)
	if most > 0 {
		width--
	}

	text := paint.WrapRows(n.text, width)
	if most > 0 && len(text) > most {
		text = text[:most]
		text[most-1] += "…"
	}

	icon := " "
	if n.icon != "" {
		icon = paint.Panel.Style(n.tone).Render(n.icon)
	}

	lines := []string{"  " + icon + " " + dimStyle.Render(text[0])}
	lines = append(lines, xslice.Map(text[1:], func(line string) string {
		return indent + dimStyle.Render(line)
	})...)

	return lines
}

func (m model) detailFieldRows(fields []detailsRow, cursor int) []string {
	labels := labelColumn(xslice.Map(fields, func(row detailsRow) recordField { return row.field }))
	rows := make([]string, 0, len(fields))

	const gutter = 2

	for i, row := range fields {
		prefix, labelStyle := "  ", dimStyle
		if i == cursor {
			prefix = m.cursorPrefix(sectionRecord)
			if m.caretOf(sectionRecord) != view.CaretAbsent {
				labelStyle = rowCursorStyle()
			}
		}

		label := m.filter.highlight(paint.Panel, padRight(row.field.label, labels), labelStyle)
		width := max(m.lineWidth()-labels-gutter-1, 1)
		value := paint.Clip(row.field.value, width)
		switch {
		case m.details.flashed == i+1:
			value = flashFieldStyle(paint.Panel).Render(value)
		case m.filter.pattern != nil:
			value = m.filter.highlight(paint.Panel, value, lipgloss.NewStyle())
		case row.field.styled != "":
			value = ansi.Truncate(row.field.styled, width, "…")
		}

		rows = append(rows, prefix+label+" "+value)
	}

	return rows
}

func (m model) lineWidth() int {
	return lineWidthOf(m.paneWidth())
}

// lineWidthOf is the column a line of prose is wrapped into, in a pane of the width given.
func lineWidthOf(pane int) int {
	if pane <= 0 {
		return nameWidth
	}

	return pane
}

func labelColumn(fields []recordField) int {
	widest := 0
	for _, field := range fields {
		widest = max(widest, len([]rune(field.label)))
	}

	return widest
}

func padRight(text string, width int) string {
	pad := width - len([]rune(text))
	if pad <= 0 {
		return text
	}

	return text + strings.Repeat(" ", pad)
}
