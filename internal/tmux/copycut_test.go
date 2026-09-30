package tmux

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCutSelection pins which of a stand-in's cells a selection copies, whichever way it was made.
func TestCutSelection(t *testing.T) {
	cut := standInCut{
		Rows:   []string{"head", "list │ one ", "list │ two", "list │ 三x", "foot"},
		Top:    1,
		Left:   7,
		Width:  3,
		Height: 3,
	}

	tests := []struct {
		name string
		sel  Selection
		want string
	}{
		{
			name: "whole lines keep only the block's cells",
			sel:  Selection{StartX: 0, StartY: 0, EndX: 9, EndY: 4},
			want: "one\ntwo\n三x",
		},
		{
			name: "a selection made backwards cuts the same",
			sel:  Selection{StartX: 9, StartY: 4, EndX: 0, EndY: 0},
			want: "one\ntwo\n三x",
		},
		{
			name: "the first and last rows start and end where the selection does",
			sel:  Selection{StartX: 8, StartY: 1, EndX: 7, EndY: 2},
			want: "ne\nt",
		},
		{
			name: "a rectangle keeps its columns on every row",
			sel:  Selection{StartX: 8, StartY: 1, EndX: 8, EndY: 3, Rectangle: true},
			want: "n\nw\n",
		},
		{
			name: "a wide character is kept whole from its first cell",
			sel:  Selection{StartX: 7, StartY: 3, EndX: 7, EndY: 3},
			want: "三",
		},
		{
			name: "a selection outside the block copies nothing",
			sel:  Selection{StartX: 0, StartY: 1, EndX: 5, EndY: 1},
			want: "",
		},
		{
			name: "rows above and below the block are dropped",
			sel:  Selection{StartX: 0, StartY: 0, EndX: 3, EndY: 0},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, cutSelection(cut, tt.sel))
		})
	}
}
