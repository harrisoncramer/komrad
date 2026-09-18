package view

import "fmt"

// gutterGlyphColumns is how many columns one Octicon takes, which is what the font draws rather
// than what Unicode declares.
const gutterGlyphColumns = 2

// plainColumns is how many columns every other glyph takes.
const plainColumns = 1

// fixedCells is the focus, the mark, the three columns and the bar, each one followed by a
// space.
const fixedCells = 6

// GutterColumns is how wide everything left of a name is, which is what a caller takes off the
// pane to reach the column a name has.
func GutterColumns() int {
	octicons := 3 * gutterGlyphColumns
	rest := (fixedCells - 3) * plainColumns
	separators := fixedCells

	return octicons + rest + separators
}

// NameColumns is the column a name has in a pane of the given width.
func NameColumns(pane int) int {
	return max(pane-GutterColumns(), 0)
}

// ruleColumns is the corner a child hangs off its parent by, and the space after it.
const ruleColumns = 2

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
// name have taken theirs, keeping a column for the name however much is spent.
func nameColumns(line Line, width int) int {
	if width <= 0 {
		return width
	}

	spent := trailingColumns(line)
	if line.Rule {
		spent += ruleColumns
	}

	return max(width-spent, 1)
}

// extraColumns is how far a drawn row overruns what lipgloss measures of it, since an Octicon
// sits in the private use area and is measured as one column there.
func extraColumns(line Line) int {
	extra := 0
	for _, cell := range []Cell{line.Focus, line.Mark, line.Repo, line.Review, line.Forge, line.Status, line.Held, line.Mail, line.Alert, line.Schedule} {
		extra += glyphColumns(cell.Glyph) - plainColumns
	}

	return extra
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
