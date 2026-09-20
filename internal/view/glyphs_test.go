package view

import (
	"fmt"
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

	assert.Equal(t, plainColumns, widths["repo"])
	assert.Equal(t, plainColumns, widths["mail"])
	assert.Equal(t, plainColumns, widths["busy"])
	assert.Equal(t, plainColumns, widths["child"])
}

// TestGlyphsSpellTheirOcticons pins each mark written from its number to its codepoint, since
// the private use characters are lost in transit and a wrong one draws as a box rather than
// failing.
func TestGlyphsSpellTheirOcticons(t *testing.T) {
	tests := []struct {
		glyph Glyph
		want  string
	}{
		{GlyphUnverified, "f4a3"},
		{GlyphCheckbox, "f4a7"},
		{GlyphBell, "f49a"},
		{GlyphHourglass, "f4e3"},
		{GlyphApproved, "f42e"},
		{GlyphForge, "f407"},
		{GlyphMerged, "f419"},
		{GlyphDraft, "f4dd"},
		{GlyphClosed, "f4dc"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			runes := []rune(Spell(tt.glyph))
			require.Len(t, runes, 1)
			assert.Equal(t, tt.want, fmt.Sprintf("%x", runes[0]))
		})
	}
}
