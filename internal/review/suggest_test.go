package review

import (
	"testing"

	"weave/internal/diff"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSuggestIsTheSpanBetweenTheFirstAndLastChange covers what a reviewer's edit of the round
// comes to, including the two edges a range holding nothing has to be widened onto.
func TestSuggestIsTheSpanBetweenTheFirstAndLastChange(t *testing.T) {
	round := []string{"one", "two", "three", "four"}

	tests := []struct {
		name   string
		edited []string
		want   Suggestion
	}{
		{
			name:   "one line rewritten",
			edited: []string{"one", "TWO", "three", "four"},
			want:   Suggestion{NewStart: 2, NewEnd: 2, Lines: []string{"TWO"}},
		},
		{
			name:   "two edits are one span, carrying what is between them",
			edited: []string{"ONE", "two", "three", "FOUR"},
			want:   Suggestion{NewStart: 1, NewEnd: 4, Lines: []string{"ONE", "two", "three", "FOUR"}},
		},
		{
			name:   "a line taken out",
			edited: []string{"one", "three", "four"},
			want:   Suggestion{NewStart: 2, NewEnd: 2, Lines: nil},
		},
		{
			name:   "a line put in replaces the one above it",
			edited: []string{"one", "two", "extra", "three", "four"},
			want:   Suggestion{NewStart: 2, NewEnd: 2, Lines: []string{"two", "extra"}},
		},
		{
			name:   "a line put in at the top replaces the one below it",
			edited: []string{"extra", "one", "two", "three", "four"},
			want:   Suggestion{NewStart: 1, NewEnd: 1, Lines: []string{"extra", "one"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := Suggest("greet.go", round, tt.edited)
			require.True(t, changed)

			assert.Equal(t, "greet.go", got.FilePath)
			assert.Equal(t, tt.want.NewStart, got.NewStart)
			assert.Equal(t, tt.want.NewEnd, got.NewEnd)
			assert.Equal(t, tt.want.Lines, sliceOrNil(got.Lines))
		})
	}
}

// TestSuggestSaysNothingAboutAnUntouchedFile covers the editor closing on a file nobody saved,
// which proposes nothing rather than an empty replacement.
func TestSuggestSaysNothingAboutAnUntouchedFile(t *testing.T) {
	round := []string{"one", "two"}

	_, changed := Suggest("greet.go", round, []string{"one", "two"})

	assert.False(t, changed)
}

// TestGetRangeAnchorRefusesWhatTheDiffDoesNotDraw covers a suggestion made over lines the patch
// has no rows for, which has nowhere on screen for a comment to stand.
func TestGetRangeAnchorRefusesWhatTheDiffDoesNotDraw(t *testing.T) {
	view := NewView(patchWith(diff.File{
		OldPath: "greet.go",
		NewPath: "greet.go",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{Kind: diff.Context, Text: "package greet", OldNumber: 1, NewNumber: 1},
			diff.Line{Kind: diff.Addition, Text: "added", NewNumber: 2},
		)},
	}), false)

	anchor, ok := view.GetRangeAnchor("greet.go", 1, 2)
	require.True(t, ok)
	assert.Equal(t, 1, anchor.OldStart)
	assert.Equal(t, 1, anchor.OldEnd)
	assert.Equal(t, 1, anchor.NewStart)
	assert.Equal(t, 2, anchor.NewEnd)
	assert.Equal(t, []string{" package greet", "+added"}, anchor.QuotedLines)

	_, ok = view.GetRangeAnchor("greet.go", 2, 9)
	assert.False(t, ok, "a range running past what the patch draws is refused")

	_, ok = view.GetRangeAnchor("elsewhere.go", 1, 1)
	assert.False(t, ok, "and so is a file the diff does not hold")
}

// TestGetSourceRefusesAFileWithNoTextDrawn covers a binary or an omitted file, which has no lines
// for an edit to be measured against.
func TestGetSourceRefusesAFileWithNoTextDrawn(t *testing.T) {
	view := NewView(patchWith(
		diff.File{
			OldPath:   "greet.go",
			NewPath:   "greet.go",
			NewSource: "package greet\n",
			Hunks:     []diff.Hunk{hunkOf(diff.Line{Kind: diff.Context, Text: "package greet", OldNumber: 1, NewNumber: 1})},
		},
		diff.File{OldPath: "logo.png", NewPath: "logo.png", Content: "aaa..bbb"},
	), false)

	source, ok := view.GetSource("greet.go")
	require.True(t, ok)
	assert.Equal(t, []string{"package greet"}, SplitSource(source))

	_, ok = view.GetSource("logo.png")
	assert.False(t, ok)
}

func sliceOrNil(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}

	return lines
}
