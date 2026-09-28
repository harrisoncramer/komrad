package diffview

import (
	"regexp"
	"strings"

	"komrad/internal/ui/cursor"
)

// scrollMargin is how close to the edge of the pane the cursor gets before the rows scroll under
// it.
const scrollMargin = 4

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

	if found, ok := view.Locate(*place); ok {
		p.cursor = found
	}
}

// Drop puts the view down, keeping where the window and the pan stood for the next one.
func (p *Pane) Drop() {
	p.view = nil
}

// MoveTo stands the reader on a row and scrolls a window of the given height to keep it in sight,
// reporting whether the reader moved.
func (p *Pane) MoveTo(row, height int) bool {
	if row == p.cursor {
		return false
	}

	p.cursor = row
	p.pinTop = false
	p.ScrollToCursor(height)

	return true
}

// Step moves the reader a number of rows up or down.
func (p *Pane) Step(direction, rows, height int) bool {
	cursor := p.cursor
	for range rows {
		cursor = p.view.Move(cursor, direction)
	}

	return p.MoveTo(cursor, height)
}

// JumpFile moves to the next or previous file and puts that file's header at the top of the pane.
func (p *Pane) JumpFile(direction, height int) bool {
	next := p.view.JumpFile(p.cursor, direction)
	if next == p.cursor {
		return false
	}

	p.cursor = next
	p.top = cursor.Clamp(p.view.GetFileTopRow(next), p.view.Len())
	p.pinTop = true
	p.pinFloor = p.top
	p.ScrollToCursor(height)

	return true
}

// Search runs a pattern over the view and stands the reader on the first match at or after them.
func (p *Pane) Search(pattern *regexp.Regexp, height int) {
	if p.view.SetPattern(pattern) == 0 {
		return
	}

	if next, ok := p.view.JumpMatch(p.cursor-1, 1); ok {
		p.cursor = next
		p.pinTop = false
	}

	p.ScrollToCursor(height)
}

// JumpMatch steps to the next or previous line the search found, wrapping at the ends, and
// reports false while nothing was found.
func (p *Pane) JumpMatch(direction, height int) (bool, bool) {
	next, ok := p.view.JumpMatch(p.cursor, direction)
	if !ok {
		return false, false
	}

	return p.MoveTo(next, height), true
}

// Keep puts the reader back on what they were standing on after the rows were built again, and
// failing that back inside the view.
func (p *Pane) Keep(place Place, ok bool) {
	was := p.cursor

	p.cursor = min(p.cursor, p.view.GetLastRow())
	if ok {
		if found, located := p.view.Locate(place); located {
			p.cursor = found
		}
	}

	if p.cursor != was {
		p.pinTop = false
	}
}

// Expansion is what toggling the file under the cursor did.
type Expansion int

const (
	// ExpandNothing is a cursor standing in no file.
	ExpandNothing Expansion = iota
	// ExpandWhole is a file now drawn whole.
	ExpandWhole
	// ExpandChanged is a file drawn as its changed lines again.
	ExpandChanged
	// ExpandNoMore is a file with nothing more to show than its hunks.
	ExpandNoMore
)

// ToggleExpanded draws the whole of the file the cursor is in, or gives back the changed lines and
// the few around them, keeping the reader on the line they were on.
func (p *Pane) ToggleExpanded(height int) Expansion {
	file, ok := p.view.GetDrawnFileAt(p.cursor)
	if !ok {
		return ExpandNothing
	}

	path := file.Path()
	expanding := !p.view.IsExpanded(path)

	// Where the cursor is standing is taken before the rows are built again.
	place, located := p.view.GetPlace(p.cursor)

	p.view.SetFileExpanded(path, expanding)
	if p.view.IsExpanded(path) != expanding {
		return ExpandNoMore
	}

	p.Keep(place, located)
	p.ScrollToCursor(height)

	if expanding {
		return ExpandWhole
	}

	return ExpandChanged
}

// PanBy slides the rows sideways, stopping at their left edge.
func (p *Pane) PanBy(delta int) {
	p.pan = max(0, p.pan+delta)
}

// ScrollToCursor moves a window of the given height only when the cursor is about to leave it.
func (p *Pane) ScrollToCursor(height int) {
	// A file jump has already said where the window goes, and the margin below would drag it
	// straight back off what it was asked to show.
	if p.pinTop || height <= 0 {
		return
	}

	margin := min(scrollMargin, max((height-1)/2, 0))

	// A file jump is only worth anything if the file stays at the top, so the margin gives up its
	// gap until the cursor walks back above the row the jump landed on.
	floor := 0
	if p.cursor >= p.pinFloor {
		floor = p.pinFloor
	}

	if low := p.cursor - margin; low < p.top {
		p.top = max(low, floor)
	}

	if high := p.cursor - height + 1 + margin; high > p.top {
		p.top = high
	}
}

// Render is the window of rows the pane shows at a width and height, with the cursor's row marked
// as given and blank rows under a view shorter than the pane.
func (p *Pane) Render(width, height int, held Cursor) []string {
	start := max(min(p.top, p.view.Len()-height), 0)

	// Only the rows the pane can show are drawn, a nine thousand row diff taking a tenth of a
	// second to draw whole.
	rows := make([]string, 0, height)
	for y := start; y < min(start+height, p.view.Len()); y++ {
		mark := NoCursor
		if y == p.cursor {
			mark = held
		}

		rows = append(rows, p.view.RenderRow(y, mark, width, p.pan))
	}

	for len(rows) < height {
		rows = append(rows, strings.Repeat(" ", width))
	}

	return rows
}
