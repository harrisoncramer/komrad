package ui

import (
	"fmt"
	"strings"

	"weave/config"
	"weave/internal/ui/paint"
	"weave/internal/view"
	"weave/lib/xslice"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// navPageStep is how many rows a page key moves the cursor.
const navPageStep = 10

// listLines is the room the list has once the chrome around it has taken its share.
func (m model) listLines(chrome ...string) int {
	if m.height <= 0 {
		return paint.Unbounded
	}

	return max(m.spareLines(chrome...), 0)
}

// spareLines is what the pane has left once the chrome has taken its share, unclamped.
func (m model) spareLines(chrome ...string) int {
	if m.height <= 0 {
		return 0
	}

	lines := m.height
	for _, part := range chrome {
		lines -= strings.Count(part, "\n")
	}

	return lines
}

type rowCursor struct {
	cursor   int
	selected selection
	focus    string
}

func (c rowCursor) move(delta, length int) (rowCursor, bool) {
	next, ok := stepCursor(c.cursor, delta, length)
	if !ok {
		return c, false
	}
	c.cursor = next

	return c, true
}

func (c rowCursor) jump(idx, length int) (rowCursor, bool) {
	next, ok := landCursor(idx, length)
	if !ok {
		return c, false
	}
	c.cursor = next

	return c, true
}

func (m model) listHeader(title string) string {
	return m.talliedHeader(title, "")
}

// screenTitle is a listing's title: its icon and its name, set in from the pane's edge.
func screenTitle(icon view.Glyph, name string) string {
	return " " + view.Spell(icon) + "  " + name
}

// talliedHeader is a list's header with a muted tally after its title.
func (m model) talliedHeader(title, tally string) string {
	return m.headerLines(title, tally, "")
}

func (m model) headerLines(title, tally, tabs string) string {
	header := title
	if tally != "" {
		header += dimStyle.Render("  " + tally)
	}
	if width := m.paneWidth(); width > 0 {
		header = ansi.Truncate(header, width, "…")
	}

	return header + "\n\n " + m.searchLine().line(paint.Panel) + "\n" + tabs + "\n"
}

// searchLine is the search the header draws: the record's while the record has the keys, the
// list's otherwise.
func (m model) searchLine() filterState {
	if m.details.focused {
		return m.details.filter
	}

	return m.filter
}

// headedListView draws a list under its header, padded out to the record and the legend under it.
func (m model) headedListView(header string, rows []string, cursor int, help string) string {
	lines := m.listLines(header, help)

	return headedBlockView(header, paint.RenderList(rows, cursor, lines, dimStyle), lines, help)
}

// emptyListLine is what a screen says over no rows.
func (m model) emptyListLine(idle string) string {
	if m.filter.isNarrowing() {
		return "nothing matches"
	}

	return idle
}

// headedBlockView draws a list already fitted to the pane.
func headedBlockView(header, block string, lines int, help string) string {
	return header + paint.PadRows(block, lines) + help
}

// keyed is a row the cursor can be put back on by name.
type keyed interface {
	key() string
}

func rowKeyAt[T keyed](rows []T, cursor int) string {
	row, ok := rowAt(rows, cursor)
	if !ok {
		return ""
	}

	return row.key()
}

func rowIndexOfKey[T keyed](rows []T, key string) int {
	if key == "" {
		return -1
	}

	for i, r := range rows {
		if r.key() == key {
			return i
		}
	}

	return -1
}

func rowAt[T any](rows []T, cursor int) (T, bool) {
	if cursor < 0 || cursor >= len(rows) {
		var zero T

		return zero, false
	}

	return rows[cursor], true
}

// navPageDelta is how far a page key moves the cursor, signed by which of the two it is.
func (m model) navPageDelta(pressed string) int {
	if isBound(pressed, m.keys.Nav.PageUp) {
		return -navPageStep
	}

	return navPageStep
}

func stepCursor(cursor, delta, length int) (int, bool) {
	next := cursor + delta
	if next < 0 || next >= length {
		return cursor, false
	}

	return next, true
}

func landCursor(idx, length int) (int, bool) {
	if length == 0 {
		return 0, false
	}

	return clampCursor(idx, length), true
}

func clampCursor(idx, length int) int {
	return max(min(idx, length-1), 0)
}

// listSpec is what the keys every list shares need of one screen: where its cursor and rows
// live, and what follows the cursor onto a row.
type listSpec[T keyed] struct {
	cursor func(*model) *rowCursor
	rows   func(model) []T
	// name is the text a search reads a row by, nil on a list that draws no filter.
	name func(T) string
	// refs is the rows as the selection sees them, nil on a list nothing can be selected in.
	refs func([]T) []rowRef
	// header is what the toggle key names a header row as.
	header string
	// placed follows the cursor onto a row at once.
	placed func(*model)
	// moved follows the cursor onto a row a key put it on, and is placed when nil.
	moved func(model) (model, tea.Cmd)
	quit  func(config.Keybindings) []string
	back  func(config.Keybindings) []string
}

func (s listSpec[T]) isSelecting(m model) bool {
	return s.cursor(&m).selected.isActive()
}

func (s listSpec[T]) isBrowsing(m model) bool {
	return !s.isSelecting(m)
}

// hovered is the key of the row the cursor is on.
func (s listSpec[T]) hovered(m model) string {
	return rowKeyAt(s.rows(m), s.cursor(&m).cursor)
}

// place puts the cursor back on a row by key, or as near to where it was as the list allows.
func (s listSpec[T]) place(m *model, hovered string) {
	rows := s.rows(*m)
	c := s.cursor(m)
	c.cursor = placeCursor(rowIndexOfKey(rows, hovered), c.cursor, len(rows))
	s.placed(m)
}

func (s listSpec[T]) filterRows(m model) []filterRow {
	return xslice.Map(s.rows(m), func(row T) filterRow {
		return filterRow{key: row.key(), name: s.name(row)}
	})
}

func (s listSpec[T]) landed(m model) (model, tea.Cmd) {
	if s.moved != nil {
		return s.moved(m)
	}

	s.placed(&m)

	return m, nil
}

func (s listSpec[T]) move(m model, delta int) (model, tea.Cmd) {
	next, ok := s.cursor(&m).move(delta, len(s.rows(m)))
	if !ok {
		return m, nil
	}

	*s.cursor(&m) = next

	return s.landed(m)
}

func (s listSpec[T]) jump(m model, idx int) (model, tea.Cmd) {
	next, ok := s.cursor(&m).jump(idx, len(s.rows(m)))
	if !ok {
		return m, nil
	}

	*s.cursor(&m) = next

	return s.landed(m)
}

// update settles the doubled top key ahead of the table.
func (s listSpec[T]) update(m model, key tea.KeyMsg, actions actionSpec) (tea.Model, tea.Cmd) {
	k := key.String()

	if m.takeTopPress(k) {
		return s.jump(m, 0)
	}
	if m.topHeld {
		return m, nil
	}

	return actions.dispatch(m, k)
}

// actions are the keys every list answers, in dispatch order.
func (s listSpec[T]) actions() []action {
	shared := []action{
		{
			id:   actionJump,
			keys: func(k config.Keybindings) []string { return concatKeys(k.Nav.Top, k.Nav.Bottom) },
			run:  s.jumpPressed,
		},
	}
	if s.refs != nil {
		shared = append(shared, s.selectionActions()...)
	}
	shared = append(shared,
		action{
			id:   actionQuit,
			keys: s.quit,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m, m.hide() },
		},
		action{
			id:   actionMove,
			keys: func(k config.Keybindings) []string { return concatKeys(k.Nav.Up, k.Nav.Down) },
			run:  s.movePressed,
		},
		action{
			id:   actionNavPage,
			keys: func(k config.Keybindings) []string { return concatKeys(k.Nav.PageUp, k.Nav.PageDown) },
			run:  s.pagePressed,
		},
	)
	if s.name != nil {
		shared = append(shared, s.filterActions()...)
	}

	return shared
}

