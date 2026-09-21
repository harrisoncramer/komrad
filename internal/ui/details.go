package ui

import (
	"slices"
	"strconv"
	"strings"

	"weave/config"
	"weave/internal/ui/paint"
	"weave/lib/xslice"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// flashFieldStyle marks a row or a field that has just been copied.
func flashFieldStyle(palette paint.Palette) lipgloss.Style {
	return lipgloss.NewStyle().Background(palette.Color(paint.ToneAccent)).Foreground(lipgloss.Color("0"))
}

type detailsState struct {
	focused bool
	cursor  int
	row     string
	flashed int
	// filter is the search over the record's own rows, standing apart from the list's.
	filter filterState
	// room is the lines the record may take, measured once per update and once per frame.
	room detailsRoom
}

// detailsRoom is one measurement of the room, keyed by the pane it was taken in.
type detailsRoom struct {
	height int
	width  int
	lines  int
	taken  bool
}

type details struct {
	id      string
	notices []notice
	fields  []recordField
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
	spec := m.screen.spec().details
	if spec == nil {
		return details{}
	}

	return spec.hovered(m)
}

func (m model) isDetailsScreen() bool {
	return m.screen.spec().details != nil
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
		if m.details.filter.isNarrowing() && !m.details.filter.doesMatch(text) {
			continue
		}

		rows = append(rows, detailsRow{id: strconv.Itoa(i), field: field, text: text})
	}

	return rows
}

func (m model) detailsCursor(held details) int {
	if m.details.row != held.id {
		return 0
	}

	return min(m.details.cursor, max(len(m.detailsRows(held))-1, 0))
}

// isDetailsDrawn is whether the record is on screen: its whole share fits while the list holds
// the keys, and a row of it fits under its search line once the record has taken them or a
// search stands over it.
func (m model) isDetailsDrawn(held details) bool {
	if len(held.fields) == 0 || m.detailsShare() == 0 {
		return false
	}

	room, ok := m.detailsRoom()
	if !ok {
		return true
	}

	if m.isDetailsSearchDrawn() {
		return room-detailsSearchLine >= 1
	}

	return room >= m.detailsShare()
}

// canFocusDetails is whether the record may take the keys: it is drawn, and stays drawn under
// its search line.
func (m model) canFocusDetails(held details) bool {
	if !m.isDetailsDrawn(held) {
		return false
	}

	room, ok := m.detailsRoom()

	return !ok || room-detailsSearchLine >= 1
}

// detailsRoom is the lines the screen has for the record between its rules, measured with the
// keys on the list, and false on a screen that draws no record.
func (m model) detailsRoom() (int, bool) {
	spec := m.screen.spec().details
	if spec == nil || spec.room == nil {
		return 0, false
	}

	if memo := m.details.room; memo.taken && memo.height == m.height && memo.width == m.paneWidth() {
		return memo.lines, true
	}

	m.details.focused = false

	return spec.room(m), true
}

// measureDetails takes the room once for the frame and the keys that follow.
func (m model) measureDetails() model {
	spec := m.screen.spec().details
	if spec == nil || spec.room == nil {
		m.details.room = detailsRoom{}

		return m
	}

	measured := m
	measured.details.focused = false

	m.details.room = detailsRoom{
		height: m.height,
		width:  m.paneWidth(),
		lines:  spec.room(measured),
		taken:  true,
	}

	return m
}

// listDetailsRoom is the lines a list screen has for the record between its rules, with a row
// kept back for the list.
func (m model) listDetailsRoom(header string, help func(model) keyMap) int {
	m.details.focused = false

	return m.spareLines(header) - m.tallestLegend(help(m), header) - detailsChromeLines - 1
}

// tallestLegend is the lines the tallest legend the record can stand over takes: the screen's
// own, the record's, or the search's while one is typed at.
func (m model) tallestLegend(own keyMap, above ...string) int {
	m.details.focused = false

	tallest := 0
	for _, k := range []keyMap{own, m.detailsHelp(), filterHelp(m.details.filter.mode)} {
		tallest = max(tallest, strings.Count(m.renderHelp(k, above...), "\n"))
	}

	return tallest
}

