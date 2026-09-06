package review

import (
	"testing"

	"weave/internal/diff"
	"weave/internal/git"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSuggestIsOneRunOfChangedLines covers what a run the reviewer wrote comes to, including the
// two edges where a run replaces nothing and has to be widened onto a line beside it.
func TestSuggestIsOneRunOfChangedLines(t *testing.T) {
	round := []string{"one", "two", "three", "four"}

	tests := []struct {
		name string
		edit git.Edit
		want Suggestion
	}{
		{
			name: "one line rewritten",
			edit: git.Edit{
				Path:  "greet.go",
				Start: 2,
				End:   2,
				Lines: []string{"TWO"},
			},
			want: Suggestion{
				NewStart: 2,
				NewEnd:   2,
				Lines:    []string{"TWO"},
			},
		},
		{
			name: "a run of lines rewritten",
			edit: git.Edit{
				Path:  "greet.go",
				Start: 2,
				End:   3,
				Lines: []string{"TWO", "THREE"},
			},
			want: Suggestion{
				NewStart: 2,
				NewEnd:   3,
				Lines:    []string{"TWO", "THREE"},
			},
		},
		{
			name: "a line taken out",
			edit: git.Edit{
				Path:  "greet.go",
				Start: 2,
				End:   2,
			},
			want: Suggestion{
				NewStart: 2,
				NewEnd:   2,
				Lines:    nil,
			},
		},
		{
			name: "a line put in replaces the one above it",
			edit: git.Edit{
				Path:  "greet.go",
				Start: 3,
				End:   2,
				Lines: []string{"extra"},
			},
			want: Suggestion{
				NewStart: 2,
				NewEnd:   2,
				Lines:    []string{"two", "extra"},
			},
		},
		{
			name: "a line put in at the top replaces the one below it",
			edit: git.Edit{
				Path:  "greet.go",
				Start: 1,
				End:   0,
				Lines: []string{"extra"},
			},
			want: Suggestion{
				NewStart: 1,
				NewEnd:   1,
				Lines:    []string{"extra", "one"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Suggest(tt.edit, round)
			require.True(t, ok)

			assert.Equal(t, "greet.go", got.FilePath)
			assert.Equal(t, tt.want.NewStart, got.NewStart)
			assert.Equal(t, tt.want.NewEnd, got.NewEnd)
			assert.Equal(t, tt.want.Lines, sliceOrNil(got.Lines))
		})
	}
}

// TestSuggestRefusesWhatTheRoundAlreadyHolds covers a run that proposes the lines already there,
// which is what an editor adding a final newline the round did not have writes.
func TestSuggestRefusesWhatTheRoundAlreadyHolds(t *testing.T) {
	round := []string{"one", "two"}

	_, ok := Suggest(git.Edit{
		Path:  "greet.go",
		Start: 2,
		End:   2,
		Lines: []string{"two"},
	}, round)

	assert.False(t, ok)
}

// TestSuggestRefusesARunPastTheEndOfTheRound covers a range the round has no lines for, which
// would otherwise be read off the end of the file it is measured against.
func TestSuggestRefusesARunPastTheEndOfTheRound(t *testing.T) {
	round := []string{"one", "two"}

	_, ok := Suggest(git.Edit{
		Path:  "greet.go",
		Start: 2,
		End:   9,
		Lines: []string{"x"},
	}, round)

	assert.False(t, ok)
}

// TestSuggestionKeySeparatesOneProposalFromAnother covers what settles a proposal, since the same
// range proposing different lines is a different thing to answer for.
func TestSuggestionKeySeparatesOneProposalFromAnother(t *testing.T) {
	first := Suggestion{
		FilePath: "greet.go",
		NewStart: 2,
		NewEnd:   2,
		Lines:    []string{"TWO"},
	}
	same := Suggestion{
		FilePath: "greet.go",
		NewStart: 2,
		NewEnd:   2,
		Lines:    []string{"TWO"},
	}
	rewritten := Suggestion{
		FilePath: "greet.go",
		NewStart: 2,
		NewEnd:   2,
		Lines:    []string{"TWO AGAIN"},
	}
	elsewhere := Suggestion{
		FilePath: "other.go",
		NewStart: 2,
		NewEnd:   2,
		Lines:    []string{"TWO"},
	}

	assert.Equal(t, first.Key(), same.Key())
	assert.NotEqual(t, first.Key(), rewritten.Key())
	assert.NotEqual(t, first.Key(), elsewhere.Key())
}

// TestGetRangeAnchorRefusesWhatTheDiffDoesNotDraw covers a suggestion made over lines the patch
// has no rows for, which has nowhere on screen for a comment to stand.
func TestGetRangeAnchorRefusesWhatTheDiffDoesNotDraw(t *testing.T) {
	view := NewView(patchWith(diff.File{
		OldPath: "greet.go",
		NewPath: "greet.go",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{
				Kind:      diff.Context,
				Text:      "package greet",
				OldNumber: 1,
				NewNumber: 1,
			},
			diff.Line{
				Kind:      diff.Addition,
				Text:      "added",
				NewNumber: 2,
			},
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
			Hunks: []diff.Hunk{hunkOf(diff.Line{
				Kind:      diff.Context,
				Text:      "package greet",
				OldNumber: 1,
				NewNumber: 1,
			})},
		},
		diff.File{
			OldPath: "logo.png",
			NewPath: "logo.png",
			Content: "aaa..bbb",
		},
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

// TestWithoutSuggestionPutsOneRunBack covers the file a comment leaves behind: the run it carried
// is the round's own again, every other run the reviewer wrote is untouched, and an insertion is
// matched by what it proposes rather than by the range git reports it on.
func TestWithoutSuggestionPutsOneRunBack(t *testing.T) {
	round := []string{"one", "two", "three", "four"}

	rewrite := git.Edit{
		Path:  "greet.go",
		Start: 2,
		End:   2,
		Lines: []string{"TWO"},
	}
	later := git.Edit{
		Path:  "greet.go",
		Start: 4,
		End:   4,
		Lines: []string{"FOUR"},
	}
	insert := git.Edit{
		Path:  "greet.go",
		Start: 3,
		End:   2,
		Lines: []string{"two and a half"},
	}
	elsewhere := git.Edit{
		Path:  "other.go",
		Start: 1,
		End:   1,
		Lines: []string{"not this file"},
	}

	sent, ok := Suggest(rewrite, round)
	require.True(t, ok)

	tests := []struct {
		name  string
		edits []git.Edit
		sent  Suggestion
		want  []string
	}{
		{
			name:  "the only run goes back",
			edits: []git.Edit{rewrite},
			sent:  sent,
			want:  round,
		},
		{
			name:  "a later run keeps the numbering it was measured in",
			edits: []git.Edit{rewrite, later},
			sent:  sent,
			want:  []string{"one", "two", "three", "FOUR"},
		},
		{
			name:  "an insertion beside it stands, widened onto the line before",
			edits: []git.Edit{rewrite, insert},
			sent:  sent,
			want:  []string{"one", "two", "two and a half", "three", "four"},
		},
		{
			name:  "another file is left alone",
			edits: []git.Edit{rewrite, elsewhere},
			sent:  sent,
			want:  round,
		},
		{
			name:  "an insertion is the run that goes back when it is the one that was carried",
			edits: []git.Edit{rewrite, insert},
			sent:  suggestionOf(t, insert, round),
			want:  []string{"one", "TWO", "three", "four"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, WithoutSuggestion(round, tt.edits, tt.sent))
		})
	}
}

func suggestionOf(t *testing.T, edit git.Edit, round []string) Suggestion {
	t.Helper()

	suggestion, ok := Suggest(edit, round)
	require.True(t, ok)

	return suggestion
}
