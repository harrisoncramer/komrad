package ui

import (
	"fmt"
	"strings"

	"komrad/internal/ui/paint"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	// dividerCols is the columns the divider between the list and the block beside it takes, with
	// the gutter on either side of it.
	dividerCols = 2*gutter + 1
	// dividerGlyph is what the divider is drawn with.
	dividerGlyph = "│"
)

// columnSplit is how the window is divided while the panel is drawing a block beside its own
// list: the block on one side, and the list, its record and its legend on the other.
type columnSplit struct {
	column  int
	sidebar int
	rows    int
}

// splitOf divides a window between a block and the sidebar at the width it is handed, and refuses
// a window that cannot hold both halves.
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

// columnFloor is the narrowest a document is worth drawing beside a list at.
const columnFloor = 40

// columnSpec is the block a screen draws beside its own list, left nil by a screen that draws
// none.
type columnSpec struct {
	// floor is the narrowest the block is worth drawing at, below which the sidebar keeps the whole
	// window.
	floor int

	// doc is the markdown the block is drawn from, and is nil on a screen that draws and scrolls
	// its block itself.
	doc func(model) string

	// docRows draws the block's rows at a width in place of doc, for a document with rows markdown
	// cannot style.
	docRows func(model, int) []string

	// holds reports whether the screen is drawing its own block at all, and is nil on a screen that
	// always is.
	holds func(model) bool

	// rows is the row budget the block is actually drawn in, and is nil on a screen with nothing
	// standing over it.
	rows func(model, columnSplit) int
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

	sidebar, measured := m.frame.SidebarWidth(m.paneCols)

	// A configured width is a starting point rather than a boundary anybody placed, so it is
	// widened to what a list of names reads in and capped at what leaves the block its floor,
	// while a measured one is drawn where the divider is whatever that leaves either half.
	chrome := m.countSidebarChrome()
	if !measured {
		sidebar = min(max(sidebar, nameWidth+chrome), m.width-spec.floor)
	}

	// Clamped so the fold and the draw are at one width.
	return min(max(sidebar-chrome, 1), m.width-1-dividerCols), spec, true
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
	return splitOf(m.width-dividerCols, m.height, sidebar, spec.floor)
}

// columnSplit is the window the block is actually drawn in, being the geometry with whatever
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

	c, ok := m.active().(columned)
	if !ok {
		return columnSpec{}, false
	}

	return c.column(), true
}

// holdsWindow reports whether the panel is drawing a block beside its own list, and so takes the
// window from the panes beside it.
func (m model) holdsWindow() bool {
	spec, ok := m.columnOf()
	if !ok {
		return false
	}

	return spec.holds == nil || spec.holds(m)
}

// reconcileSolo settles whether the panel has the whole window, and hands it back while the info
// pane is up, which a sidebar holding the window would cover.
func (m *model) reconcileSolo() {
	// Compared here rather than inside Frame.Solo, which Attach calls with the field already
	// false on purpose to clear a zoom a dead panel left behind: the frame answers from what it
	// holds, while Solo asks tmux with a subprocess, and this runs on every listing change and
	// every cursor move.
	want := m.holdsWindow() && m.info.kind == infoNothing
	if want == m.frame.IsSolo() {
		return
	}

	m.frame.Solo(want)
	m.awaitingSize = true

	if m.paneCols > 0 {
		m.width = max(m.paneCols-m.countInsetCols(), 0)
		*m = m.relayout(true)
	}
}

// syncKeys puts the frame right about whether it still holds the window, which one focus report
// cannot say on its own since the keys leaving the pane and the terminal losing focus arrive as
// the same message.
func (m *model) syncKeys() {
	m.frame.SyncKeys()
}

// isOverColumn reports whether a column of the window falls in the block beside the list.
func (m model) isOverColumn(x int) bool {
	split, ok := m.screenColumnSplit()
	if !ok {
		return false
	}

	x -= m.countInsetLeft()

	if m.frame.IsSidebarOnRight() {
		return x < split.column
	}

	return x >= split.sidebar+dividerCols
}