func (s listSpec[T]) selectionActions() []action {
	return []action{
		{
			id:   actionSelectClear,
			keys: func(k config.Keybindings) []string { return k.Selection.Clear },
			when: s.isSelecting,
			run:  s.clearSelection,
		},
		{
			id:   actionSelectToggle,
			keys: func(k config.Keybindings) []string { return k.Selection.Toggle },
			run:  s.toggleSelection,
		},
		{
			id:   actionSelectRange,
			keys: func(k config.Keybindings) []string { return k.Selection.Range },
			run:  s.extendSelection,
		},
	}
}

func (s listSpec[T]) filterActions() []action {
	standing := func(m model) bool { return s.isBrowsing(m) && m.filter.isStanding() }

	return []action{
		{
			id:   actionFilterOpen,
			keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
			when: s.isBrowsing,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.openFilter() },
		},
		{
			id:   actionFilterMatch,
			keys: func(k config.Keybindings) []string { return concatKeys(k.Filter.NextMatch, k.Filter.PrevMatch) },
			when: standing,
			run:  model.jumpFilterMatch,
		},
		{
			id:   actionFilterClear,
			keys: s.back,
			when: standing,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.clearSearch() },
		},
	}
}

// jumpPressed answers the bottom key.
func (s listSpec[T]) jumpPressed(m model, pressed string) (tea.Model, tea.Cmd) {
	if isBound(pressed, m.keys.Nav.Bottom) {
		return s.jump(m, len(s.rows(m))-1)
	}

	return m, nil
}

