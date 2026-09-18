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
	GlyphChild
	GlyphFocus
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
)

// Cell is one drawn position: a glyph named rather than spelled, in a tone the palette turns
// into a colour.
type Cell struct {
	Glyph Glyph
	Tone  paint.Tone
}

// Line is one row of the agents list with every decision already made.
type Line struct {
	Focused bool
	Mark    string
	Repo    Cell
	Review  Cell
	Forge   Cell
	Status  Cell
	// Indent is the spaces the name is pushed across by, which is what says a row hangs under
	// the workflow above it.
	Indent int
	// Rule draws the corner a child hangs off.
	Rule     bool
	Name     string
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

// List is the agents list resolved to glyphs and tones.
type List struct {
	Lines []Line
}
