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

	assert.Equal(t, Stat{Files: 2, Added: 3, Removed: 2}, patch.Stat())
}

// TestStatOfAnEmptyPatchIsZero pins that a patch holding nothing summarises as zero rather
// than as one empty file.
func TestStatOfAnEmptyPatchIsZero(t *testing.T) {
	assert.Equal(t, Stat{}, Patch{}.Stat())
}
