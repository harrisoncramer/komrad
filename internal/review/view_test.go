package review

import (
	"regexp"
	"testing"

	"weave/internal/diff"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// patchBaseSHA is the commit these patches are measured from, non-empty so that a thread sharing
// it says so rather than matching by two empty strings.
const patchBaseSHA = "0e1f2a3b4c5d6e7f80910a1b2c3d4e5f60718293"

func patchWith(files ...diff.File) *diff.Patch {
	return &diff.Patch{
		Root:    "/repo",
		BaseSHA: patchBaseSHA,
		Files:   files,
	}
}

func hunkOf(lines ...diff.Line) diff.Hunk {
	return diff.Hunk{Header: "@@ -1 +1 @@", Lines: lines}
}

// TestEditTargetIsWhereTheLineLivesNow covers a surviving line opening at its own number and a
// deleted one at the closest line that survived it.
func TestEditTargetIsWhereTheLineLivesNow(t *testing.T) {
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
				Kind:      diff.Deletion,
				Text:      "old",
				OldNumber: 2,
			},
			diff.Line{
				Kind:      diff.Addition,
				Text:      "new",
				NewNumber: 2,
			},
		)},
	}), false)

	tests := []struct {
		name string
		row  int
		want int
	}{
		{
			name: "a line both sides have opens at its own number",
			row:  2,
			want: 1,
		},
		{
			name: "a deleted line opens at the line above it",
			row:  3,
			want: 1,
		},
		{
			name: "an added line opens at its own number",
			row:  4,
			want: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, line, ok := view.GetEditTarget(tt.row)

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
			diff.Line{
				Kind:      diff.Deletion,
				Text:      "old",
				OldNumber: 1,
			},
			diff.Line{
				Kind:      diff.Context,
				Text:      "package greet",
				OldNumber: 2,
				NewNumber: 1,
			},
		)},
	}), false)

	_, line, ok := view.GetEditTarget(2)

	assert.True(t, ok)
	assert.Equal(t, 1, line)
}

// TestEditTargetRefusesADeletedFile covers the one case with nothing to open: the file the diff
// is showing is not on disk any more.
func TestEditTargetRefusesADeletedFile(t *testing.T) {
	view := NewView(patchWith(diff.File{
		OldPath: "gone.go",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{
				Kind:      diff.Deletion,
				Text:      "old",
				OldNumber: 1,
			},
		)},
	}), false)

	_, _, ok := view.GetEditTarget(2)

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
				diff.Line{
					Kind:      diff.Deletion,
					Text:      "old",
					OldNumber: 1,
				},
			)},
		},
		diff.File{
			OldPath: "other.go",
			NewPath: "other.go",
			Hunks: []diff.Hunk{hunkOf(
				diff.Line{
					Kind:      diff.Addition,
					Text:      "new",
					NewNumber: 9,
				},
			)},
		},
	), false)

	path, line, ok := view.GetEditTarget(2)

	assert.True(t, ok)
	assert.Equal(t, "emptied.go", path)
	assert.Equal(t, 1, line, "an emptied file opens at its top rather than at another file's line")
}

// twoFilePatch is two files with one changed line apiece, which is the smallest patch that can
// show a file folded away while the diff around it stays whole.
func twoFilePatch() *diff.Patch {
	file := func(path, text string) diff.File {
		return diff.File{
			OldPath: path,
			NewPath: path,
			Hunks: []diff.Hunk{hunkOf(
				diff.Line{
					Kind:      diff.Context,
					Text:      "package greet",
					OldNumber: 1,
					NewNumber: 1,
				},
				diff.Line{
					Kind:      diff.Addition,
					Text:      text,
					NewNumber: 2,
				},
			)},
		}
	}

	return patchWith(file("greet.go", "// greet"), file("wave.go", "// wave"))
}

// TestViewedFileCollapsesToItsHeader covers the fold itself: the file keeps its header and
// loses its lines, and the file beside it is untouched.
func TestViewedFileCollapsesToItsHeader(t *testing.T) {
	view := NewView(twoFilePatch(), false)
	require.Equal(t, 8, view.Len())

	view.SetFileViewed("greet.go", true)

	require.Equal(t, 5, view.Len())
	assert.True(t, view.IsViewed("greet.go"))

	read, total := view.CountViewedFiles()
	assert.Equal(t, 1, read)
	assert.Equal(t, 2, total)

	view.SetFileViewed("greet.go", false)
	assert.Equal(t, 8, view.Len())
	assert.False(t, view.IsViewed("greet.go"))
}

// TestCursorCanRestOnACollapsedHeader pins the one thing that makes the fold reversible: the
// header is the only row the file has left, so the cursor has to be able to stand on it.
func TestCursorCanRestOnACollapsedHeader(t *testing.T) {
	view := NewView(twoFilePatch(), false)
	view.SetFileViewed("greet.go", true)

	assert.Equal(t, 0, view.GetFirstRow(), "the fold leaves the header as the first row to rest on")

	file, ok := view.GetDrawnFileAt(0)
	require.True(t, ok)
	assert.Equal(t, "greet.go", file.Path())
}

