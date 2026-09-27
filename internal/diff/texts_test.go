package diff

import (
	"fmt"
	"strings"
	"testing"

	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
)

// TestFromTexts covers the hunks one text's change to another is drawn as.
func TestFromTexts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		before string
		after  string
		want   []Hunk
	}{
		{
			name:   "equal texts change nothing",
			before: "one\ntwo",
			after:  "one\ntwo\n",
			want:   []Hunk{},
		},
		{
			name:  "a first text is all added",
			after: "one\ntwo",
			want: []Hunk{{
				Header:   "@@ -0,0 +1,2 @@",
				NewStart: 1,
				Lines: []Line{
					{Kind: Addition, Text: "one", NewNumber: 1},
					{Kind: Addition, Text: "two", NewNumber: 2},
				},
			}},
		},
		{
			name:   "an emptied text is all removed",
			before: "one",
			want: []Hunk{{
				Header:   "@@ -1 +0,0 @@",
				NewStart: 1,
				Lines:    []Line{{Kind: Deletion, Text: "one", OldNumber: 1}},
			}},
		},
		{
			name:   "a replaced line is removed then added between the lines around it",
			before: "one\ntwo\nthree",
			after:  "one\n2\nthree",
			want: []Hunk{{
				Header:   "@@ -1,3 +1,3 @@",
				NewStart: 1,
				Lines: []Line{
					{Kind: Context, Text: "one", OldNumber: 1, NewNumber: 1},
					{Kind: Deletion, Text: "two", OldNumber: 2},
					{Kind: Addition, Text: "2", NewNumber: 2},
					{Kind: Context, Text: "three", OldNumber: 3, NewNumber: 3},
				},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file := FromTexts("document.md", tt.before, tt.after)

			assert.Equal(t, tt.want, file.Hunks)
			assert.Equal(t, "document.md", file.Path())
			assert.Equal(t, tt.before, file.OldSource)
			assert.Equal(t, tt.after, file.NewSource)
		})
	}
}

// TestFromTextsSplitsDistantChangesIntoHunks covers two changes too far apart to share a hunk.
func TestFromTextsSplitsDistantChangesIntoHunks(t *testing.T) {
	t.Parallel()

	lines := make([]string, 20)
	for i := range lines {
		lines[i] = fmt.Sprint("line ", i+1)
	}
	before := strings.Join(lines, "\n")
	lines[0], lines[19] = "first", "last"

	file := FromTexts("document.md", before, strings.Join(lines, "\n"))

	assert.Equal(t, []string{"@@ -1,4 +1,4 @@", "@@ -17,4 +17,4 @@"}, xslice.Map(file.Hunks, func(hunk Hunk) string {
		return hunk.Header
	}))
	assert.Equal(t, 17, file.Hunks[1].NewStart)
}
