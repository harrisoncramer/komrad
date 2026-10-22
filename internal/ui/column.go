package ui

import (
	"fmt"

	"komrad/internal/glyph"
	"komrad/internal/ui/column"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
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

// columnSpec is the block a screen renders beside its own list, left nil by a screen that renders
// none.
type columnSpec struct {
	// floor is the narrowest the block is worth rendering at, below which the sidebar keeps the whole
	// window.
	floor int

	// doc is the markdown the block is rendered from, and is nil on a screen that renders and scrolls
	// its block itself.
	doc func(*model, screen.Inputs) string

	// docRows renders the block's rows at a width in place of doc, for a document with rows markdown
	// cannot style.
	docRows func(*model, screen.Inputs, int) []string

	// read reports whether the block is a document the keys can be handed to, and is nil on a
	// screen whose block never is.
	read          func(*model, screen.Inputs) bool
	searchOptions []filterOption

	// holds reports whether the screen is rendering its own block at all, and is nil on a screen that
	// always is.
	holds func(*model, screen.Inputs) bool

	// rows is the row budget the block is actually rendered in, and is nil on a screen with nothing
	// standing over it.
	rows func(*model, screen.Inputs, column.Split) int

	// beside is the agent the screen keeps on the stage beside its block, and is nil on a screen that
	// keeps none.
	beside func(*model, screen.Inputs) (panel.AgentRow, bool)
}

// isHeld reports whether the screen is rendering its own block rather than handing its columns to
// another process.
func (s columnSpec) isHeld(m *model) bool {
	return s.holds == nil || s.holds(m, m.inputs())
}

// sidebarColumns is how wide the panel's own list is rendered while it holds the window, and the
// block it is holding it for.
func (m *model) sidebarColumns() (int, columnSpec, bool) {
	// Nothing can be divided before the first window size arrives, and the frame has no layout to
	// answer for either.
	if m.term.width <= 0 || m.term.height <= 0 {
		return 0, columnSpec{}, false
	}

	spec := m.columnOf()
	if !spec.isHeld(m) {
		return 0, columnSpec{}, false
	}

	if !m.frame.IsSolo() && !m.frame.IsBeside() {
		return 0, columnSpec{}, false
	}

	sidebar, measured := m.frame.SidebarWidth(m.term.paneCols)

	// A configured width is a starting point rather than a boundary anybody placed, so it is
	// widened to what a list of names reads in and capped at what leaves the block its floor,
	// while a measured one is rendered where the divider is whatever that leaves either half.
	if !measured {
		sidebar = min(max(sidebar, nameWidth+sidebarChrome), m.term.width-spec.floor)
	}

	// Clamped so the fold and the render are at one width.
	return min(max(sidebar-sidebarChrome, 1), m.term.width-1-dividerCols), spec, true
}

// columnGeometry is how wide each half of the window is, and how many rows the block beside the
// list is rendered in.
func (m *model) columnGeometry() column.Split {
	sidebar, spec, ok := m.sidebarColumns()
	if !ok {
		return column.Split{}
	}

	// The whole height, with the sidebar's chrome rendered inside the sidebar and no rows reserved
	// here.
	return splitOf(m.term.width-dividerCols, m.term.height, sidebar, spec.floor)
}

// columnSplit is the window the block is actually rendered in, being the geometry with whatever
// stands over the block taken off its rows.
func (m *model) columnSplit() column.Split {
	split := m.columnGeometry()
	if !split.On {
		return split
	}

	// Only ever asked of a block rendered in the other column.
	if spec := m.columnOf(); spec.rows != nil {
		split.Rows = max(spec.rows(m, m.inputs(), split), 1)
	}

	return split
}

// screenColumnSplit is the split a screen's own block is rendered across, refused while a form is
// up.
func (m *model) screenColumnSplit() column.Split {
	if (m.form.isOpening() && !m.isPopupForm()) || m.isMessagesFocused() {
		return column.Split{}
	}

	return m.columnSplit()
}

// columnOf is the block the panel renders beside its list, which is the form's while one is up and
// the screen's otherwise.
func (m *model) columnOf() columnSpec {
	if m.isMessagesFocused() {
		return buildMessagesColumn()
	}

	if m.form.isOpening() && !m.isPopupForm() {
		return formColumn(m.form.Narrow)
	}

	return m.active().column()
}

// holdsWindow reports whether the panel is rendering a block beside its own list, and so takes the
// window from the panes beside it.
func (m *model) holdsWindow() bool {
	return m.columnOf().isHeld(m)
}

// wantsSolo reports whether the panel should hold the whole window.
func (m *model) wantsSolo() bool {
	return m.holdsWindow() && m.terminalDir == "" && !m.isStageBeside()
}

// isStageBeside reports whether the screen keeps an agent on the stage beside its block, which it
// does only while the agent is running and the window has room for both.
func (m *model) isStageBeside() bool {
	spec := m.active().column()
	if spec.beside == nil || m.terminalDir != "" {
		return false
	}

	if !m.frame.FitsBeside(m.term.windowCols, spec.floor) {
		return false
	}

	_, live := spec.beside(m, m.inputs())

	return live
}

// restageBeside restages the screen when the frame no longer matches whether its agent belongs
// beside its block.
func (m *model) restageBeside() {
	spec := m.active().column()
	if spec.beside != nil && spec.isHeld(m) && m.isStageBeside() != m.frame.IsBeside() {
		m.stageSelection()
	}
}

// syncBeside draws the sidebar across the stage for the block while an agent is beside it, and
// gives the stage its columns back once none is or another process has them.
func (m *model) syncBeside() {
	m.frame.Beside(m.isStageBeside() && m.active().column().isHeld(m))
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
	split := m.screenColumnSplit()
	if !split.On {
		return false
	}

	return x-gutter >= split.Sidebar+dividerCols
}

// paneWidth is the column the panel renders its own list, record and legend in, which is the whole
// pane except while it is rendering a block beside them.
func (m *model) paneWidth() int {
	// A half of a split is laid out at the column already chosen for it rather than asking for the
	// split again.
	if m.term.paneMeasure > 0 {
		return m.term.paneMeasure
	}

	if split := m.columnGeometry(); split.On {
		return split.Sidebar
	}

	return m.term.width
}

// docShown names what the block beside the list is showing, so the reader is dropped once it
// changes.
func (m *model) docShown() string {
	hovered := m.hoveredDetails().id
	if f, ok := m.active().(activeLister); ok {
		hovered = f.activeList().hovered(m, m.inputs())
	}
	if n, ok := m.active().(docNamer); ok {
		hovered = n.docName(m)
	}

	return fmt.Sprintf("%d/%s/%t", m.screen, hovered, m.isMessagesFocused())
}

// docNamer is a screen whose every row reads the one document, named so the reader keeps its place
// as the cursor moves.
type docNamer interface {
	docName(m *model) string
}

// columnDoc is the document the block is rendered from, read where the reader left it.
func (m *model) columnDoc(split column.Split) docWindow {
	if m.isMessagesFocused() {
		return m.renderMessagesWindow(split.Column, split.Rows)
	}

	if m.isDocReadable() {
		return docWindow{rows: m.docBlock(split)}
	}

	return docWindow{
		rows:    m.columnRows(split),
		markers: true,
	}
}

// columnRows is the block rendered at its width, and empty on a screen whose block is not a
// document.
func (m *model) columnRows(split column.Split) []string {
	spec := m.columnOf()
	if spec.docRows != nil {
		return spec.docRows(m, m.inputs(), split.Column)
	}
	if spec.doc == nil {
		return nil
	}

	return paint.DocStyledRows(spec.doc(m, m.inputs()), max(split.Column, 1), columnStyle())
}
