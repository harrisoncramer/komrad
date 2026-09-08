package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// columnSplit is how the window is divided while the panel is drawing a block beside its own
// list: the block on one side and the list, its record and its legend on the other.
type columnSplit struct {
	column  int
	sidebar int
	rows    int
}

// splitOf divides a window between a block and the sidebar at the width it is handed, which is
// left where it was asked for since a boundary drawn anywhere but the divider under the panel is
// the misalignment the measuring is for, and refuses a window that cannot hold both halves, where
// the sidebar takes the whole of it and the block waits for the room.
func splitOf(width, rows, sidebar, floor int) (columnSplit, bool) {
	if rows <= 0 || width < nameWidth+floor {
		return columnSplit{}, false
	}

	sidebar = min(max(sidebar, 1), width-1)

	return columnSplit{
		column:  width - sidebar,
		sidebar: sidebar,
		rows:    rows,
	}, true
}

// columnFloor is the narrowest a document is worth drawing beside a list at, which is about as
// short as a line of prose stays readable.
const columnFloor = 40

// columnState is where in the block beside the list the reader has scrolled to, held by the panel
// rather than by each screen.
type columnState struct {
	// top is the first row of the block on screen, worked out from nothing but what the reader has
	// scrolled.
	top int

	// row is the row that offset was scrolled on, kept against the cursor's own index sliding under
	// a reader who has not moved.
	row string
}

// place takes the block back to its top when the cursor lands on a different row.
func (c *columnState) place(row string) {
	if row != c.row {
		c.top = 0
	}

	c.row = row
}

// placeColumn takes the block back to its top for the screen that holds it, ignoring a screen
// that is not on top.
func (m *model) placeColumn(owner screen, row string) {
	if m.screen != owner {
		return
	}

	m.column.place(row)
}

// columnSpec is the block a screen draws beside its own list, left nil by a screen that draws
// none, which is what says the panel keeps the window to the share the frame gave it.
type columnSpec struct {
	// floor is the narrowest the block is worth drawing at, below which the sidebar keeps the
	// whole window and the block waits for the room.
	floor int

	// doc is the markdown the block is drawn from, for a screen whose block is a document read
	// from the top, and is nil on a screen that draws and scrolls its block itself.
	doc func(model) string

	// holds reports whether the screen is drawing its own block at all, and is nil on a screen
	// that always is: the agents list says no while the cursor is on a running agent, whose own
	// pane is what stands there instead.
	holds func(model) bool

	// rows is the row budget the block is actually drawn in, and is nil on a screen with nothing
	// standing over it taking rows of its own.
	rows func(model, columnSplit) int

	// fits refuses the split when the sidebar cannot hold its own chrome in the narrower column
	// it is left with, and is nil on a screen that would rather have the block either way.
	fits func(model, columnSplit) bool
}

// sidebarColumns is how wide the panel's own list is drawn while it holds the window, and the
// block it is holding it for.
func (m model) sidebarColumns() (int, columnSpec, bool) {
	// Nothing can be divided before the first window size arrives, and the frame has no layout to
	// answer for either.
	if m.width <= 0 || m.height <= 0 {
		return 0, columnSpec{}, false
	}

	spec, ok := m.columnOf()
	if !ok {
		return 0, columnSpec{}, false
	}

	if spec.holds != nil && !spec.holds(m) {
		return 0, columnSpec{}, false
	}

	if !m.frame.IsSolo() {
		return 0, columnSpec{}, false
	}

	sidebar, measured := m.frame.SidebarWidth(m.width)

	// A configured width is a starting point rather than a boundary anybody placed, so it is
	// widened to what a list of names reads in and capped at what leaves the block its floor,
	// while a measured one is drawn where the divider is whatever that leaves either half.
	if !measured {
		sidebar = min(max(sidebar, nameWidth), m.width-spec.floor)
	}

	// Clamped so the fold and the draw are at one width.
	return min(max(sidebar, 1), m.width-1), spec, true
}

// columnGeometry is how wide each half of the window is, and how many rows the block beside the
// list is drawn in.
func (m model) columnGeometry() (columnSplit, bool) {
	sidebar, spec, ok := m.sidebarColumns()
	if !ok {
		return columnSplit{}, false
	}

	// The whole height, with the sidebar's chrome drawn inside the sidebar and no rows reserved
	// here.
	split, ok := splitOf(m.width, m.height, sidebar, spec.floor)
	if !ok {
		return columnSplit{}, false
	}

	// Asked here rather than in columnSplit, which paneWidth reads.
	if spec.fits != nil && !spec.fits(m, split) {
		return columnSplit{}, false
	}

	return split, true
}

// columnSplit is the window the block is actually drawn in, which is the geometry with whatever
// stands over the block taken off its rows.
func (m model) columnSplit() (columnSplit, bool) {
	split, ok := m.columnGeometry()
	if !ok {
		return split, false
	}

	// Only ever asked of a block drawn in the other column.
	if spec, _ := m.columnOf(); spec.rows != nil {
		split.rows = max(spec.rows(m, split), 1)
	}

	return split, true
}

// screenColumnSplit is the split a screen's own block is drawn across, refused while a form is
// up.
func (m model) screenColumnSplit() (columnSplit, bool) {
	if m.form.isOpening() {
		return columnSplit{}, false
	}

	return m.columnSplit()
}

// columnOf is the block the panel draws beside its list, which is the form's while one is up and
// the screen's otherwise.
func (m model) columnOf() (columnSpec, bool) {
	if m.form.isOpening() {
		return formColumn(m.form.kind)
	}

	// The region takes the block while it holds the keys, except under a form, which keeps the
	// column it was laid out against.
	if m.isMessagesFocused() && !m.form.isOpening() {
		return messagesColumn(), true
	}

	column := m.screen.spec().column
	if column == nil {
		return columnSpec{}, false
	}

	return column(), true
}

