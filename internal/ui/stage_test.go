package ui

import (
	"testing"

	"weave/internal/appconfig"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestStageInitAnchorsToTheGivenPane covers which pane the stage is laid out beside. It is
// the one the panel was told it runs in, never one resolved from tmux, which answers for
// whatever window the session is currently looking at. A panel restarted while the session
// sits on an agent would otherwise split its stage into that agent's window.
func TestStageInitAnchorsToTheGivenPane(t *testing.T) {
	tmux := &recordingTmux{}
	stage := NewStage(tmux, appconfig.DefaultSplit)

	stage.Init("%7")

	assert.Equal(t, []string{"%7"}, tmux.anchors)
	assert.Equal(t, "%7", stage.NavPane())
}

// TestStageInitSizesThePane covers a stage pane that outlived the panel. It is not
// split again, so the resize is the only thing that puts it at the configured
// split.
func TestStageInitSizesThePane(t *testing.T) {
	tmux := &recordingTmux{}
	NewStage(tmux, 0.4).Init("%0")

	assert.Equal(t, []int{60}, tmux.resizes)
}

// TestStageWrapMeasuresThePane covers the width markdown is rendered to, which is the
// stage pane as tmux reports it and not the share of the width the panel asked for.
func TestStageWrapMeasuresThePane(t *testing.T) {
	tmux := &recordingTmux{width: 216}
	stage := NewStage(tmux, appconfig.DefaultSplit)
	stage.Init("%0")

	assert.Equal(t, 216, stage.wrap())
}

// TestStageWrapUnmeasurablePane covers a pane tmux gives no width for, which leaves
// the renderer on its own default rather than rendering to nothing.
func TestStageWrapUnmeasurablePane(t *testing.T) {
	stage := NewStage(&recordingTmux{}, appconfig.DefaultSplit)
	stage.Init("%0")

	assert.Equal(t, 0, stage.wrap())
}

// TestStageHideOnlyTellsTmuxOnAChange covers the zoom that takes the stage away for a
// form. The panel reconciles it from the screen on every message, so asking for the state
// the stage is already in has to cost nothing.
func TestStageHideOnlyTellsTmuxOnAChange(t *testing.T) {
	tmux := &recordingTmux{}
	stage := NewStage(tmux, appconfig.DefaultSplit)
	stage.Init("%0")

	stage.Hide(false)
	stage.Hide(true)
	stage.Hide(true)
	stage.Hide(false)

	assert.Equal(t, []bool{true, false}, tmux.zooms)
}

// TestFormTakesTheWholeWidth covers a form opening and being abandoned. A form has
// nothing to preview, so the stage is zoomed away while it is up and comes back as the
// screen behind it does.
func TestFormTakesTheWholeWidth(t *testing.T) {
	tmux := &recordingTmux{}
	m := infoModel(tmux)

	opened, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	form, ok := opened.(model)
	require.True(t, ok)
	require.True(t, form.screen.isForm())
	assert.Equal(t, []bool{true}, tmux.zooms)

	aborted, _ := form.Update(tea.KeyMsg{Type: tea.KeyEsc})
	closed, ok := aborted.(model)
	require.True(t, ok)
	require.False(t, closed.screen.isForm())
	assert.Equal(t, []bool{true, false}, tmux.zooms)
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
			want: []string{"less", "-R", "-X", "-Ps", "/tmp/stage"},
		},
		{
			name: "the first line is the top already",
			line: 1,
			want: []string{"less", "-R", "-X", "-Ps", "/tmp/stage"},
		},
		{
			name: "a line further down is where the pager opens",
			line: 42,
			want: []string{"less", "-R", "-X", "-Ps", "+42", "/tmp/stage"},
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

// TestStageInitReclaimsTheWindow covers the panel starting into a window the last one
// left panes in, which is what a reload or a panel that died leaves behind. An agent
// staged at the time is sitting in that window as its own pane, so it goes back to a
// window of its own, and the last stage's pane is killed rather than left for the new
// stage to split beside.
func TestStageInitReclaimsTheWindow(t *testing.T) {
	tmux := &recordingTmux{
		siblings: []string{"%7", "%8"},
		owners:   map[string]string{"%7": "019fbb22-3735-71a9-ad83-acf4aa537acb"},
	}

	NewStage(tmux, 0.4).Init("%0")

	assert.Equal(t, []string{"%7"}, tmux.broken, "a staged agent is returned to its own window")
	assert.Equal(t, []string{"%8"}, tmux.killed, "the last stage's own pane is not kept")
}

// TestStageInitLeavesAnEmptyWindowAlone covers the ordinary first start, where the panel
// is the only pane. Nothing is reclaimed, since there is nothing there to reclaim.
func TestStageInitLeavesAnEmptyWindowAlone(t *testing.T) {
	tmux := &recordingTmux{}

	NewStage(tmux, 0.4).Init("%0")

	assert.Empty(t, tmux.broken)
	assert.Empty(t, tmux.killed)
}