func (m model) isDetailsFocused() bool {
	return m.details.focused && m.isDetailsDrawn(m.hoveredDetails())
}

// toggleDetailsFocus moves the keys between the list and the record, reading its directory again
// on the way in.
func (m model) toggleDetailsFocus() (model, tea.Cmd) {
	if m.isDetailsFocused() {
		return m.blurDetails(), nil
	}

	held := m.hoveredDetails()
	if !m.canFocusDetails(held) {
		return m, nil
	}

	m.details.focused = true
	m.blurTabs()
	if m.details.row != held.id {
		m.details.cursor = 0
	}
	m.details.row = held.id

	return m.refocused(), m.rereadAgentWorktreeCmd()
}

// blurDetails hands the keys back to the list, dropping a search that was still being typed.
func (m model) blurDetails() model {
	m.details.focused = false
	if m.details.filter.typing {
		m.details.filter.abandon()
	}

	return m.refocused()
}

// refocused tells the screen the keys have moved between its list and the record under it.
func (m model) refocused() model {
	spec := m.screen.spec().details
	if spec == nil || spec.refocus == nil {
		return m
	}

	spec.refocus(&m)

	return m
}

func (m model) updateDetails(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	if isBound(k, m.quitKeys()) {
		return m, m.hide()
	}

	return detailsActions().dispatch(m, k)
}

func detailsActions() actionSpec {
	standing := func(m model) bool { return m.details.filter.isStanding() }

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
				keys: func(k config.Keybindings) []string { return append(slices.Clone(k.Details.Focus), "esc") },
				run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.blurDetails(), nil },
			},
			{
				id:   actionMove,
				keys: func(k config.Keybindings) []string { return concatKeys(k.Nav.Up, k.Nav.Down) },
				run:  model.moveDetailsCursor,
			},
			{
				id:   actionJump,
				keys: func(k config.Keybindings) []string { return concatKeys(k.Nav.Top, k.Nav.Bottom) },
				run:  model.jumpDetailsCursor,
			},
			{
				id:   actionFilterOpen,
				keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
				run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.openFilter() },
			},
			{
				id:   actionFilterMatch,
				keys: func(k config.Keybindings) []string { return concatKeys(k.Filter.NextMatch, k.Filter.PrevMatch) },
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
				label: func(k config.Keybindings) string { return keyLabel(k.Details.Focus) },
				strip: true,
			},
			{
				of:   []actionID{actionFilterOpen},
				desc: model.detailsFilterDesc,
			},
			{
				of:   []actionID{actionFilterMatch},
				desc: func(model) string { return "next/prev match" },
			},
			{
				of:   []actionID{actionFilterClear},
				desc: func(m model) string { return "clear " + m.details.filter.mode.String() },
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
		},
	}
}

func (m model) detailsFilterDesc() string {
	if m.details.filter.isStanding() {
		return m.details.filter.mode.String() + ": " + m.details.filter.query
	}

	return "search"
}

func (m model) detailsHelp() keyMap {
	own := detailsActions().help(m)

	return keyMap{
		short: slices.Concat(own.short, m.globalHelp()),
		full:  slices.Concat(own.full, m.globalHelp()),
	}
}

// detailsFilterSpec is how the record's search keeps its cursor on the field it was on as the
// pattern changes under it.
func detailsFilterSpec() *filterSpec {
	return &filterSpec{
		hovered: func(m model) string {
			held := m.hoveredDetails()

			return rowKeyAt(m.detailsRows(held), m.detailsCursor(held))
		},
		place: func(m *model, hovered string) {
			held := m.hoveredDetails()
			rows := m.detailsRows(held)
			m.details.cursor = placeCursor(rowIndexOfKey(rows, hovered), m.detailsCursor(held), len(rows))
			m.details.row = held.id
		},
		rows: func(m model) []filterRow {
			return xslice.Map(m.detailsRows(m.hoveredDetails()), func(row detailsRow) filterRow {
				return filterRow{key: row.id, name: row.text}
			})
		},
	}
}

