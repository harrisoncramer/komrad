package view

import "fmt"

// plainColumns is how many columns one glyph takes.
const plainColumns = 1

// spaceColumns is the space a cell carries after its glyph.
const spaceColumns = 1

// plainCells is the focus, the mark and the bar, which no width gives up.
const plainCells = 3

// minNameColumns is the narrowest a name is squeezed to before a column gives way instead.
const minNameColumns = 10

// drawn is how many of the three columns a list draws.
func (c Columns) drawn() int {
	count := 0
	for _, column := range []bool{c.Repo, c.Review, c.Forge} {
		if column {
			count++
		}
	}

	return count
}

// GutterColumns is how wide everything left of a name is, which is what a caller takes off the
// pane to reach the column a name has.
func GutterColumns(columns Columns) int {
	// Every cell holds one glyph and the space after it, a cell with nothing to draw included.
	cells := plainCells + columns.drawn()

	return cells * (plainColumns + spaceColumns)
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
	for _, cell := range []Cell{line.Held, line.Mail, line.Alert, line.Schedule} {
		if !cell.IsBlank() {
			columns += plainColumns + spaceColumns
		}
	}

	if line.Tally > 0 {
		columns += plainColumns + len(fmt.Sprintf("+%d", line.Tally))
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

	return width - spent
}

// drawnColumnCells is the three columns a list is wide enough to draw, in the order work moves
// through them.
func drawnColumnCells(line Line, columns Columns) []Cell {
	cells := make([]Cell, 0, 3)
	if columns.Repo {
		cells = append(cells, line.Repo)
	}
	if columns.Review {
		cells = append(cells, line.Review)
	}
	if columns.Forge {
		cells = append(cells, line.Forge)
	}

	return cells
}
