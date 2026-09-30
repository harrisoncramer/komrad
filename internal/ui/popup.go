package ui

import (
	"strings"

	"komrad/internal/ui/paint"
	"komrad/lib/xslice"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	popupWidth    = 92
	popupMinWidth = 40
	popupHeight   = 14
	popupChrome   = 4
)

// popupAnchor is what a popup is placed by over an area.
type popupAnchor struct {
	// cursor is the row the popup stands beside.
	cursor int
	// edge is the column the popup's right edge stands against where it has room.
	edge int
	// right is the farthest column the popup reaches, which may lie past the area.
	right int
	// code is the column from which the text on the rows the popup covers is kept clear.
	code       int
	keepsClear bool
}

// popupSlot is where a popup stands over an area and how big it is.
type popupSlot struct {
	x      int
	y      int
	width  int
	height int
}

func (p popupSlot) inner() int {
	return p.width - popupChrome
}

func (p popupSlot) shift(columns int) popupSlot {
	p.x += columns

	return p
}

// popupScroll is how far a popup is scrolled back from the end of the body it holds, and whose
// body that is.
type popupScroll struct {
	owner string
	rows  int
}

// move scrolls the popup down by rows, up when rows is negative, starting over when the body is
// another owner's.
func (p *popupScroll) move(owner, body string, rows int) {
	if p.owner != owner {
		p.owner = owner
		p.rows = 0
	}

	most := max(paint.CountLines(body)-(popupHeight-2), 0)
	p.rows = min(max(p.rows-rows, 0), most)
}

// reset forgets whose body the popup is scrolled over.
func (p *popupScroll) reset() {
	p.owner = ""
}

// cut is the body as far as the popup is scrolled, the whole of it when the scroll is another
// owner's.
func (p popupScroll) cut(owner, body string) string {
	if p.owner != owner {
		return body
	}

	return cutPopupEnd(body, p.rows)
}

// placePopup stands a popup over the cursor's row of an area, or under it when there is no room
// above, against the anchor's edge and clear of the text it covers where there is room.
func placePopup(area string, anchor popupAnchor) popupSlot {
	lines := strings.Split(area, "\n")
	height := min(popupHeight, len(lines))

	y := anchor.cursor - height
	if y < 0 {
		y = anchor.cursor + 1
	}
	if y+height > len(lines) {
		y = max(len(lines)-height, 0)
	}

	start := 0
	if anchor.keepsClear {
		start = measureTextEnd(lines[y:y+height], anchor.code) + 1
	}

	width := min(popupWidth, anchor.right-start)
	x := max(anchor.edge-width, start)
	if width < popupMinWidth {
		width = max(min(popupMinWidth, anchor.right), popupChrome+1)
		x = max(min(start, anchor.right-width), 0)
	}

	return popupSlot{
		x:      x,
		y:      y,
		width:  width,
		height: height,
	}
}

// measureTextEnd is the column past the last character of text any of the lines holds from the
// column from, and from itself when none holds any.
func measureTextEnd(lines []string, from int) int {
	end := from
	for _, line := range lines {
		text := strings.TrimRight(ansi.Strip(ansi.TruncateLeft(line, from, "")), " ")
		end = max(end, from+ansi.StringWidth(text))
	}

	return end
}

// overlayPopup draws a body in a popup at a slot of an area, and leaves the area alone when the
// body is empty.
func overlayPopup(area, body string, slot popupSlot) string {
	if body == "" {
		return area
	}

	box := drawPopup(body, slot.width, slot.height)

	return compose(lipgloss.Width(area), strings.Count(area, "\n")+1, []*lipgloss.Layer{
		lipgloss.NewLayer(area),
		lipgloss.NewLayer(box).X(slot.x).Y(slot.y).Z(1),
	})
}

// cutPopupEnd drops rows off the end of a body, never leaving fewer than the popup shows.
func cutPopupEnd(body string, rows int) string {
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	keep := max(len(lines)-rows, min(popupHeight-2, len(lines)))

	return strings.Join(lines[:keep], "\n")
}

// drawPopup boxes a body at the popup's width and height, keeping the end of it when it runs
// longer than the box.
func drawPopup(body string, width, height int) string {
	inner := width - popupChrome
	rows := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if room := max(height-2, 1); len(rows) > room {
		rows = rows[len(rows)-room:]
	}

	for len(rows) < height-2 {
		rows = append(rows, "")
	}

	padded := xslice.Map(rows, func(row string) string {
		return fitPopupRow(row, inner)
	})

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(paint.Panel.Color(paint.ToneMuted)).
		Padding(0, 1).
		Render(strings.Join(padded, "\n"))
}

// fitPopupRow cuts or pads a row to the popup's inner width, so the area under it never shows
// through.
func fitPopupRow(row string, width int) string {
	row = ansi.Truncate(row, width, "…")

	return row + strings.Repeat(" ", max(width-lipgloss.Width(row), 0))
}
