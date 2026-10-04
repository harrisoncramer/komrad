package diffview

import (
	"regexp"
	"strings"

	"komrad/internal/ui/cursor"
)

// scrollMargin is how close to the edge of the pane the cursor gets before the rows scroll under
// it.
const scrollMargin = 4

// Window is the size a pane is drawn at and whether its long lines wrap.
type Window struct {
	Width  int
	Height int
	Wrap   bool
}

// Pane is a view with a reader standing in it: the row under the cursor, the first row on screen,
// and how far the rows are slid sideways.
type Pane struct {
	view   *View
	cursor int
	pan    int
	// top is the first row the pane is showing, held rather than derived from the cursor.
	top int

	// pinTop holds the window where a file jump put it until the cursor moves.
	pinTop bool
	// pinFloor is the row a file jump put at the top of the pane, which the window stays at or
	// below while the cursor is inside that file.
	pinFloor int
}

// NewPane stands a reader on the first row of a view.
func NewPane(view *View) Pane {
	return Pane{view: view, cursor: view.GetFirstRow()}
}

// View is the view the reader is standing in, nil while there is none.
func (p *Pane) View() *View {
	return p.view
}

// Cursor is the row the reader is standing on.
func (p *Pane) Cursor() int {
	return p.cursor
}

// Top is the first row the pane is showing.
func (p *Pane) Top() int {
	return p.top
}

// Pan is how far the rows are slid sideways.
func (p *Pane) Pan() int {
	return p.pan
}

// Replace puts a view built again in place of the one on screen, standing the reader on the place
// given or on the first row, and keeping the window and the pan.
func (p *Pane) Replace(view *View, place *Place) {
	p.view = view
	p.cursor = view.GetFirstRow()
	if place == nil {
		return
	}

	if found, ok := view.locate(*place); ok {
		p.cursor = found
	}
}

// Drop puts the view down, keeping where the window and the pan stood for the next one.
func (p *Pane) Drop() {
	p.view = nil
}

// MoveTo stands the reader on a row and scrolls the window to keep it in sight,
// reporting whether the reader moved.
func (p *Pane) MoveTo(row int, window Window) bool {
	if row == p.cursor {
		return false
	}

	p.cursor = row
	p.pinTop = false
	p.ScrollToCursor(window)

	return true
}

// Step moves the reader a number of rows up or down.
func (p *Pane) Step(direction, rows int, window Window) bool {
	cursor := p.cursor
	for range rows {
		cursor = p.view.Move(cursor, direction)
	}

	return p.MoveTo(cursor, window)
}

// JumpFile moves to the next or previous file and puts that file's header at the top of the pane.
func (p *Pane) JumpFile(direction int, window Window) bool {
	next := p.view.JumpFile(p.cursor, direction)
	if next == p.cursor {
		return false
	}

	p.pinFile(next, p.view.getFileTopRow(next), window)

	return true
}

// JumpToFile moves to the file whose header is on row and puts that header at the top of the pane.
func (p *Pane) JumpToFile(row int, window Window) bool {
	next := p.view.getFileLanding(row)
	if next == p.cursor {
		return false
	}

	p.pinFile(next, row, window)

	return true
}

// pinFile stands the reader on next and pins the header of its file to the top of the pane.
func (p *Pane) pinFile(next, header int, window Window) {
	p.cursor = next
	p.top = cursor.Clamp(header, p.view.Len())
	p.pinTop = true
	p.pinFloor = p.top
	p.ScrollToCursor(window)
}

// Search runs a pattern over the view and stands the reader on the first match at or after them.
func (p *Pane) Search(pattern *regexp.Regexp, window Window) {
	if p.view.SetPattern(pattern) == 0 {
		return
	}

	if next, ok := p.view.jumpMatch(p.cursor-1, 1); ok {
		p.cursor = next
		p.pinTop = false
	}

	p.ScrollToCursor(window)
}

// JumpMatch steps to the next or previous line the search found, wrapping at the ends, and
// reports false while nothing was found.
func (p *Pane) JumpMatch(direction int, window Window) (bool, bool) {
	next, ok := p.view.jumpMatch(p.cursor, direction)
	if !ok {
		return false, false
	}

	return p.MoveTo(next, window), true
}

// Keep puts the reader back on what they were standing on after the rows were built again, and
// failing that back inside the view.
func (p *Pane) Keep(place Place, ok bool) {
	was := p.cursor

	p.cursor = min(p.cursor, p.view.GetLastRow())
	if ok {
		if found, located := p.view.locate(place); located {
			p.cursor = found
		}
	}

	if p.cursor != was {
		p.pinTop = false
	}
}

// expansion is what toggling the file under the cursor did.
type expansion int

const (
	// ExpandNothing is a cursor standing in no file.
	ExpandNothing expansion = iota
	// ExpandWhole is a file now drawn whole.
	ExpandWhole
	// ExpandChanged is a file drawn as its changed lines again.
	ExpandChanged
	// ExpandNoMore is a file with nothing more to show than its hunks.
	ExpandNoMore
)

