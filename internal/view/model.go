package view

import (
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
)

// weight is the structure a row carries in its own lettering.
type weight int

const (
	weightRegular weight = iota
	weightBold
	weightItalic
)

// emphasis is how brightly a row's name renders.
type emphasis int

const (
	emphasisNormal emphasis = iota
	EmphasisDim
	emphasisFocused
	// emphasisAlert is an agent waiting on a person or broken, which wants that person.
	emphasisAlert
)

// Cell is one rendered position, carrying either a named glyph or text, in a tone the palette turns
// into a colour.
type Cell struct {
	Glyph glyph.Glyph
	Text  string
	Tone  paint.Tone
	// Parts render the cell as stretches each in its own tone, in place of its glyph and text.
	Parts []Cell
}

// IsBlank reports whether a cell has nothing to render.
func (c Cell) IsBlank() bool {
	return c.Glyph == glyph.None && c.Text == "" && len(c.Parts) == 0
}

// line is one row of the agents list with every decision already made.
type line struct {
	// Focused says the keyboard is standing on this row, which the caret in Focus is rendered from.
	Focused bool
	Focus   Cell
	// Columns are the cells of the list's own columns, one per column in the order they are rendered.
	Columns []Cell
	// Branch is the tree a row hangs in, rendered greyed before its name.
	Branch string
	// Rule renders the corner a nested row hangs off, inside the name cell rather than before it.
	Rule bool
	// Indent is how far this row's name is set in from the gutter.
	Indent int
	Lead   string
	// Role is the mark drawn before a reviewer agent's name.
	Role Cell
	Name string
	// Matches are the stretches of the name a search picked out, in bytes of the name as it is
	// rendered rather than as it was gathered.
	Matches []Span
	// Editing says the name is text a person is typing into, rendered with the text cursor in it.
	Editing bool
	// Behind is how many runes of the name follow the text cursor.
	Behind   int
	Weight   weight
	Emphasis emphasis
	// Working is the loader turning after the name of an agent at work.
	Working  Cell
	Held     Cell
	Hidden   Cell
	Document Cell
	Mail     Cell
	Alert    Cell
	// Tool is the tool the agent is running, rendered greyed after the marks.
	Tool Cell
	// Tally is how many rows this one stands in for, rendered as a number.
	Tally    int
	Schedule Cell
	// Workflow is the workflow's name, rendered greyed after the marks.
	Workflow string
	Tail     Cell
	// Comments are the comment counts, rendered against the right edge of the pane.
	Comments []Cell
	Selected bool
	// Divider renders the row as a rule across the pane in place of everything else.
	Divider bool
}

// columnKind is what a column of the gutter holds.
type columnKind int

const (
	columnRepo columnKind = iota
	columnForge
	// ColumnText is a column the row fills itself, right-aligned in the width the list declares.
	ColumnText
)

// Column is one column of the gutter a list renders between the mark and the name.
type Column struct {
	Kind  columnKind
	Width int
}

// Columns is the gutter a list declares, in the order its columns render.
type Columns []Column

// workColumns is the gutter of the agents list: repo and forge, one glyph each.
func workColumns() Columns {
	return Columns{
		{Kind: columnRepo, Width: plainColumns},
		{Kind: columnForge, Width: plainColumns},
	}
}

// noColumns is the gutter of a list with nothing to say before a name.
func noColumns() Columns {
	return Columns{}
}

// List is the agents list resolved to glyphs and tones.
type List struct {
	Lines   []line
	Columns Columns
	// Glyphs is the character each glyph in a cell is rendered with.
	Glyphs glyph.Set
}
