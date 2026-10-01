package view

import (
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
)

// Weight is the structure a row carries in its own lettering.
type Weight int

const (
	WeightRegular Weight = iota
	WeightBold
	WeightItalic
)

// Emphasis is how brightly a row's name is drawn.
type Emphasis int

const (
	EmphasisNormal Emphasis = iota
	EmphasisDim
	EmphasisFocused
)

// Cell is one drawn position, carrying either a named glyph or text, in a tone the palette turns
// into a colour.
type Cell struct {
	Glyph glyph.Glyph
	Text  string
	Tone  paint.Tone
	// Parts draw the cell as stretches each in its own tone, in place of its glyph and text.
	Parts []Cell
}

// IsBlank reports whether a cell has nothing to draw.
func (c Cell) IsBlank() bool {
	return c.Glyph == glyph.None && c.Text == "" && len(c.Parts) == 0
}

// Line is one row of the agents list with every decision already made.
type Line struct {
	// Focused says the keyboard is standing on this row, which the caret in Focus is drawn from.
	Focused bool
	Focus   Cell
	// Columns are the cells of the list's own columns, one per column in the order they are drawn.
	Columns []Cell
	Status  Cell
	// Rule draws the corner a child hangs off, inside the name cell rather than before it.
	Rule bool
	// Indent is how far this row's name is set in from the gutter.
	Indent int
	Lead   string
	Name   string
	// Matches are the stretches of the name a search picked out, in bytes of the name as it is
	// drawn rather than as it was gathered.
	Matches []Span
	// Editing says the name is text a person is typing into, drawn with the text cursor in it.
	Editing bool
	// Behind is how many runes of the name follow the text cursor.
	Behind   int
	Weight   Weight
	Emphasis Emphasis
	Held     Cell
	Hidden   Cell
	Document Cell
	Mail     Cell
	Alert    Cell
	// Tally is how many rows this one stands in for, drawn as a number.
	Tally    int
	Schedule Cell
	// Comments are the comment counts, drawn against the right edge of the pane.
	Comments []Cell
	Selected bool
	// Divider draws the row as a rule across the pane in place of everything else.
	Divider bool
}

// ColumnKind is what a column of the gutter holds.
type ColumnKind int

const (
	ColumnRepo ColumnKind = iota
	ColumnForge
	// ColumnText is a column the row fills itself, right-aligned in the width the list declares.
	ColumnText
)

// Column is one column of the gutter a list draws between the mark and the bar.
type Column struct {
	Kind  ColumnKind
	Width int
}

// Columns is the gutter a list declares, in the order its columns are drawn.
type Columns []Column

// workColumns is the gutter of the agents list: repo and forge, one glyph each.
func workColumns() Columns {
	return Columns{
		{Kind: ColumnRepo, Width: plainColumns},
		{Kind: ColumnForge, Width: plainColumns},
	}
}

// NoColumns is the gutter of a list with nothing to say before a name.
func NoColumns() Columns {
	return Columns{}
}

// List is the agents list resolved to glyphs and tones.
type List struct {
	Lines   []Line
	Columns Columns
	// Glyphs is the character each glyph in a cell is drawn with.
	Glyphs glyph.Set
}