// ToggleExpanded draws the whole of the file the cursor is in, or gives back the changed lines and
// the few around them, keeping the reader on the line they were on.
func (p *Pane) ToggleExpanded(window Window) expansion {
	file, ok := p.view.GetDrawnFileAt(p.cursor)
	if !ok {
		return ExpandNothing
	}

	path := file.Path()
	expanding := !p.view.IsExpanded(path)

	// Where the cursor is standing is taken before the rows are built again.
	place, located := p.view.GetPlace(p.cursor)

	p.view.setFileExpanded(path, expanding)
	if p.view.IsExpanded(path) != expanding {
		return ExpandNoMore
	}

	p.Keep(place, located)
	p.ScrollToCursor(window)

	if expanding {
		return ExpandWhole
	}

	return ExpandChanged
}

// PanBy slides the rows sideways, stopping at their left edge.
func (p *Pane) PanBy(delta int) {
	p.pan = max(0, p.pan+delta)
}

// ScrollToCursor moves the window only when the cursor is about to leave it.
func (p *Pane) ScrollToCursor(window Window) {
	if window.Height <= 0 {
		return
	}

	// A file jump has already said where the window goes, and the margin below would drag it
	// straight back off what it was asked to show, until a wrapped row pushes the cursor out.
	if p.pinTop {
		if p.countLines(p.top, p.cursor, window) < window.Height {
			return
		}

		p.pinTop = false
	}

	margin := min(scrollMargin, max((window.Height-p.view.measureRow(p.cursor, window))/2, 0))

	// A file jump is only worth anything if the file stays at the top, so the margin gives up its
	// gap until the cursor walks back above the row the jump landed on.
	floor := 0
	if p.cursor >= p.pinFloor {
		floor = p.pinFloor
	}

	isAtTop := p.cursor == p.view.GetFirstRow()

	low, above := p.cursor, 0
	if isAtTop {
		low = 0
	}

	for low > 0 && above < margin {
		low--
		above += p.view.measureRow(low, window)
	}

	if low < p.top {
		p.top = max(low, floor)
	}

	high := p.findFirstFitting(p.cursor, window.Height-margin, window)
	if high > p.top {
		p.top = high
	}
}

// Render is the window of rows the pane shows, with the cursor's row marked as given and blank
// lines under a view shorter than the pane.
func (p *Pane) Render(window Window, held Cursor) []string {
	offset := p.pan
	if window.Wrap {
		offset = 0
	}

	// Only the rows the pane can show are drawn, a nine thousand row diff taking a tenth of a
	// second to draw whole.
	lines := make([]string, 0, window.Height)
	start := p.findWindowStart(window)
	for y := start; y < p.view.Len() && len(lines) < window.Height; y++ {
		mark := noCursor
		if y == p.cursor {
			mark = held
		}

		lines = append(lines, p.view.renderLines(y, mark, window, offset)...)
	}

	lines = lines[:min(len(lines), max(window.Height, 0))]
	p.coverWithHeader(lines, start, window)

	for len(lines) < window.Height {
		lines = append(lines, strings.Repeat(" ", window.Width))
	}

	return lines
}

// GetCursorLines is the first line of the window the cursor's row is drawn on and how many lines
// it takes.
func (p *Pane) GetCursorLines(window Window) (int, int) {
	if p.view == nil {
		return 0, 1
	}

	start := p.findWindowStart(window)
	if p.cursor < start {
		return p.cursor - start, 1
	}

	return p.countLines(start, p.cursor, window), p.view.measureRow(p.cursor, window)
}

// findWindowStart is the first row of the view the window draws.
func (p *Pane) findWindowStart(window Window) int {
	if p.view.Len() == 0 {
		return 0
	}

	last := p.findFirstFitting(p.view.Len()-1, window.Height, window)

	return max(min(p.top, last), p.findFirstFitting(p.cursor, window.Height, window))
}

// findFirstFitting is the first row from which the rows through end fit in lines.
func (p *Pane) findFirstFitting(end, lines int, window Window) int {
	first, drawn := end, p.view.measureRow(end, window)
	for first > 0 && drawn+p.view.measureRow(first-1, window) <= lines {
		first--
		drawn += p.view.measureRow(first, window)
	}

	return first
}

// countLines is how many lines the rows from start up to end take.
func (p *Pane) countLines(start, end int, window Window) int {
	lines := 0
	for y := start; y < end; y++ {
		lines += p.view.measureRow(y, window)
	}

	return lines
}

// coverWithHeader draws the header of the file the window starts inside over the window's first row.
func (p *Pane) coverWithHeader(lines []string, start int, window Window) {
	header, ok := p.view.findFileHeader(start)
	if !ok || p.cursor == start || len(lines) == 0 {
		return
	}

	lines[0] = p.view.renderRow(header, noCursor, window.Width, 0)
	for i := 1; i < min(p.view.measureRow(start, window), len(lines)); i++ {
		lines[i] = strings.Repeat(" ", window.Width)
	}
}
