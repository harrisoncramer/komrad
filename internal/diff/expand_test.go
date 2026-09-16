package diff

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wholeSource is a ten line file, each line naming its own number so a wrongly numbered gap line
// is visible in the failure rather than merely unequal.
const wholeSource = "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n"

// changedFile is a file whose fifth line was rewritten, drawn with no context at all so that
// every line the expansion adds is one the test asked for.
func changedFile() File {
	return File{
		OldPath:   "greet.go",
		NewPath:   "greet.go",
		NewSource: wholeSource,
		Hunks: []Hunk{{
			Header:   "@@ -5,1 +5,1 @@",
			OldStart: 5,
			NewStart: 5,
			Lines: []Line{
				{
					Kind:      Deletion,
					Text:      "était",
					OldNumber: 5,
				},
				{
					Kind:      Addition,
					Text:      "five",
					NewNumber: 5,
				},
			},
		}},
	}
}

// TestExpandRefusesWhatItCannotDraw covers the files with no text to fill a gap from: expanding
// one of those half way would draw blank lines where the file's own text should be.
func TestExpandRefusesWhatItCannotDraw(t *testing.T) {
	tests := []struct {
		name string
		file func(File) File
	}{
		{
			name: "a deleted file has no new side to draw",
			file: func(file File) File {
				file.NewPath = ""

				return file
			},
		},
		{
			name: "a binary file names its blobs instead of carrying text",
			file: func(file File) File {
				file.Content = "0a90f17..23cef25"

				return file
			},
		},
		{
			name: "a file whose source the loader left out",
			file: func(file File) File {
				file.NewSource = ""

				return file
			},
		},
		{
			name: "a file the patch draws no hunks of",
			file: func(file File) File {
				file.Hunks = nil

				return file
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := Expand(tt.file(changedFile()))
			assert.False(t, ok)
		})
	}
}

// TestExpandRefusesAFileAlreadyDrawnWhole covers a new file, every line of which is already an
// addition, so there is no gap to fill and nothing for the key to do.
func TestExpandRefusesAFileAlreadyDrawnWhole(t *testing.T) {
	file := File{
		NewPath:   "greet.go",
		NewSource: "one\ntwo\n",
		Hunks: []Hunk{{
			OldStart: 0,
			NewStart: 1,
			Lines: []Line{
				{
					Kind:      Addition,
					Text:      "one",
					NewNumber: 1,
				},
				{
					Kind:      Addition,
					Text:      "two",
					NewNumber: 2,
				},
			},
		}},
	}

	_, ok := Expand(file)
	assert.False(t, ok)
}

// TestExpandFillsBothEnds covers the runs above the first hunk and below the last, which is
// where most of a file is when the change is one line in the middle of it.
func TestExpandFillsBothEnds(t *testing.T) {
	expanded, ok := Expand(changedFile())
	require.True(t, ok)
	require.Len(t, expanded.Hunks, 1, "an expanded file is one run of lines")

	lines := expanded.Hunks[0].Lines
	require.Len(t, lines, 11, "ten lines of the new side, with the line it replaced among them")

	assert.Equal(t, "one", lines[0].Text)
	assert.Equal(t, 1, lines[0].OldNumber)
	assert.Equal(t, 1, lines[0].NewNumber)
	assert.Equal(t, Context, lines[0].Kind)

	assert.Equal(t, Deletion, lines[4].Kind, "the hunk's own lines are kept where they were")
	assert.Equal(t, Addition, lines[5].Kind)

	assert.Equal(t, "ten", lines[10].Text)
	assert.Equal(t, 10, lines[10].OldNumber)
	assert.Equal(t, 10, lines[10].NewNumber)
}

// TestExpandNumbersAGapBySidesThatHaveDiverged covers the run between two hunks after one of
// them has added lines: the two sides are no longer numbered alike there, and a gap line that
// takes its old number from its new one would anchor a comment to the wrong line of the old file.
func TestExpandNumbersAGapBySidesThatHaveDiverged(t *testing.T) {
	file := File{
		OldPath:   "greet.go",
		NewPath:   "greet.go",
		NewSource: wholeSource,
		Hunks: []Hunk{
			{
				OldStart: 2,
				NewStart: 2,
				Lines: []Line{
					{
						Kind:      Addition,
						Text:      "two",
						NewNumber: 2,
					},
					{
						Kind:      Addition,
						Text:      "three",
						NewNumber: 3,
					},
				},
			},
			{
				OldStart: 6,
				NewStart: 8,
				Lines: []Line{{
					Kind:      Addition,
					Text:      "eight",
					NewNumber: 8,
				}},
			},
		},
	}

	expanded, ok := Expand(file)
	require.True(t, ok)

	between := map[int]Line{}
	for _, line := range expanded.Hunks[0].Lines {
		if line.Kind == Context {
			between[line.NewNumber] = line
		}
	}

	require.Contains(t, between, 5)
	assert.Equal(t, 3, between[5].OldNumber, "two lines were added above it, so the old side is two behind")
	assert.Equal(t, "five", between[5].Text)

	require.Contains(t, between, 1)
	assert.Equal(t, 1, between[1].OldNumber, "above the first hunk the sides have not yet diverged")
}

// TestExpandDrawsEveryLineOfTheNewSide covers the whole point of the key: after it, every line
// of the file as it now stands is on the screen, in order.
func TestExpandDrawsEveryLineOfTheNewSide(t *testing.T) {
	expanded, ok := Expand(changedFile())
	require.True(t, ok)

	var drawn []string
	for _, line := range expanded.Hunks[0].Lines {
		if line.Kind != Deletion {
			drawn = append(drawn, line.Text)
		}
	}

	assert.Equal(t, strings.Split(strings.TrimSuffix(wholeSource, "\n"), "\n"), drawn)
}