// joinColumns lays the two halves of the window side by side, on the side the frame puts the
// sidebar, having squared each off at its share first.
func (m model) joinColumns(split columnSplit, sidebar, column string) string {
	// Trimmed so the two halves are the same height.
	sidebar = fitColumn(strings.TrimRight(sidebar, "\n"), split.sidebar)
	column = fitColumn(column, split.column)

	left, right := sidebar, column
	if m.frame.IsSidebarOnRight() {
		left, right = column, sidebar
	}

	leftLines, rightLines := strings.Split(left, "\n"), strings.Split(right, "\n")
	rows := max(len(leftLines), len(rightLines))
	leftWidth, rightWidth := lipgloss.Width(left), lipgloss.Width(right)

	out := make([]string, rows)
	for i := range rows {
		l := readLineAt(leftLines, i, leftWidth)
		r := readLineAt(rightLines, i, rightWidth)
		out[i] = l + drawDividerRow(isRuleEnd(l, true), isRuleEnd(r, false)) + r
	}

	return strings.Join(out, "\n")
}

// readLineAt is the row of a half at i, or blank cells when the half is shorter than the other.
func readLineAt(lines []string, i, width int) string {
	if i < len(lines) {
		return lines[i]
	}

	return strings.Repeat(" ", width)
}

// isRuleEnd reports whether a row is a rule running up to the divider on the given side of it.
func isRuleEnd(line string, left bool) bool {
	plain := ansi.Strip(line)
	if left {
		return strings.HasSuffix(plain, "─")
	}

	return strings.HasPrefix(plain, "─")
}

// drawDividerRow is one row of the divider, joined to the rules that meet it from either side.
func drawDividerRow(fromLeft, fromRight bool) string {
	glyph := dividerGlyph
	switch {
	case fromLeft && fromRight:
		glyph = "┼"
	case fromLeft:
		glyph = "┤"
	case fromRight:
		glyph = "├"
	}

	leftPad, rightPad := strings.Repeat(" ", gutter), strings.Repeat(" ", gutter)
	if fromLeft {
		leftPad = dimStyle.Render(strings.Repeat("─", gutter))
	}
	if fromRight {
		rightPad = dimStyle.Render(strings.Repeat("─", gutter))
	}

	return leftPad + dimStyle.Render(glyph) + rightPad
}

// paneWidth is the column the panel draws its own list, record and legend in, which is the whole
// pane except while it is drawing a block beside them.
func (m model) paneWidth() int {
	// A half of a split is laid out at the column already chosen for it rather than asking for the
	// split again.
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
	// column.
	held := m.measuredAt(split.sidebar)

	return m.joinColumns(split, list(held), held.docColumnView(split))
}

// docColumnView is the first screenful of the block beside the list, with a marker under it
// counting what is past it.
func (m model) docColumnView(split columnSplit) string {
	return m.columnDoc(split).view(split.rows)
}

// docScroll is how far down the block beside the list the reader has scrolled, and what the block
// was showing when they did.
type docScroll struct {
	shown string
	top   int
}

// docShown names what the block beside the list is showing, so a scroll is dropped once it changes.
func (m model) docShown() string {
	return fmt.Sprintf("%d/%s/%t", m.screen, m.hoveredDetails().id, m.isMessagesFocused())
}

// scrollDoc moves the block beside the list, stopping at either end.
func (m *model) scrollDoc(rows int) {
	split, ok := m.screenColumnSplit()
	if !ok {
		return
	}

	doc := m.columnDoc(split)
	top := doc.clamp(doc.top, split.rows)
	m.docScroll = docScroll{shown: m.docShown(), top: doc.clamp(top+rows, split.rows)}
}

// dropStaleDocScroll forgets a scroll once the block shows something else.
func (m model) dropStaleDocScroll() model {
	if m.docScroll.shown != m.docShown() {
		m.docScroll = docScroll{}
	}

	return m
}

// columnDoc is the document the block is drawn from, scrolled where the reader left it.
func (m model) columnDoc(split columnSplit) docWindow {
	doc := m.unscrolledDoc(split)
	doc.top = m.docScroll.top

	return doc
}

// unscrolledDoc is the document the block is drawn from, and empty on a screen whose block is not
// one.
func (m model) unscrolledDoc(split columnSplit) docWindow {
	// The region draws the message in its own rows, except under a form, which covers them.
	if m.isMessagesFocused() && m.form.isOpening() {
		return m.messagesBlock(split)
	}

	spec, ok := m.columnOf()
	if ok && spec.docRows != nil {
		return docWindow{
			rows:    spec.docRows(m, split.column),
			markers: true,
		}
	}
	if !ok || spec.doc == nil {
		return docWindow{}
	}

	return docWindow{
		rows:    paint.DocStyledRows(spec.doc(m), split.column, columnStyle()),
		markers: true,
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
