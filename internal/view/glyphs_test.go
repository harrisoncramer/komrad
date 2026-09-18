package view

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDrawingsCoverEveryGlyph covers the one way this list rots, which is a glyph being added to
// the enum and never measured because nobody thought of the doctor.
func TestDrawingsCoverEveryGlyph(t *testing.T) {
	named := map[Glyph]bool{}
	for _, entry := range glyphNames {
		named[entry.glyph] = true
	}

	for glyph := GlyphNone + 1; glyph <= GlyphFocus; glyph++ {
		assert.True(t, named[glyph], "glyph %d is drawn but is not in the inventory the doctor measures", glyph)
	}
}

// TestDrawingsCarryTextAndWidth covers that the inventory is worth measuring, since a drawing
// with no text would measure zero and read as a terminal fault.
func TestDrawingsCarryTextAndWidth(t *testing.T) {
	drawings := Drawings()
	require.Len(t, drawings, int(GlyphFocus))

	for _, drawing := range drawings {
		assert.NotEmpty(t, drawing.Text, "%s has nothing to draw", drawing.Name)
		assert.Positive(t, drawing.Columns, "%s is counted as no columns at all", drawing.Name)
	}
}

// TestDrawingsAgreeWithTheLayout covers the constant the whole package's arithmetic rests on,
// so a change to it is a change to a test rather than a silent shift in every row.
func TestDrawingsAgreeWithTheLayout(t *testing.T) {
	widths := map[string]int{}
	for _, drawing := range Drawings() {
		widths[drawing.Name] = drawing.Columns
	}

	assert.Equal(t, gutterGlyphColumns, widths["repo"])
	assert.Equal(t, gutterGlyphColumns, widths["mail"])
	assert.Equal(t, plainColumns, widths["busy"])
	assert.Equal(t, plainColumns, widths["child"])
}
