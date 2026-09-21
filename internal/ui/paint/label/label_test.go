package label

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBytes covers the unit and the precision at each size, and the roll into the next unit
// where the rounded value would run to five characters.
func TestBytes(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{bytes: 0, want: "0B"},
		{bytes: 512, want: "512B"},
		{bytes: 999, want: "999B"},
		{bytes: 1000, want: "1.0K"},
		{bytes: 1023, want: "1.0K"},
		{bytes: 10240, want: "10K"},
		{bytes: 848 * 1024, want: "848K"},
		{bytes: 9*1024*1024 + 200*1024, want: "9.2M"},
		{bytes: 1048063, want: "1.0M"},
		{bytes: 49 * 1024 * 1024, want: "49M"},
		{bytes: 1536 * 1024 * 1024, want: "1.5G"},
		{bytes: 24 * 1024 * 1024 * 1024, want: "24G"},
		{bytes: 1024*1024*1024*1024 - 1, want: "1.0T"},
		{bytes: 3 * 1024 * 1024 * 1024 * 1024, want: "3.0T"},
		{bytes: 2048 * 1024 * 1024 * 1024 * 1024, want: "2048T"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := Bytes(tt.bytes)

			assert.Equal(t, tt.want, got)
			if tt.bytes < 1000*1024*1024*1024*1024 {
				assert.LessOrEqual(t, len(got), 4)
			}
		})
	}
}

// TestCount covers the noun following the number, one alone taking the singular.
func TestCount(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{n: 0, want: "0 worktrees"},
		{n: 1, want: "1 worktree"},
		{n: 2, want: "2 worktrees"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, Count(tt.n, "worktree", "worktrees"))
		})
	}
}

// TestCounted covers the plural taking s or es by the word's ending, and one taking neither.
func TestCounted(t *testing.T) {
	tests := []struct {
		n    int
		word string
		want string
	}{
		{
			n:    1,
			word: "match",
			want: "1 match",
		},
		{
			n:    0,
			word: "file",
			want: "0 files",
		},
		{
			n:    2,
			word: "match",
			want: "2 matches",
		},
		{
			n:    3,
			word: "box",
			want: "3 boxes",
		},
		{
			n:    4,
			word: "comment",
			want: "4 comments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, Counted(tt.n, tt.word))
		})
	}
}

// TestOfCount covers where one stands in a run of things.
func TestOfCount(t *testing.T) {
	assert.Equal(t, "3 of 7", OfCount(3, 7))
}

// TestOrDash covers blank and whitespace reading as a dash and anything else as itself.
func TestOrDash(t *testing.T) {
	tests := []struct {
		text string
		want string
	}{
		{text: "", want: Dash},
		{text: "  ", want: Dash},
		{text: "main", want: "main"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, OrDash(tt.text))
		})
	}
}

// TestTally covers how many more sit behind the one shown.
func TestTally(t *testing.T) {
	assert.Equal(t, "+4", Tally(4))
}
