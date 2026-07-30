package ui

import (
	"testing"

	"weave/internal/appconfig"

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

// TestStageWrapMeasuresThePane covers the width markdown is rendered to, which is the
// stage pane as tmux reports it and not the share of the width the panel asked for.
func TestStageWrapMeasuresThePane(t *testing.T) {
	tmux := &recordingTmux{width: 216}
	stage := NewStage(tmux, appconfig.DefaultSplit)
	stage.Init()

	assert.Equal(t, 216, stage.wrap())
}

// TestStageWrapUnmeasurablePane covers a pane tmux gives no width for, which leaves
// the renderer on its own default rather than rendering to nothing.
func TestStageWrapUnmeasurablePane(t *testing.T) {
	stage := NewStage(&recordingTmux{}, appconfig.DefaultSplit)
	stage.Init()

	assert.Equal(t, 0, stage.wrap())
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
