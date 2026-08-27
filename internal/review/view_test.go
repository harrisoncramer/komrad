package review

import (
	"testing"

	"weave/internal/diff"

	"github.com/stretchr/testify/assert"
)

func patchWith(files ...diff.File) *diff.Patch {
	return &diff.Patch{Root: "/repo", Files: files}
}

func hunkOf(lines ...diff.Line) diff.Hunk {
	return diff.Hunk{Header: "@@ -1 +1 @@", Lines: lines}
}

// TestEditTargetIsWhereTheLineLivesNow covers what the editor key opens. A line the file still
// has opens at its own number, and a deleted one at the closest line that survived it, since
// there is nothing at its old number any more.
func TestEditTargetIsWhereTheLineLivesNow(t *testing.T) {
	view := NewView(patchWith(diff.File{
		OldPath: "greet.go",
		NewPath: "greet.go",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{Kind: diff.Context, Text: "package greet", OldNumber: 1, NewNumber: 1},
			diff.Line{Kind: diff.Deletion, Text: "old", OldNumber: 2},
			diff.Line{Kind: diff.Addition, Text: "new", NewNumber: 2},
		)},
	}), false)

	tests := []struct {
		name string
		row  int
		want int
	}{
		{name: "a line both sides have opens at its own number", row: 2, want: 1},
		{name: "a deleted line opens at the line above it", row: 3, want: 1},
		{name: "an added line opens at its own number", row: 4, want: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, line, ok := view.EditTarget(tt.row)

			assert.True(t, ok)
			assert.Equal(t, "greet.go", path)
			assert.Equal(t, tt.want, line)
		})
	}
}

// TestEditTargetLooksForwardWhenNothingCameBefore covers a file opening with deletions: there is
// no surviving line above them, so the nearest one is under them.
func TestEditTargetLooksForwardWhenNothingCameBefore(t *testing.T) {
	view := NewView(patchWith(diff.File{
		OldPath: "greet.go",
		NewPath: "greet.go",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{Kind: diff.Deletion, Text: "old", OldNumber: 1},
			diff.Line{Kind: diff.Context, Text: "package greet", OldNumber: 2, NewNumber: 1},
		)},
	}), false)

	_, line, ok := view.EditTarget(2)

	assert.True(t, ok)
	assert.Equal(t, 1, line)
}

// TestEditTargetRefusesADeletedFile covers the one case with nothing to open: the file the diff
// is showing is not on disk any more.
func TestEditTargetRefusesADeletedFile(t *testing.T) {
	view := NewView(patchWith(diff.File{
		OldPath: "gone.go",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{Kind: diff.Deletion, Text: "old", OldNumber: 1},
		)},
	}), false)

	_, _, ok := view.EditTarget(2)

	assert.False(t, ok)
}

// TestEditTargetStopsAtTheFileItIsIn covers a diff of several files: a file whose every line is
// gone never borrows a line number from the file drawn after it.
func TestEditTargetStopsAtTheFileItIsIn(t *testing.T) {
	view := NewView(patchWith(
		diff.File{
			OldPath: "emptied.go",
			NewPath: "emptied.go",
			Hunks: []diff.Hunk{hunkOf(
				diff.Line{Kind: diff.Deletion, Text: "old", OldNumber: 1},
			)},
		},
		diff.File{
			OldPath: "other.go",
			NewPath: "other.go",
			Hunks: []diff.Hunk{hunkOf(
				diff.Line{Kind: diff.Addition, Text: "new", NewNumber: 9},
			)},
		},
	), false)

	path, line, ok := view.EditTarget(2)

	assert.True(t, ok)
	assert.Equal(t, "emptied.go", path)
	assert.Equal(t, 1, line, "an emptied file opens at its top rather than at another file's line")
}