func (s listSpec[T]) pagePressed(m model, pressed string) (tea.Model, tea.Cmd) {
	return s.jump(m, s.cursor(&m).cursor+m.navPageDelta(pressed))
}

func (s listSpec[T]) movePressed(m model, pressed string) (tea.Model, tea.Cmd) {
	if isBound(pressed, m.keys.Nav.Down) {
		return s.move(m, 1)
	}

	return s.move(m, -1)
}

func (s listSpec[T]) clearSelection(m model, _ string) (tea.Model, tea.Cmd) {
	s.cursor(&m).selected = selection{}

	return m, nil
}

func (s listSpec[T]) toggleSelection(m model, _ string) (tea.Model, tea.Cmd) {
	c := s.cursor(&m)
	c.selected = c.selected.toggle(s.refs(s.rows(m)), c.cursor)

	return m, nil
}

func (s listSpec[T]) extendSelection(m model, pressed string) (tea.Model, tea.Cmd) {
	if !s.isSelecting(m) {
		return s.toggleSelection(m, pressed)
	}

	c := s.cursor(&m)
	c.selected = c.selected.extend(s.refs(s.rows(m)), c.cursor)

	return m, nil
}

// selectionLegend is the three rows a standing selection is named by.
func (s listSpec[T]) selectionLegend() []legendRow {
	return []legendRow{
		{
			of:    []actionID{actionSelectToggle},
			desc:  s.toggleDesc,
			show:  s.isSelecting,
			strip: true,
		},
		{
			of:   []actionID{actionSelectRange},
			desc: func(model) string { return "select to nearest" },
			show: s.isSelecting,
		},
		{
			of:    []actionID{actionSelectClear},
			desc:  s.clearDesc,
			show:  s.isSelecting,
			strip: true,
		},
	}
}

func (s listSpec[T]) toggleDesc(m model) string {
	c := s.cursor(&m)
	hovered, _ := rowAt(s.refs(s.rows(m)), c.cursor)

	switch {
	case hovered.header:
		return "select " + s.header
	case c.selected.has(hovered.key):
		return "deselect"
	}

	return "select"
}

func (s listSpec[T]) clearDesc(m model) string {
	return fmt.Sprintf("clear %d selected", len(s.cursor(&m).selected.keys))
}

// navLegend is the three rows the cursor keys are named by.
func navLegend() []legendRow {
	return []legendRow{
		{
			of:    []actionID{actionMove},
			desc:  func(model) string { return "move" },
			label: moveLabel,
		},
		{
			of:    []actionID{actionJump},
			desc:  func(model) string { return "top/bottom" },
			label: jumpLabel,
		},
		{
			of:    []actionID{actionNavPage},
			desc:  func(model) string { return "page" },
			label: navPageLabel,
		},
	}
}

// listChrome is the frame a list is drawn in: its title, its tally and its legend.
type listChrome struct {
	title string
	tally func(model) string
	help  func(model) keyMap
}

func (c listChrome) tallyOf(m model) string {
	if c.tally == nil {
		return ""
	}

	return c.tally(m)
}

func (c listChrome) header(m model) string {
	return m.tabbedHeader(c.title, c.tallyOf(m))
}

func (c listChrome) doesFitTabs(m model) bool {
	return m.doesListFitTabs(c.title, c.tallyOf(m), c.help)
}

func (c listChrome) doesFitDetails(m model) bool {
	return m.doesListFitDetails(c.header(m), c.help)
}

func (c listChrome) detailsRoom(m model) int {
	return m.listDetailsRoom(c.header(m), c.help)
}

type listViewParams struct {
	chrome listChrome
	// empty is what the list says over no rows, and blank on a list that draws its window anyway.
	empty string
	// rows is how many rows the list holds.
	rows     int
	snapshot func(model, int) view.Snapshot
}

// listView draws a list under its header with its record and its legend under it.
func (m model) listView(params listViewParams) string {
	header := params.chrome.header(m)
	record, help := m.detailsView(), m.renderHelp(params.chrome.help(m), header)
	below := record + m.messagesRegion(record, help, header) + help

	if params.rows == 0 && (params.empty != "" || m.filter.isNarrowing()) {
		return m.headedListView(header, []string{dimStyle.Render(m.emptyListLine(params.empty))}, 0, below)
	}

	pane := m.paneWidth()
	lines := m.listLines(header, below)
	window := view.Window(view.WindowParams{
		List:    view.Prepare(params.snapshot(m, pane)),
		Palette: paint.Panel,
		Width:   pane,
		Lines:   lines,
	})

	return headedBlockView(header, window, lines, below)
}
