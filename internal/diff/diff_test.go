package diff

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestStatCountsFilesAndLines pins the summary a patch reduces to, which is the files touched
// and the lines added and removed.
func TestStatCountsFilesAndLines(t *testing.T) {
	patch := Patch{Files: []File{
		{
			NewPath: "one.go",
			Hunks: []Hunk{{Lines: []Line{
				{Kind: Context},
				{Kind: Addition},
				{Kind: Addition},
				{Kind: Deletion},
			}}},
		},
		{
			NewPath: "two.go",
			Hunks: []Hunk{
				{Lines: []Line{{Kind: Addition}}},
				{Lines: []Line{{Kind: Deletion}, {Kind: Context}}},
			},
		},
	}}

	assert.Equal(t, Stat{
		Files:   2,
		Added:   3,
		Removed: 2,
	}, patch.Stat())
}

// TestStatOfAnEmptyPatchIsZero pins that a patch holding nothing summarises as zero rather
// than as one empty file.
func TestStatOfAnEmptyPatchIsZero(t *testing.T) {
	assert.Equal(t, Stat{}, Patch{}.Stat())
}

// TestFingerprintFollowsTheDiffRatherThanTheFile covers what a mark of having read a file is
// held against: the lines the diff draws, not the source either side of them.
func TestFingerprintFollowsTheDiffRatherThanTheFile(t *testing.T) {
	file := File{
		OldPath: "greet.go",
		NewPath: "greet.go",
		Hunks: []Hunk{{
			Header: "@@ -1,1 +1,2 @@",
			Lines: []Line{
				{
					Kind:      Context,
					Text:      "package greet",
					OldNumber: 1,
					NewNumber: 1,
				},
				{
					Kind:      Addition,
					Text:      "// greet",
					NewNumber: 2,
				},
			},
		}},
	}

	original := file.Fingerprint()

	sourceOnly := file
	sourceOnly.NewSource = "package greet\n// greet\n"
	assert.Equal(t, original, sourceOnly.Fingerprint(), "source the hunks do not show was never read")

	rewritten := file
	rewritten.Hunks = []Hunk{{
		Header: file.Hunks[0].Header,
		Lines: []Line{
			file.Hunks[0].Lines[0],
			{
				Kind:      Addition,
				Text:      "// wave",
				NewNumber: 2,
			},
		},
	}}
	assert.NotEqual(t, original, rewritten.Fingerprint(), "a line the agent rewrote drops the mark")

	moved := file
	moved.Hunks = []Hunk{{Header: "@@ -8,1 +8,2 @@", Lines: file.Hunks[0].Lines}}
	assert.NotEqual(t, original, moved.Fingerprint(), "the base moving under the file drops the mark")

	renamed := file
	renamed.Metadata = []string{"renamed from hello.go"}
	assert.NotEqual(t, original, renamed.Fingerprint(), "what the change did to the file is part of it")
}

// TestFingerprintStandsOnContentWhereThereAreNoHunks is the binary case: nothing else about the
// file moves when an agent replaces it, so a mark held without the content never comes off.
func TestFingerprintStandsOnContentWhereThereAreNoHunks(t *testing.T) {
	file := File{
		OldPath:  "logo.png",
		NewPath:  "logo.png",
		Metadata: []string{"binary file"},
		Content:  "994211c..952a679",
	}

	replaced := file
	replaced.Content = "994211c..1f0a3bd"
	assert.NotEqual(t, file.Fingerprint(), replaced.Fingerprint(), "the binary the agent replaced reads as unread")
}

// TestPatchFingerprintFollowsWhatTheDiffDraws covers the whole-patch hash the review screen
// compares a background read against: it moves when the drawn change moves and holds when only
// what the patch never showed has.
func TestPatchFingerprintFollowsWhatTheDiffDraws(t *testing.T) {
	greet := func(comment string) File {
		return File{
			OldPath: "greet.go",
			NewPath: "greet.go",
			Hunks: []Hunk{{
				Header: "@@ -1,1 +1,2 @@",
				Lines: []Line{
					{
						Kind:      Context,
						Text:      "package greet",
						OldNumber: 1,
						NewNumber: 1,
					},
					{
						Kind:      Addition,
						Text:      comment,
						NewNumber: 2,
					},
				},
			}},
		}
	}

	patch := Patch{
		Root:        "/tmp/one",
		BaseSHA:     "abc",
		SnapshotSHA: "def",
		Files:       []File{greet("// greet")},
	}
	original := patch.Fingerprint()

	elsewhere := Patch{
		Root:        "/tmp/two",
		BaseSHA:     "ghi",
		SnapshotSHA: "jkl",
		Files:       []File{greet("// greet")},
	}
	assert.Equal(t, original, elsewhere.Fingerprint(), "the same change read from somewhere else is the same diff")

	rewritten := Patch{Files: []File{greet("// hello")}}
	assert.NotEqual(t, original, rewritten.Fingerprint(), "a line the agent rewrote is a diff that moved")

	added := Patch{Files: []File{greet("// greet"), {NewPath: "next.go"}}}
	assert.NotEqual(t, original, added.Fingerprint(), "and so is a file the agent added")

	assert.NotEqual(t, Patch{}.Fingerprint(), original, "an empty patch is not the one on screen")
}
