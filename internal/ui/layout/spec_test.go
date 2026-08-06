package layout

import (
	"testing"

	"weave/config"

	"github.com/stretchr/testify/assert"
)

// TestSpecMainPercent covers the width the main region is given. The configured split is the
// nav region's share, so what tmux is told is the remainder of it.
func TestSpecMainPercent(t *testing.T) {
	tests := []struct {
		name  string
		split float64
		want  int
	}{
		{
			name:  "a spec built with no split at all falls back to the default",
			split: 0,
			want:  80,
		},
		{
			name:  "so does one no window could be divided at",
			split: 1.5,
			want:  80,
		},
		{
			name:  "a share that does not land on a whole percent rounds",
			split: 0.335,
			want:  67,
		},
		{
			name:  "the default leaves the stage the greater part",
			split: config.DefaultSplit,
			want:  80,
		},
		{
			name:  "an even split",
			split: 0.5,
			want:  50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, NewSpec(config.Layout{Split: tt.split}).getMainPercent())
		})
	}
}

// TestSpecBottomPercent covers the height the bottom region opens at, which is the share of the
// main region the config asks for as it stands.
func TestSpecBottomPercent(t *testing.T) {
	tests := []struct {
		name  string
		split float64
		want  int
	}{
		{
			name:  "an omitted share is the default",
			split: 0,
			want:  40,
		},
		{
			name:  "a configured share is taken as it is",
			split: 0.6,
			want:  60,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, NewSpec(config.Layout{SecondarySplit: tt.split}).getBottomPercent())
		})
	}
}
