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

// splitOf divides a window between a block and the sidebar, widening the configured share to
// what a list of names can be read in and refusing a window that cannot hold both, where the
// sidebar takes the whole of it and the block waits for the room.
func splitOf(width, rows, sidebar, floor int) (columnSplit, bool) {
	if rows <= 0 || width < nameWidth+floor {
		return columnSplit{}, false
	}

	sidebar = min(max(sidebar, nameWidth), width-floor)

	return columnSplit{column: width - sidebar, sidebar: sidebar, rows: rows}, true
}

// columnSpec is the block a screen draws beside its own list, left nil by a screen that draws
// none, which is what says the panel keeps the window to the share the frame gave it.
type columnSpec struct {
	// floor is the narrowest the block is worth drawing at, below which the sidebar keeps the
	// whole window and the block waits for the room.
	floor int

	// rows is the row budget the block is actually drawn in, and is nil on a screen with nothing
	// standing over it taking rows of its own.
	rows func(model, columnSplit) int

	// fits refuses the split when the sidebar cannot hold its own chrome in the narrower column
	// it is left with, and is nil on a screen that would rather have the block either way.
	fits func(model, columnSplit) bool
}

// columnGeometry is how wide each half of the window is, and carries the row budget the frame
// gave rather than the one the block is drawn in, because a screen measuring what stands over its
// block would ask this its own question and render itself to answer.
func (m model) columnGeometry() (columnSplit, bool) {
	spec, ok := m.columnOf()
	if !ok {
		return columnSplit{}, false
	}

	if !m.frame.IsSolo() {
		return columnSplit{}, false
	}

	split, ok := splitOf(m.width, m.height-m.chromeLines(), m.frame.SidebarWidth(m.width), spec.floor)
	if !ok {
		return columnSplit{}, false
	}

	// Asked here rather than in columnSplit because paneWidth reads this one, and a screen given
	// its own column while its list is drawn in the whole pane would lay the two out against
	// different widths.
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

	// Only ever asked of a block drawn in the other column, since a screen that has to measure
	// its own sidebar to size its block is asking paneWidth the question it is answering.
	if spec, _ := m.columnOf(); spec.rows != nil {
		split.rows = max(spec.rows(m, split), 1)
	}

	return split, true
}

// screenColumnSplit is the split a screen's own block is drawn across, refused while a form is up
// because the form has taken the column and the list is drawn beside it as context.
func (m model) screenColumnSplit() (columnSplit, bool) {
	if m.form.isOpening() {
		return columnSplit{}, false
	}

	return m.columnSplit()
}

// columnOf is the block the panel is drawing beside its list, which is the form's while one is
// up and the screen's otherwise, since a form is drawn over whichever screen opened it.
func (m model) columnOf() (columnSpec, bool) {
	if m.form.isOpening() {
		return formColumn(m.form.kind)
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
	_, ok := m.columnOf()

	return ok
}

// reconcileSolo settles whether the panel has the whole window, which is every way onto a screen
// and every way into a form, and hands it back while the info pane is up since a sidebar holding
// the window covers the pane the logs and the editor are drawn in.
func (m *model) reconcileSolo() {
	m.frame.Solo(m.holdsWindow() && m.info.kind == infoNothing)
}

// joinColumns lays the two halves of the window side by side, on the side the frame puts the
// sidebar, having squared each off at its share first.
func (m model) joinColumns(split columnSplit, sidebar, column string) string {
	// Trimmed so the two halves are the same height, since a trailing newline is a row the
	// sidebar block would have and the block beside it would not.
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

// blankColumn is a column's share of the window with nothing in it, drawn by a screen still
// waiting on what goes there rather than giving the split up and moving everything else.
func blankColumn(split columnSplit) string {
	return strings.Repeat("\n", max(split.rows-1, 0))
}

// fitColumn squares a block off at a width, since the halves of the window are joined side by
// side and a block whose longest line falls short of its share moves the other one under it.
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
