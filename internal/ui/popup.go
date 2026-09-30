package ui

import (
	"strings"

	"komrad/internal/ui/paint"
	"komrad/lib/xslice"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	popupWidth  = 92
	popupHeight = 14
	popupChrome = 4
)

// popupFrame is the box a popup is drawn in over an area, and where it stands.
type popupFrame struct {
	box string
	x   int
	y   int
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

// reset forgets the scroll, so the next body opens on its end.
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

// measurePopupWidth is the width a popup takes over a column of the width given.
func measurePopupWidth(column int) int {
	return max(min(popupWidth, column), popupChrome+1)
}

// measurePopupInner is the room inside a popup over a column of the width given.
func measurePopupInner(column int) int {
	return measurePopupWidth(column) - popupChrome
}

// overlayPopup draws a body in a popup beside the cursor's row of an area, from the column left,
// and leaves the area alone when the body is empty.
func overlayPopup(area, body string, cursor, left int) string {
	if body == "" {
		return area
	}

	rows := paint.CountLines(area)
	width := lipgloss.Width(area)
	box := drawPopup(body, measurePopupWidth(width-left), min(popupHeight, rows))
	frame := placePopup(box, cursor, rows, left)

	return compose(width, strings.Count(area, "\n")+1, []*lipgloss.Layer{
		lipgloss.NewLayer(area),
		lipgloss.NewLayer(frame.box).X(frame.x).Y(frame.y).Z(1),
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

// placePopup stands a popup under the cursor's row, or over it when there is no room beneath,
// inside an area of the rows given.
func placePopup(box string, cursor, rows, left int) popupFrame {
	height := paint.CountLines(box)
	y := cursor + 1
	if y+height > rows {
		y = max(cursor-height, 0)
	}

	return popupFrame{
		box: box,
		x:   left,
		y:   y,
	}
}

// fitPopupRow cuts or pads a row to the popup's inner width, so the area under it never shows
// through.
func fitPopupRow(row string, width int) string {
	row = ansi.Truncate(row, width, "…")

	return row + strings.Repeat(" ", max(width-lipgloss.Width(row), 0))
}
