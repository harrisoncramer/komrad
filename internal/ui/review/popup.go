package ui

import "strings"

const (
	popupWidth    = 92
	popupMinWidth = 40
	popupHeight   = 60
	popupChrome   = 4
)

// popupAnchor is the columns from the left edge a popup fills, up to the code beside them.
type popupAnchor struct {
	right int
	// codeOnRight is set when the code lies right of the columns.
	codeOnRight bool
}

// measureWidth is how wide a popup stands in the anchor's columns, running into the code only to
// keep its narrowest width.
func (a popupAnchor) measureWidth() int {
	return max(min(popupWidth, a.right), popupMinWidth)
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

func (p popupSlot) contains(x, y int) bool {
	return x >= p.x && x < p.x+p.width && y >= p.y && y < p.y+p.height
}

// popupScroll is how far a popup has scrolled back from the end of the body it holds, and whose
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

// reset forgets whose body the popup scrolls over.
func (p *popupScroll) reset() {
	p.owner = ""
}

// back is how many rows the popup has scrolled back from the end of an owner's body, none when
// the scroll belongs to another owner.
func (p popupScroll) back(owner string) int {
	if p.owner != owner {
		return 0
	}

	return p.rows
}

// popupStage is the rows a popup stands over: the first it may take, the first and last lines the
// cursor's row renders on and how many there are.
type popupStage struct {
	top        int
	cursor     int
	cursorLast int
	rows       int
}

// placePopup stands a popup as tall as asked, up to its tallest, over the cursor's row of a stage
// when its tallest fits there or above is the roomier side, and under it otherwise, against the
// code beside the anchor's columns.
func placePopup(anchor popupAnchor, stage popupStage, tall int) popupSlot {
	above, below := stage.cursor-stage.top, stage.rows-stage.cursorLast-1

	want := min(tall, popupHeight)
	height := min(want, below)
	y := stage.cursorLast + 1
	if above >= popupHeight || (below < popupHeight && above >= below) {
		height = min(want, above)
		y = stage.cursor - height
	}

	width := anchor.measureWidth()
	x := 0
	if anchor.codeOnRight && width <= anchor.right {
		x = anchor.right - width
	}

	return popupSlot{
		x:      x,
		y:      y,
		width:  width,
		height: height,
	}
}

// splitPopupRows is the rows of a body a popup holds, less the empty rows it ends with.
func splitPopupRows(body string) []string {
	return strings.Split(strings.TrimRight(body, "\n"), "\n")
}
