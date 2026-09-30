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
	popupHeight   = 24
	popupChrome   = 4
)

// popupAnchor is the columns beside the code a popup fills from the side facing the code.
type popupAnchor struct {
	left  int
	right int
	// codeOnRight is set when the code lies right of the columns.
	codeOnRight bool
}

// measureWidth is how wide a popup stands in the anchor's columns, running into the code only to
// keep its narrowest width.
func (a popupAnchor) measureWidth() int {
	return max(min(popupWidth, a.right-a.left), popupMinWidth)
}

// popupSlot is where a popup stands over an area and how big it is.
type popupSlot struct {
	x      int
	y      int
	width  int
	height int
}

func (p popupSlot) room() int {
	return max(p.height-2, 1)
}

// popupScroll is how far a popup is scrolled back from the end of the body it holds, and whose
// body that is.
type popupScroll struct {
	owner string
	rows  int
}

// move scrolls the popup down by rows, up when rows is negative, in a popup showing room rows of
// the body, starting over when the body is another owner's.
func (p *popupScroll) move(owner, body string, rows, room int) {
	if p.owner != owner {
		p.owner = owner
		p.rows = 0
	}

	most := max(len(splitPopupRows(body))-room, 0)
	p.rows = min(max(p.rows-rows, 0), most)
}

// reset forgets whose body the popup is scrolled over.
func (p *popupScroll) reset() {
	p.owner = ""
}

// back is how many rows the popup is scrolled back from the end of an owner's body, none when the
// scroll is another owner's.
func (p popupScroll) back(owner string) int {
	if p.owner != owner {
		return 0
	}

	return p.rows
}

// popupStage is the rows a popup stands over: the first it may take, the row the cursor is drawn
// on and how many there are.
type popupStage struct {
	top    int
	cursor int
	rows   int
}

// placePopup stands a popup as tall as asked, up to its tallest, over the cursor's row of a stage,
// or under it when there is no room above, shrinking into the roomier side when neither holds it
// whole, against the code beside the anchor's columns.
func placePopup(anchor popupAnchor, stage popupStage, tall int) popupSlot {
	above, below := stage.cursor-stage.top, stage.rows-stage.cursor-1

	want := min(tall, popupHeight)
	height, y := want, stage.cursor-want
	switch {
	case above >= want:
	case below >= want:
		y = stage.cursor + 1
	case above >= below:
		height, y = above, stage.top
	default:
		height, y = below, stage.cursor+1
	}

	width := anchor.measureWidth()
	x := anchor.left
	if anchor.codeOnRight == (width <= anchor.right-anchor.left) {
		x = anchor.right - width
	}

	return popupSlot{
		x:      max(x, 0),
		y:      y,
		width:  width,
		height: height,
	}
}

// overlayPopup draws a body in a popup at a slot of an area, scrolled back rows from its end.
func overlayPopup(area, body string, back int, slot popupSlot) string {
	box := drawPopup(body, back, slot)

	return compose(lipgloss.Width(area), strings.Count(area, "\n")+1, []*lipgloss.Layer{
		lipgloss.NewLayer(area),
		lipgloss.NewLayer(box).X(slot.x).Y(slot.y).Z(1),
	})
}

// drawPopup boxes a body at a slot's width and height, showing the rows back rows from its end
// and marking what runs past the box above and below.
func drawPopup(body string, back int, slot popupSlot) string {
	room := slot.room()
	lines := splitPopupRows(body)
	window := paint.WindowOf(len(lines), room, func(visible int) int {
		if back >= len(lines)-room {
			return 0
		}

		return max(len(lines)-visible-back, 0)
	})

	shown := paint.DrawWindow(lines[window.Start:window.End], window, len(lines), dimStyle())
	rows := strings.Split(strings.TrimSuffix(shown, "\n"), "\n")
	for len(rows) < room {
		rows = append(rows, "")
	}

	padded := xslice.Map(rows, func(row string) string {
		return fitPopupRow(row, slot.width-popupChrome)
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

// splitPopupRows is the rows of a body a popup holds, less the empty rows it ends with.
func splitPopupRows(body string) []string {
	return strings.Split(strings.TrimRight(body, "\n"), "\n")
}
