package view

import "komrad/internal/ui/paint"

// Glyph names a mark the sidebar draws, spelled out by the renderer rather than here.
type Glyph int

const (
	GlyphNone Glyph = iota
	GlyphRepo
	GlyphUnverified
	GlyphForge
	GlyphApproved
	GlyphBroken
	GlyphCheckbox
	GlyphBell
	GlyphHourglass
	GlyphMerged
	GlyphDraft
	GlyphClosed
	GlyphAlert
	GlyphMail
	GlyphPaused
	GlyphHidden
	GlyphSchedule
	GlyphWorkflow
	GlyphArchive
	GlyphSearch
	GlyphBusy
	GlyphQuiet
	GlyphChild
	GlyphFocus
	GlyphFailing
	GlyphConflicting
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
	EmphasisDim
	EmphasisFocused
)

// Cell is one drawn position, carrying either a named glyph or a letter a person typed, in a
// tone the palette turns into a colour.
type Cell struct {
	Glyph Glyph
	Text  string
	Tone  paint.Tone
	// Parts draw the cell as stretches each in its own tone, in place of its glyph and text.
	Parts []Cell
}

// IsBlank reports whether a cell has nothing to draw.
func (c Cell) IsBlank() bool {
	return c.Glyph == GlyphNone && c.Text == "" && len(c.Parts) == 0
}

// Line is one row of the agents list with every decision already made.
type Line struct {
	// Focused says the keyboard is standing on this row, which the caret in Focus is drawn from.
	Focused bool
	Focus   Cell
	Mark    Cell
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
	Matches  []Span
	Weight   Weight
	Emphasis Emphasis
	Held     Cell
	Hidden   Cell
	Mail     Cell
	Alert    Cell
	// Tally is how many rows this one stands in for, drawn as a number.
	Tally    int
	Schedule Cell
	Selected bool
}

// ColumnKind is what a column of the gutter holds.
type ColumnKind int

const (
	ColumnRepo ColumnKind = iota
	ColumnReview
	ColumnForge
	// ColumnReviewerComments counts the comments reviewer agents left, open then resolved.
	ColumnReviewerComments
	// ColumnPersonComments counts the comments the person left, open then resolved, with a dot
	// while any of them holds something unread.
	ColumnPersonComments
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

// WorkColumns is the gutter of the agents list: repo, review and forge, one glyph each.
func WorkColumns() Columns {
	return Columns{
		{Kind: ColumnRepo, Width: plainColumns},
		{Kind: ColumnReview, Width: plainColumns},
		{Kind: ColumnForge, Width: plainColumns},
	}
}

// CommentedWorkColumns is the agents list's gutter with the comment counts of reviewer agents and
// of the person drawn between review and forge, each as wide as its widest pair.
func CommentedWorkColumns(reviewer, person int) Columns {
	return Columns{
		{Kind: ColumnRepo, Width: plainColumns},
		{Kind: ColumnReview, Width: plainColumns},
		{Kind: ColumnReviewerComments, Width: max(reviewer, countColumns)},
		{Kind: ColumnPersonComments, Width: max(person, countColumns) + plainColumns},
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
}
