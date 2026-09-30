package loader_test

import (
	"testing"

	"komrad/internal/loader"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFrame covers the frame each style draws at a step.
func TestFrame(t *testing.T) {
	tests := []struct {
		name  string
		style loader.Style
		step  int
		want  string
	}{
		{name: "braille starts on its first frame", style: loader.Braille, step: 0, want: "⠋"},
		{name: "braille wraps after its last frame", style: loader.Braille, step: 10, want: "⠋"},
		{name: "arcs turn a quarter a step", style: loader.Arcs, step: 1, want: "◝"},
		{name: "ascii wraps after four steps", style: loader.ASCII, step: 7, want: `\`},
		{name: "a negative step still lands on a frame", style: loader.ASCII, step: -1, want: `\`},
		{name: "an unknown style draws braille", style: loader.Style("dots"), step: 1, want: "⠙"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.style.Frame(tt.step))
		})
	}
}

// TestEveryFrameIsOneColumn covers every frame fitting the one column a glyph takes.
func TestEveryFrameIsOneColumn(t *testing.T) {
	for _, style := range loader.Styles() {
		for _, frame := range style.Frames() {
			assert.Equal(t, 1, ansi.StringWidth(frame), "%s draws %q", style, frame)
		}
	}
}

// TestParse covers the styles a config may name.
func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    loader.Style
		wantErr string
	}{
		{name: "braille", in: "braille", want: loader.Braille},
		{name: "arcs", in: "arcs", want: loader.Arcs},
		{name: "ascii with spaces around it", in: " ascii ", want: loader.ASCII},
		{name: "a style the panel cannot draw", in: "dots", wantErr: `"dots" is not one of braille, arcs, ascii`},
		{name: "nothing at all", in: "", wantErr: `"" is not one of braille, arcs, ascii`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := loader.Parse(tt.in)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
