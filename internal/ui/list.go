package ui

import (
	"strings"

	"weave/config"
	"weave/internal/ui/paint"
	"weave/internal/view"

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

	return header + "\n\n " + m.filter.line(paint.Panel) + "\n" + tabs + "\n"
}

// headedListView draws a list under its header, padded out to the record and the legend under it.
func (m model) headedListView(header string, rows []string, cursor int, help string) string {
	lines := m.listLines(header, help)

	return headedBlockView(header, paint.RenderList(rows, cursor, lines, dimStyle), lines, help)
}

// emptyListLine is what a screen says over no rows.
func (m model) emptyListLine(idle string) string {
	return emptyLineOf(m.filter, idle)
}

// emptyLineOf is what a list says over no rows, under the search standing in it.
func emptyLineOf(filter filterState, idle string) string {
	if filter.doesNarrow(sectionList) {
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
	return navPageDeltaOf(m.keys, pressed)
}

func navPageDeltaOf(keys config.Keybindings, pressed string) int {
	if isBound(pressed, keys.Nav.PageUp) {
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

	if params.rows == 0 && (params.empty != "" || m.filter.doesNarrow(sectionList)) {
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
