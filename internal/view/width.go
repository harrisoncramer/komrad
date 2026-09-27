package view

import (
	"strconv"

	"komrad/internal/glyph"
	"komrad/internal/ui/paint/label"
)

// plainColumns is how many columns one glyph takes.
const plainColumns = glyph.Columns

// spaceColumns is the space a cell carries after its glyph.
const spaceColumns = 1

// plainCells is the focus, the mark and the bar, which no width gives up.
const plainCells = 3

// GutterColumns is how wide everything left of a name is.
func GutterColumns(columns Columns) int {
	// Every cell holds its width and the space after it, a cell with nothing to draw included.
	width := plainCells * (plainColumns + spaceColumns)
	for _, column := range columns {
		width += column.Width + spaceColumns
	}

	return width
}

// countPairColumns is how wide one side's pair of counts is drawn, nothing when it has none.
func countPairColumns(comments *Comments) int {
	if comments == nil {
		return 0
	}

	return len(strconv.Itoa(comments.Open)) + plainColumns + len(strconv.Itoa(comments.Resolved))
}

// NameColumns is how many columns a name has in a pane of the given width.
func NameColumns(pane int, columns Columns) int {
	return max(pane-GutterColumns(columns), 0)
}

// ruleColumns is the corner a child hangs off its parent by, and the space after it.
const ruleColumns = 2

// indentColumns is how far a name is set in under the row above it.
const indentColumns = 2

// trailingColumns is everything a row draws after its name, each part a space away from what
// precedes it.
func trailingColumns(line Line) int {
	columns := 0
	for _, cell := range []Cell{line.Held, line.Hidden, line.Document, line.Mail, line.Alert, line.Schedule} {
		if !cell.IsBlank() {
			columns += plainColumns + spaceColumns
		}
	}

	if line.Tally > 0 {
		columns += plainColumns + len(label.Tally(line.Tally))
	}

	return columns
}

// roomForName is what one row has left for its name once its own corner and the marks after its
// name have taken theirs.
func roomForName(line Line, width int) int {
	spent := trailingColumns(line) + line.Indent
	if line.Rule {
		spent += ruleColumns
	}
	if line.Lead != "" {
		spent += len([]rune(line.Lead)) + spaceColumns
	}

	return width - spent
}
