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

// TestFingerprintIsDeterministic covers the same file hashing to the same string twice.
func TestFingerprintIsDeterministic(t *testing.T) {
	file := File{
		NewPath:  "greet.go",
		Metadata: []string{"new file mode 100644"},
		Hunks: []Hunk{{
			Header: "@@ -0,0 +1,3 @@",
			Lines: []Line{
				{Kind: Addition, Text: "package main", NewNumber: 1},
				{Kind: Addition, Text: "", NewNumber: 2},
				{Kind: Addition, Text: `func greet() { println("hi") }`, NewNumber: 3},
			},
		}},
	}

	first := file.Fingerprint()
	second := file.Fingerprint()
	assert.Equal(t, first, second)
	assert.Len(t, first, 64, "hex-encoded SHA-256")
}

// TestFingerprintChangesWhenHunksDiffer covers a changed hunk line moving the hash.
func TestFingerprintChangesWhenHunksDiffer(t *testing.T) {
	base := File{
		NewPath: "greet.go",
		Hunks: []Hunk{{
			Header: "@@ -1,1 +1,1 @@",
			Lines:  []Line{{Kind: Deletion, Text: "old"}, {Kind: Addition, Text: "new"}},
		}},
	}

	changed := File{
		NewPath: "greet.go",
		Hunks: []Hunk{{
			Header: "@@ -1,1 +1,1 @@",
			Lines:  []Line{{Kind: Deletion, Text: "old"}, {Kind: Addition, Text: "newer"}},
		}},
	}

	assert.NotEqual(t, base.Fingerprint(), changed.Fingerprint())
}

// TestFingerprintChangesWhenMetadataDiffers covers a changed file mode moving the hash.
func TestFingerprintChangesWhenMetadataDiffers(t *testing.T) {
	a := File{
		NewPath:  "greet.go",
		Metadata: []string{"new file mode 100644"},
		Hunks:    []Hunk{{Header: "@@ -0,0 +1 @@", Lines: []Line{{Kind: Addition, Text: "x"}}}},
	}

	b := File{
		NewPath:  "greet.go",
		Metadata: []string{"new file mode 100755"},
		Hunks:    []Hunk{{Header: "@@ -0,0 +1 @@", Lines: []Line{{Kind: Addition, Text: "x"}}}},
	}

	assert.NotEqual(t, a.Fingerprint(), b.Fingerprint())
}

// TestFingerprintIgnoresPathAndSource covers a renamed file hashing to the same string.
func TestFingerprintIgnoresPathAndSource(t *testing.T) {
	hunks := []Hunk{{Header: "@@ -1 +1 @@", Lines: []Line{{Kind: Context, Text: "x"}}}}

	a := File{OldPath: "a.go", NewPath: "b.go", OldSource: "old", NewSource: "new", Hunks: hunks}
	b := File{OldPath: "c.go", NewPath: "d.go", OldSource: "different", NewSource: "also different", Hunks: hunks}

	assert.Equal(t, a.Fingerprint(), b.Fingerprint(), "path and source are not part of the fingerprint")
}

// TestFingerprintOfBinaryUsesContent covers a binary file, which has no hunks to hash.
func TestFingerprintOfBinaryUsesContent(t *testing.T) {
	a := File{NewPath: "icon.png", Content: "Binary file"}
	b := File{NewPath: "icon.png", Content: "Binary file (differs)"}

	assert.NotEqual(t, a.Fingerprint(), b.Fingerprint())
}
