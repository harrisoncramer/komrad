package view

import "fmt"

// gutterGlyphColumns is how many columns one Octicon takes, which is what the font draws rather
// than what Unicode declares.
const gutterGlyphColumns = 2

// plainColumns is how many columns every other glyph takes.
const plainColumns = 1

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

// everyColumn is the list a pane wide enough for all three draws.
func everyColumn() Columns {
	return Columns{Repo: true, Review: true, Forge: true}
}

// without gives up the columns a screen does not draw at all.
func (c Columns) without(dropped Columns) Columns {
	return Columns{
		Repo:   c.Repo && !dropped.Repo,
		Review: c.Review && !dropped.Review,
		Forge:  c.Forge && !dropped.Forge,
	}
}

// GutterColumns is how wide everything left of a name is, which is what a caller takes off the
// pane to reach the column a name has.
func GutterColumns(columns Columns) int {
	cells := plainCells + columns.drawn()
	octicons := columns.drawn() * gutterGlyphColumns
	rest := plainCells * plainColumns

	return octicons + rest + cells
}

// NameColumns is the column a name has in a pane of the given width.
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
			columns += plainColumns + glyphColumns(cell.Glyph)
		}
	}

	if line.Tally > 0 {
		columns += plainColumns + len(fmt.Sprintf("+%d", line.Tally))
	}

	return columns
}

// nameColumns is what one row has left for its name once its own corner and the marks after its
// name have taken theirs, which on a narrow pane is nothing at all.
func nameColumns(line Line, width int) int {
	spent := trailingColumns(line) + line.Indent
	if line.Rule {
		spent += ruleColumns
	}

	return width - spent
}

// extraColumns is how far a drawn row overruns what lipgloss measures of it, since an Octicon
// sits in the private use area and is measured as one column there.
func extraColumns(line Line, columns Columns) int {
	cells := []Cell{line.Focus, line.Mark, line.Status, line.Held, line.Mail, line.Alert, line.Schedule}
	cells = append(cells, drawnColumnCells(line, columns)...)

	extra := 0
	for _, cell := range cells {
		extra += glyphColumns(cell.Glyph) - plainColumns
	}

	return extra
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

// glyphColumns is how many columns one glyph takes.
func glyphColumns(glyph Glyph) int {
	switch glyph {
	case GlyphNone:
		return plainColumns
	case GlyphBusy, GlyphQuiet, GlyphChild, GlyphFocus:
		return plainColumns
	case GlyphRepo, GlyphReview, GlyphForge, GlyphApproved, GlyphBroken, GlyphChanges, GlyphMerged, GlyphAlert, GlyphMail, GlyphPaused, GlyphSchedule:
		return gutterGlyphColumns
	}

	return plainColumns
}