func (m model) moveDetailsCursor(pressed string) (tea.Model, tea.Cmd) {
	held := m.hoveredDetails()
	cursor := m.detailsCursor(held)
	if isBound(pressed, m.keys.Nav.Down) {
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

func (m model) setDetailsCursor(held details, idx int) model {
	m.details.cursor = clampCursor(idx, len(m.detailsRows(held)))
	m.details.row = held.id

	return m
}

func (m model) quitKeys() []string {
	spec := m.screen.spec().details
	if spec == nil {
		return m.keys.Agents.Quit
	}

	return spec.quit(m.keys)
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

// detailsShare is the field lines the record is configured to draw.
func (m model) detailsShare() int {
	if m.height <= 0 {
		return 0
	}

	lines := int(m.detailsHeight)
	if m.detailsHeight < 1 {
		lines = int(float64(m.height) * m.detailsHeight)
	}

	return min(lines, max(m.height/2-detailsChromeLines, 0))
}

// detailsLines is the field lines the record draws: its share, or every line it needs once it
// holds the keys.
func (m model) detailsLines() int {
	share := m.detailsShare()

	room, ok := m.detailsRoom()
	if !ok || !m.isDetailsSearchDrawn() {
		return share
	}

	lines := share
	if m.details.focused {
		lines = max(share, m.detailsWanted(m.hoveredDetails()))
	}

	return min(lines, max(room-detailsSearchLine, 1))
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

// detailsChromeLines is the two rules the record is drawn between.
const detailsChromeLines = 2

// detailsSearchLine is the row the record's search takes when it is drawn.
const detailsSearchLine = 1

// isDetailsSearchDrawn reports whether the record draws its search line: while it holds the keys
// or a search stands over it.
func (m model) isDetailsSearchDrawn() bool {
	return m.details.focused || m.details.filter.isStanding() || m.details.filter.typing
}

func (m model) detailsView() string {
	held := m.hoveredDetails()
	if !m.isDetailsDrawn(held) {
		return ""
	}

	// Two field lines are kept back rather than one, which is what renderList needs to say how
	// many fields it left out.
	notices := m.detailsNotices(held, m.detailsLines()-3)
	block, lines := "", m.detailsLines()
	if len(notices) > 0 {
		block = strings.Join(notices, "\n") + "\n\n"
		lines -= len(notices) + 1
	}

	cursor := m.detailsCursor(held)
	rows := m.detailFieldRows(m.detailsRows(held), cursor)
	if len(rows) == 0 && m.details.filter.isNarrowing() {
		rows = []string{dimStyle.Render("  nothing matches")}
	}

	return m.detailsRule() +
		m.detailsSearchLine() +
		block +
		paint.PadRows(paint.RenderList(rows, cursor, lines, dimStyle), lines) +
		m.detailsRule()
}

func (m model) detailsSearchLine() string {
	if !m.isDetailsSearchDrawn() {
		return ""
	}

	return " " + m.details.filter.line(paint.Panel) + "\n"
}

func (m model) detailsRule() string {
	width := m.paneWidth()
	if width <= 0 {
		return "\n"
	}

	return dimStyle.Render(strings.Repeat("─", width)) + "\n"
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
	for _, line := range text[1:] {
		lines = append(lines, indent+dimStyle.Render(line))
	}

	return lines
}

func (m model) detailFieldRows(fields []detailsRow, cursor int) []string {
	labels := labelColumn(xslice.Map(fields, func(row detailsRow) recordField { return row.field }))
	rows := make([]string, 0, len(fields))

	const gutter = 2

	for i, row := range fields {
		prefix := "  "
		if i == cursor {
			prefix = m.cursorPrefix(m.details.focused)
		}

		label := m.details.filter.highlight(paint.Panel, padRight(row.field.label, labels), dimStyle)
		value := paint.Clip(row.field.value, max(m.lineWidth()-labels-gutter-1, 1))
		switch {
		case m.details.flashed == i+1:
			value = flashFieldStyle(paint.Panel).Render(value)
		case m.details.filter.pattern != nil:
			value = m.details.filter.highlight(paint.Panel, value, lipgloss.NewStyle())
		}

		rows = append(rows, prefix+label+" "+value)
	}

	return rows
}

func (m model) lineWidth() int {
	if m.paneWidth() <= 0 {
		return nameWidth
	}

	return m.paneWidth()
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