// TestJumpFileLandsOnACollapsedHeader covers walking the files with one of them folded away,
// which would otherwise swallow the jump for want of a line to land on.
func TestJumpFileLandsOnACollapsedHeader(t *testing.T) {
	view := NewView(twoFilePatch(), false)
	view.SetFileViewed("wave.go", true)

	landed := view.JumpFile(view.GetFirstRow(), 1)

	file, ok := view.GetDrawnFileAt(landed)
	require.True(t, ok)
	assert.Equal(t, "wave.go", file.Path())
}

// TestFoldingKeepsTheCursorInTheFile covers the cursor standing in a file as it is folded, and
// then standing on the header as it is opened again.
func TestFoldingKeepsTheCursorInTheFile(t *testing.T) {
	view := NewView(twoFilePatch(), false)

	inside := view.Move(view.GetFirstRow(), 1)
	place, ok := view.GetPlace(inside)
	require.True(t, ok)

	view.SetFileViewed("greet.go", true)

	header, ok := view.Locate(place)
	require.True(t, ok)
	assert.Equal(t, 0, header, "the header is what is left of the file to come back to")

	place, ok = view.GetPlace(header)
	require.True(t, ok)

	view.SetFileViewed("greet.go", false)

	back, ok := view.Locate(place)
	require.True(t, ok)

	file, ok := view.GetDrawnFileAt(back)
	require.True(t, ok)
	assert.Equal(t, "greet.go", file.Path())
	assert.True(t, view.isPinnable(back), "opening the file again lands the cursor on a line of it")
}

// TestHeaderFoundByASearchIsHeldAcrossARebuild covers the one row only a search reaches: the
// header of a file drawn whole, which the reader is left standing on and which has no line
// under it for the place to be read off.
func TestHeaderFoundByASearchIsHeldAcrossARebuild(t *testing.T) {
	view := NewView(twoFilePatch(), false)

	require.Equal(t, 1, view.SetPattern(regexp.MustCompile("wave.go")))
	header, ok := view.JumpMatch(-1, 1)
	require.True(t, ok)

	place, ok := view.GetPlace(header)
	require.True(t, ok)
	require.True(t, place.Header)

	view.PinThreads(nil)

	back, ok := view.Locate(place)
	require.True(t, ok)
	assert.Equal(t, header, back, "the header the search found is where the reader is put back")
}

// TestHeaderFoundByASearchSurvivesTheFold covers the same place read again once the file has
// been folded away under it, where the header is the only row the file has left.
func TestHeaderFoundByASearchSurvivesTheFold(t *testing.T) {
	view := NewView(twoFilePatch(), false)

	place, ok := view.GetPlace(0)
	require.True(t, ok)
	require.True(t, place.Header)

	view.SetFileViewed("greet.go", true)

	back, ok := view.Locate(place)
	require.True(t, ok)
	assert.Equal(t, 0, back)
}

// TestHeaderOfAFileWithNoLinesIsHeld covers a rename or a mode change, which is drawn as a
// header with nothing under it, so there is no line for the place to fall back to.
func TestHeaderOfAFileWithNoLinesIsHeld(t *testing.T) {
	view := NewView(patchWith(
		diff.File{
			OldPath:  "old.go",
			NewPath:  "new.go",
			Metadata: []string{"renamed"},
		},
		diff.File{
			OldPath: "greet.go",
			NewPath: "greet.go",
			Hunks: []diff.Hunk{hunkOf(diff.Line{
				Kind:      diff.Context,
				Text:      "package greet",
				OldNumber: 1,
				NewNumber: 1,
			})},
		},
	), false)

	place, ok := view.GetPlace(0)
	require.True(t, ok)

	back, ok := view.Locate(place)
	require.True(t, ok)
	assert.Equal(t, 0, back)
}

// TestThreadInACollapsedFileIsNotAdrift pins that folding a file away does not send the
// conversations in it to the section for threads with nothing left to stand on.
func TestThreadInACollapsedFileIsNotAdrift(t *testing.T) {
	view := NewView(twoFilePatch(), false)

	thread := threadAt("t1", "greet.go", 0, 2, "+// greet")
	view.PinThreads([]*agentv1.ReviewThread{thread})
	require.Equal(t, 8, view.Len())

	view.SetFileViewed("greet.go", true)

	assert.Equal(t, 5, view.Len(), "no row is added at the foot for the folded thread")
	assert.Equal(t, collapsed, view.placed[thread.GetId()])
}

// TestHiddenMatchesCountsWhatTheFoldHides covers searching with a file folded away, where the
// matches inside it are on no row and stepping through the search never reaches them.
func TestHiddenMatchesCountsWhatTheFoldHides(t *testing.T) {
	view := NewView(twoFilePatch(), false)
	view.SetFileViewed("greet.go", true)

	found := view.SetPattern(regexp.MustCompile("package"))

	assert.Equal(t, 1, found, "only the file still drawn has rows to match")
	assert.Equal(t, 1, view.HiddenMatches())
}
