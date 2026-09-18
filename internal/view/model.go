package view

import "weave/internal/ui/paint"

// Glyph names a mark the sidebar draws, spelled out by the renderer rather than here.
type Glyph int

const (
	GlyphNone Glyph = iota
	GlyphRepo
	GlyphReview
	GlyphForge
	GlyphApproved
	GlyphBroken
	GlyphChanges
	GlyphMerged
	GlyphAlert
	GlyphMail
	GlyphPaused
	GlyphSchedule
	GlyphBusy
	GlyphQuiet
	GlyphStopped
	GlyphChild
	GlyphFocus
	// glyphCount stands one past the last glyph.
	glyphCount
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
	EmphasisBright
	EmphasisDim
	EmphasisFocused
)

// Cell is one drawn position, carrying either a named glyph or a letter a person typed, in a
// tone the palette turns into a colour.
type Cell struct {
	Glyph Glyph
	Text  string
	Tone  paint.Tone
}

// IsBlank reports whether a cell has nothing to draw.
func (c Cell) IsBlank() bool {
	return c.Glyph == GlyphNone && c.Text == ""
}

// Line is one row of the agents list with every decision already made.
type Line struct {
	// Focused says the keyboard is standing on this row, which the caret in Focus is drawn from.
	Focused bool
	Focus   Cell
	Mark    Cell
	Repo    Cell
	Review  Cell
	Forge   Cell
	Status  Cell
	// Rule draws the corner a child hangs off, inside the name cell rather than before it.
	Rule bool
	// Indent is how far this row's name is set in from the gutter.
	Indent int
	Name   string
	// Tag is a word drawn dim after the name, for what the symbology has no glyph for.
	Tag string
	// Matches are the stretches of the name a search picked out, in bytes of the name as it is
	// drawn rather than as it was gathered.
	Matches  []Span
	Weight   Weight
	Emphasis Emphasis
	Held     Cell
	Mail     Cell
	Alert    Cell
	// Tally is how many rows this one stands in for, drawn as a number.
	Tally    int
	Schedule Cell
	Selected bool
}

// Columns says which of the three columns a list is wide enough to draw.
type Columns struct {
	Repo   bool
	Review bool
	Forge  bool
}

// List is the agents list resolved to glyphs and tones.
type List struct {
	Lines   []Line
	Columns Columns
}
