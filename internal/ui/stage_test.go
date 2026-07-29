package ui

import (
	"testing"

	"claude-agents/internal/appconfig"

	"github.com/stretchr/testify/assert"
)

// TestStagePercent covers the width the stage pane is given. The configured split
// is the list's share, so what tmux is told is the remainder of it.
func TestStagePercent(t *testing.T) {
	tests := []struct {
		name  string
		split float64
		want  int
	}{
		{
			name:  "the default leaves the preview the greater part",
			split: appconfig.DefaultSplit,
			want:  80,
		},
		{
			name:  "an even split",
			split: 0.5,
			want:  50,
		},
		{
			name:  "a share that does not land on a whole percent rounds",
			split: 0.335,
			want:  67,
		},
		{
			name:  "a stage built with no split at all falls back to the default",
			split: 0,
			want:  80,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, NewStage(&recordingTmux{}, tt.split).percent())
		})
	}
}

// TestStageInitSizesThePane covers a stage pane that outlived the panel. It is not
// split again, so the resize is the only thing that puts it at the configured
// split.
func TestStageInitSizesThePane(t *testing.T) {
	tmux := &recordingTmux{}
	NewStage(tmux, 0.4).Init()

	assert.Equal(t, []int{60}, tmux.resizes)
}
