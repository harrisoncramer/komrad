package diffview

import (
	"testing"

	"komrad/internal/diff"

	"github.com/stretchr/testify/assert"
)

func offerPatch() *diff.Patch {
	return patchWith(diff.File{
		OldPath: "greet.go",
		NewPath: "greet.go",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{Kind: diff.Context, Text: "package greet", OldNumber: 1, NewNumber: 1},
			diff.Line{Kind: diff.Deletion, Text: "return nil", OldNumber: 2},
			diff.Line{Kind: diff.Addition, Text: "return nil", NewNumber: 2},
			diff.Line{Kind: diff.Addition, Text: "}", NewNumber: 3},
			diff.Line{Kind: diff.Addition, Text: "}", NewNumber: 4},
		)},
	})
}

// TestPinOffers covers the row each forge comment is drawn on: where git traced its line, then
// the one line on its side reading as it quoted, then its file's header.
func TestPinOffers(t *testing.T) {
	tests := []struct {
		name    string
		offer   Offer
		traced  map[string]Traced
		viewed  bool
		row     int
		placed  bool
		drawn   bool
		without bool
	}{
		{
			name:   "a traced line is drawn where git says it went",
			offer:  Offer{ID: "o", FilePath: "greet.go", Line: 9, Quote: "gone"},
			traced: map[string]Traced{"o": {FilePath: "greet.go", NewStart: 3}},
			row:    6,
			placed: true,
			drawn:  true,
		},
		{
			name:   "an untraced line falls back to the one line on its side quoting it",
			offer:  Offer{ID: "o", FilePath: "greet.go", Line: 40, Quote: "return nil"},
			row:    5,
			placed: true,
			drawn:  true,
		},
		{
			name:   "an old side quote is found on the old side",
			offer:  Offer{ID: "o", FilePath: "greet.go", Line: 2, OldSide: true, Quote: "return nil"},
			row:    4,
			placed: true,
			drawn:  true,
		},
		{
			name:   "an old side offer is not traced along the new side",
			offer:  Offer{ID: "o", FilePath: "greet.go", Line: 2, OldSide: true},
			traced: map[string]Traced{"o": {FilePath: "greet.go", NewStart: 3}},
			row:    1,
			placed: true,
		},
		{
			name:   "a quote two lines read the same way goes to the file",
			offer:  Offer{ID: "o", FilePath: "greet.go", Line: 40, Quote: "}"},
			row:    1,
			placed: true,
		},
		{
			name:   "a comment on the file goes to its header",
			offer:  Offer{ID: "o", FilePath: "greet.go"},
			row:    1,
			placed: true,
		},
		{
			name:   "a comment in a folded file goes to its header",
			offer:  Offer{ID: "o", FilePath: "greet.go", Line: 1, Quote: "package greet"},
			viewed: true,
			row:    1,
			placed: true,
		},
		{
			name:  "a comment on a file outside the diff has no row",
			offer: Offer{ID: "o", FilePath: "other.go", Line: 1, Quote: "package greet"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := NewView(offerPatch(), false)
			view.SetViewed(map[string]bool{"greet.go": tt.viewed})
			view.SetOfferTraced(tt.traced)
			view.PinOffers([]Offer{tt.offer})

			row, ok := view.GetOfferRow("o")
			assert.Equal(t, tt.placed, ok)
			assert.Equal(t, tt.row, row)
			assert.Equal(t, tt.drawn, len(view.GetOffersAt(row)) == 1)
		})
	}
}

// TestOffersHoldThroughFolding covers a file folded and opened again, which rebuilds the rows the
// forge's comments stand on.
func TestOffersHoldThroughFolding(t *testing.T) {
	view := NewView(offerPatch(), false)
	view.PinOffers([]Offer{{ID: "o", FilePath: "greet.go", Line: 1, Quote: "package greet"}})

	view.SetFileViewed("greet.go", true)
	view.SetFileViewed("greet.go", false)

	row, ok := view.GetOfferRow("o")
	assert.True(t, ok)
	assert.Len(t, view.GetOffersAt(row), 1)
}
