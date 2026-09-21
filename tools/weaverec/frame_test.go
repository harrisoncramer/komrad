package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestStitch covers how windows are stacked, padded and cut to the frame.
func TestStitch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		params stitchParams
		want   []string
	}{
		{
			name: "a label wider than the frame is cut and the name goes",
			params: stitchParams{
				label: "key Escape",
				cols:  4,
				rows:  0,
			},
			want: []string{
				"\x1b[7mkey \x1b[0m",
			},
		},
		{
			name: "a short capture is padded and a long one cut",
			params: stitchParams{
				lines: []string{"\x1b[31mred", "two", "three"},
				label: "key j",
				cols:  14,
				rows:  2,
			},
			want: []string{
				"\x1b[7m panel  key j \x1b[0m",
				"\x1b[31mred\x1b[0m",
				"two\x1b[0m",
			},
		},
		{
			name: "no capture is a blank window",
			params: stitchParams{
				cols: 8,
				rows: 1,
			},
			want: []string{
				"\x1b[7m panel  \x1b[0m",
				"\x1b[0m",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := stitch(tt.params)
			assert.Equal(t, tt.want, got.lines)
		})
	}
}

// TestFrameEquals covers the comparison the cast dedups on.
func TestFrameEquals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  frame
		right frame
		want  bool
	}{
		{
			name:  "different length",
			left:  frame{lines: []string{"a"}},
			right: frame{lines: []string{"a", "b"}},
			want:  false,
		},
		{
			name:  "different line",
			left:  frame{lines: []string{"a", "b"}},
			right: frame{lines: []string{"a", "c"}},
			want:  false,
		},
		{
			name:  "same lines",
			left:  frame{lines: []string{"a", "b"}},
			right: frame{lines: []string{"a", "b"}},
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.left.equals(tt.right))
		})
	}
}

// TestFrameScreen covers the clear and redraw a frame becomes in the cast.
func TestFrameScreen(t *testing.T) {
	t.Parallel()

	got := frame{lines: []string{"a", "b"}}.screen()
	assert.Equal(t, "\x1b[2J\x1b[Ha\r\nb", got)
}
