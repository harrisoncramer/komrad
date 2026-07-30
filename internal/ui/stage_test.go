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

// TestStageWrap covers the width markdown is rendered to, which the stage works out
// from the list beside it rather than measuring a pane it does not draw into.
func TestStageWrap(t *testing.T) {
	tests := []struct {
		name      string
		split     float64
		listWidth int
		want      int
	}{
		{
			name:      "a stage nothing has measured yet has no width of its own",
			split:     appconfig.DefaultSplit,
			listWidth: 0,
			want:      0,
		},
		{
			name:      "the default split leaves the stage four times the list",
			split:     appconfig.DefaultSplit,
			listWidth: 40,
			want:      159,
		},
		{
			name:      "an even split matches the list, less the divider",
			split:     0.5,
			listWidth: 100,
			want:      99,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stage := NewStage(&recordingTmux{}, tt.split)
			stage.SetListWidth(tt.listWidth)

			assert.Equal(t, tt.want, stage.wrap())
		})
	}
}

// TestPagerArgv covers the command the stage pane runs, which carries the line to
// open on for a search preview and nothing for a record read from the top.
func TestPagerArgv(t *testing.T) {
	tests := []struct {
		name string
		line int
		want []string
	}{
		{
			name: "no line opens at the top",
			line: 0,
			want: []string{"less", "-R", "-X", "/tmp/stage"},
		},
		{
			name: "the first line is the top already",
			line: 1,
			want: []string{"less", "-R", "-X", "/tmp/stage"},
		},
		{
			name: "a line further down is where the pager opens",
			line: 42,
			want: []string{"less", "-R", "-X", "+42", "/tmp/stage"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, pagerArgv("/tmp/stage", tt.line))
		})
	}
}

// TestHighlighted covers the mark written into a transcript preview, which is what
// makes the matched line findable in the conversation around it.
func TestHighlighted(t *testing.T) {
	content := "first\nsecond\nthird"

	tests := []struct {
		name string
		line int
		want string
	}{
		{
			name: "no matched line leaves the transcript alone",
			line: 0,
			want: content,
		},
		{
			name: "a line past the end of the transcript leaves it alone",
			line: 4,
			want: content,
		},
		{
			name: "the matched line is marked and its neighbours are not",
			line: 2,
			want: "first\n\x1b[7msecond\x1b[m\nthird",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, highlighted(content, tt.line))
		})
	}
}
