package ui

import (
	"fmt"

	"komrad/internal/glyph"
	"komrad/internal/ui/column"
	"komrad/internal/ui/screen"
)

const (
	// dividerCols is the columns the divider between the list and the block beside it takes.
	dividerCols = 2*gutter + 1
	// dividerGlyph is what the divider is rendered with.
	dividerGlyph = glyph.Divider
)

// splitOf divides a window between a block and the sidebar at the width it is handed, and refuses
// a window that cannot hold both halves.
func splitOf(width, rows, sidebar, floor int) column.Split {
	if rows <= 0 || width < nameWidth+floor {
		return column.Split{}
	}

	sidebar = min(max(sidebar, 1), width-1)

	return column.Split{
		Column:  width - sidebar,
		Sidebar: sidebar,
		Rows:    rows,
		On:      true,
	}
}

// columnFloor is the narrowest a document is worth rendering beside a list at.
const columnFloor = 40

// sidebarColumns is how wide the panel's own list is rendered while it holds the window, and the
// block it is holding it for.
func (m *model) sidebarColumns(in screen.Inputs) (int, column.Spec, bool) {
	// Nothing can be divided before the first window size arrives, and the frame has no layout to
	// answer for either.
	if m.term.width <= 0 || m.term.height <= 0 {
		return 0, column.Spec{}, false
	}

	spec := m.columnOf(in)
	if !spec.IsHeld() {
		return 0, column.Spec{}, false
	}

	if !m.frame.IsSolo() && !m.frame.IsBeside() {
		return 0, column.Spec{}, false
	}

	sidebar, measured := m.frame.SidebarWidth(m.term.paneCols)

	// A configured width is a starting point rather than a boundary anybody placed, so it is
	// widened to what a list of names reads in and capped at what leaves the block its floor,
	// while a measured one is rendered where the divider is whatever that leaves either half.
	if !measured {
		sidebar = min(max(sidebar, nameWidth+sidebarChrome), m.term.width-spec.Floor)
	}

	// Clamped so the fold and the render are at one width.
	return min(max(sidebar-sidebarChrome, 1), m.term.width-1-dividerCols), spec, true
}

// columnGeometry is how wide each half of the window is, and how many rows the block beside the
// list is rendered in.
func (m *model) columnGeometry(in screen.Inputs) column.Split {
	sidebar, spec, ok := m.sidebarColumns(in)
	if !ok {
		return column.Split{}
	}

	// The whole height, with the sidebar's chrome rendered inside the sidebar and no rows reserved
	// here.
	return splitOf(m.term.width-dividerCols, m.term.height, sidebar, spec.Floor)
}

// columnSplit is the window the block is actually rendered in, being the geometry with whatever
// stands over the block taken off its rows.
func (m *model) columnSplit(in screen.Inputs) column.Split {
	split := in.Room.Geometry
	if !split.On {
		return split
	}

	// Only ever asked of a block rendered in the other column.
	if spec := m.columnOf(in); spec.Rows != nil {
		split.Rows = max(spec.Rows(split), 1)
	}

	return split
}

// screenColumnSplit is the split a screen's own block is rendered across, refused while a form is
// up.
func (m *model) screenColumnSplit(in screen.Inputs) column.Split {
	if (m.form.isOpening() && !m.isPopupForm()) || in.Room.MessagesFocused {
		return column.Split{}
	}

	return in.Room.Column
}

// columnOf is the block the panel renders beside its list, which is the form's while one is up and
// the screen's otherwise.
func (m *model) columnOf(in screen.Inputs) column.Spec {
	if in.Room.MessagesFocused {
		return buildMessagesColumn()
	}

	if m.form.isOpening() && !m.isPopupForm() {
		return formColumn(m.form.Narrow)
	}

	return m.active().column(m, in)
}

// holdsWindow reports whether the panel is rendering a block beside its own list, and so takes the
// window from the panes beside it.
func (m *model) holdsWindow() bool {
	return m.columnOf(m.inputs()).IsHeld()
}

// wantsSolo reports whether the panel should hold the whole window.
func (m *model) wantsSolo() bool {
	return m.holdsWindow() && m.terminalDir == "" && !m.isStageBeside()
}