// holdsWindow reports whether the panel is drawing a block beside its own list, which is what
// says it takes the window from the panes beside it rather than keeping its share of the frame.
func (m model) holdsWindow() bool {
	spec, ok := m.columnOf()
	if !ok {
		return false
	}

	return spec.holds == nil || spec.holds(m)
}

// reconcileSolo settles whether the panel has the whole window, which is every way onto a screen
// and every way into a form, and hands it back while the info pane is up since a sidebar holding
// the window covers the pane the logs and the editor are drawn in.
func (m *model) reconcileSolo() {
	// Compared here rather than inside Frame.Solo, which Attach calls with the field already
	// false on purpose to clear a zoom a dead panel left behind: the frame answers from what it
	// holds, while Solo asks tmux with a subprocess, and this runs on every refresh tick and
	// every cursor move.
	want := m.holdsWindow() && m.info.kind == infoNothing
	if want == m.frame.IsSolo() {
		return
	}

	m.frame.Solo(want)

	// A collected change has not moved the panes yet, so the size is read back once it has
	// landed rather than here, where tmux would answer for the layout as it stood.
	if m.frame.IsCollecting() {
		m.resizeOwed = true

		return
	}

	m.resize()
}

// resize reads the panel's size back from its own pane, which a zoom changes before the terminal
// reports it.
func (m *model) resize() {
	width, height := m.frame.SidebarSize()
	if width <= 0 || height <= 0 {
		return
	}

	m.width, m.height = width, height
}

// syncKeys puts the frame right about whether it still holds the window, which a focus report
// cannot say on its own: the same message arrives when the keys move off the panel's pane, which
// drops the sidebar's zoom, and when the terminal the whole session is in loses focus, which does
// not.
func (m *model) syncKeys() {
	m.frame.SyncKeys()
}

// joinColumns lays the two halves of the window side by side, on the side the frame puts the
// sidebar, having squared each off at its share first.
func (m model) joinColumns(split columnSplit, sidebar, column string) string {
	// Trimmed so the two halves are the same height.
	sidebar = fitColumn(strings.TrimRight(sidebar, "\n"), split.sidebar)
	column = fitColumn(column, split.column)

	if m.frame.IsSidebarOnRight() {
		return lipgloss.JoinHorizontal(lipgloss.Top, column, sidebar)
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, column)
}

// paneWidth is the column the panel draws its own list, record and legend in, which is the whole
// pane except while it is drawing a block of its own beside them.
func (m model) paneWidth() int {
	// A screen working out whether its own split leaves room for its chrome has already chosen
	// the column, so it is told the answer rather than asked for the split it is still deciding.
	if m.paneMeasure > 0 {
		return m.paneMeasure
	}

	if split, ok := m.columnGeometry(); ok {
		return split.sidebar
	}

	return m.width
}

// columnView draws a screen as its list beside its own block, or as the list alone on a window
// that will not hold both.
func (m model) columnView(list func(model) string) string {
	split, ok := m.screenColumnSplit()
	if !ok {
		return list(m)
	}

	// Asked for before either half is drawn, so the halves are laid out already knowing their
	// column rather than sending every line of chrome back through paneWidth into the geometry.
	held := m.at(split.sidebar)

	return m.joinColumns(split, list(held), held.docColumnView(split))
}

// docColumnView is the screenful of the block beside the list, from the row the reader has
// scrolled to, with a marker on each end counting what is past it.
func (m model) docColumnView(split columnSplit) string {
	return m.columnDoc(split).view(split.rows)
}

// columnDoc is the document the block is drawn from, empty on a screen whose block is not one,
// which docWindow draws as the blank half of a window rather than refusing to draw at all.
func (m model) columnDoc(split columnSplit) docWindow {
	if m.isMessagesFocused() {
		return m.messagesBlock(split)
	}

	spec, ok := m.columnOf()
	if !ok || spec.doc == nil {
		return docWindow{}
	}

	return docWindow{
		rows:    docRows(spec.doc(m), split.column),
		top:     m.column.top,
		markers: true,
	}
}

// scrollColumn moves the block beside the list, clamped to what is left of it so the reader cannot
// scroll a short document off the top of its own column.
func (m model) scrollColumn(direction int) model {
	split, ok := m.screenColumnSplit()

	// A form refuses the screen's own split, and the region borrows the column it has to draw the
	// message being scrolled.
	if !ok && m.isMessagesFocused() {
		split, ok = m.columnSplit()
	}
	if !ok {
		return m
	}

	doc := m.columnDoc(split)
	doc.scroll(direction*docScrollStep, split.rows)
	m.column.top = doc.top

	return m
}

// isColumnScroll reports whether a key moves the block beside the list, which it does only on a
// screen whose block is a document the panel scrolls rather than one the screen scrolls itself.
func (m model) isColumnScroll(k string) (int, bool) {
	if spec, ok := m.columnOf(); !m.isMessagesFocused() && (!ok || spec.doc == nil) {
		return 0, false
	}

	switch {
	case isBound(k, m.keys.Column.ScrollDown):
		return 1, true
	case isBound(k, m.keys.Column.ScrollUp):
		return -1, true
	default:
		return 0, false
	}
}

// fitColumn squares a block off at a width.
func fitColumn(block string, width int) string {
	rows := strings.Split(block, "\n")
	for i, row := range rows {
		if lipgloss.Width(row) > width {
			row = ansi.Truncate(row, width, "")
		}

		rows[i] = row + strings.Repeat(" ", max(width-lipgloss.Width(row), 0))
	}

	return strings.Join(rows, "\n")
}
