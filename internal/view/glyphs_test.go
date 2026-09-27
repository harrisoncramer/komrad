package view

import (
	"testing"

	"komrad/internal/glyph"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDrawingsCoverEveryGlyphAndTheChrome covers the one way this list rots.
func TestDrawingsCoverEveryGlyphAndTheChrome(t *testing.T) {
	drawings := Drawings(glyph.Defaults())
	require.Len(t, drawings, len(glyph.All())+len(glyph.Chrome()))

	for _, drawing := range drawings {
		assert.NotEmpty(t, drawing.Text, "%s has nothing to draw", drawing.Name)
		assert.Equal(t, plainColumns, drawing.Columns, "%s is counted as other than one glyph wide", drawing.Name)
	}
}

// TestDrawingsMeasureTheSetGiven covers the doctor measuring what a person chose rather than
// what ships.
func TestDrawingsMeasureTheSetGiven(t *testing.T) {
	set, err := glyph.Load(map[glyph.Glyph]string{glyph.FailedScript: ""})
	require.NoError(t, err)

	texts := map[string]string{}
	for _, drawing := range Drawings(set) {
		texts[drawing.Name] = drawing.Text
	}

	assert.Equal(t, "", texts[glyph.FailedScript.Key()])
}