// isStageBeside reports whether the screen keeps an agent on the stage beside its block, which it
// does only while the agent is running and the window has room for both.
func (m *model) isStageBeside() bool {
	spec := m.active().column(m, m.inputs())
	if spec.Beside == nil || m.terminalDir != "" {
		return false
	}

	if !m.frame.FitsBeside(m.term.windowCols, spec.Floor) {
		return false
	}

	_, live := spec.Beside()

	return live
}

// restageBeside restages the screen when the frame no longer matches whether its agent belongs
// beside its block.
func (m *model) restageBeside() {
	spec := m.active().column(m, m.inputs())
	if spec.Beside != nil && spec.IsHeld() && m.isStageBeside() != m.frame.IsBeside() {
		m.stageSelection()
	}
}

// syncBeside draws the sidebar across the stage for the block while an agent is beside it, and
// gives the stage its columns back once none is or another process has them.
func (m *model) syncBeside() {
	m.frame.Beside(m.isStageBeside() && m.active().column(m, m.inputs()).IsHeld())
}

// reconcileSolo settles whether the panel has the whole window, and hands it back while the info
// pane is up, which a sidebar holding the window would cover.
func (m *model) reconcileSolo() {
	// Compared here rather than inside Frame.Solo, which Attach calls with the field already
	// false on purpose to clear a zoom a dead panel left behind: the frame answers from what it
	// holds, while Solo asks tmux with a subprocess, and this runs on every listing change and
	// every cursor move.
	want := m.wantsSolo()
	if want == m.frame.IsSolo() {
		m.syncBeside()

		return
	}

	if want && m.term.keysElsewhere {
		return
	}

	m.frame.Solo(want)
	m.term.awaitingSince = m.clock.Now()
	m.syncBeside()
}

func (m *model) isAwaitingSize() bool {
	return !m.term.awaitingSince.IsZero()
}

// syncKeys puts the frame right about whether it still holds the window, which one focus report
// cannot say on its own since the keys leaving the pane and the terminal losing focus arrive as
// the same message.
func (m *model) syncKeys() {
	m.frame.SyncKeys()
}

// isOverColumn reports whether a column of the window falls in the block beside the list.
func (m *model) isOverColumn(x int) bool {
	split := m.inputs().Room.Split
	if !split.On {
		return false
	}

	return x-gutter >= split.Sidebar+dividerCols
}

// paneWidth is the column the panel renders its own list, record and legend in, which is the whole
// pane except while it is rendering a block beside them.
func (m *model) paneWidth() int {
	return m.measuredInputsFor(m.active()).Room.Width
}

// paneWidthAt is the pane's width beside the geometry a window is divided by.
func (m *model) paneWidthAt(geometry column.Split) int {
	// A half of a split is laid out at the column already chosen for it rather than asking for the
	// split again.
	if m.term.paneMeasure > 0 {
		return m.term.paneMeasure
	}

	if geometry.On {
		return geometry.Sidebar
	}

	return m.term.width
}

// docShown names what the block beside the list is showing, so the reader is dropped once it
// changes.
func (m *model) docShown(in screen.Inputs) string {
	hovered := m.hoveredDetails(in).id
	if f, ok := m.active().(activeLister); ok {
		hovered = f.activeList().hovered(m, in)
	}
	if n, ok := m.active().(docNamer); ok {
		hovered = n.docName(m)
	}

	return fmt.Sprintf("%d/%s/%t", m.screen, hovered, in.Room.MessagesFocused)
}

// docNamer is a screen whose every row reads the one document, named so the reader keeps its place
// as the cursor moves.
type docNamer interface {
	docName(m *model) string
}

// columnDoc is the document the block is rendered from, read where the reader left it.
func (m *model) columnDoc(in screen.Inputs, split column.Split) docWindow {
	if in.Room.MessagesFocused {
		return m.renderMessagesWindow(split.Column, split.Rows)
	}

	if m.isDocReadable(in) {
		return docWindow{rows: m.docBlock(in, split)}
	}

	return docWindow{
		rows:    m.columnRows(in, split),
		markers: true,
	}
}

// columnRows is the block rendered at its width, and empty on a screen whose block is not a
// document.
func (m *model) columnRows(in screen.Inputs, split column.Split) []string {
	if spec := m.columnOf(in); spec.DocRows != nil {
		return spec.DocRows(split.Column)
	}

	return nil
}
