package pane

import (
	"testing"

	"komrad/internal/harness"
	"komrad/internal/harnesstest"
	"komrad/internal/tmuxtest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestObserveReadsTheFrameThroughItsHarness pins that an observation carries the capture it was
// taken from alongside what the harness made of it.
func TestObserveReadsTheFrameThroughItsHarness(t *testing.T) {
	tests := []struct {
		name  string
		frame string
		want  harness.Observation
	}{
		{
			name:  "an agent part-way through a turn",
			frame: harnesstest.FrameWorking,
			want:  harness.Observation{Activity: harness.ActivityWorking, Frame: harnesstest.FrameWorking},
		},
		{
			name:  "an empty input box",
			frame: harnesstest.FrameIdle,
			want:  harness.Observation{Activity: harness.ActivityIdle, Frame: harnesstest.FrameIdle},
		},
		{
			name:  "a box holding a draft",
			frame: harnesstest.FrameComposing + ": half a thought",
			want: harness.Observation{
				Activity: harness.ActivityComposing,
				Frame:    harnesstest.FrameComposing + ": half a thought",
			},
		},
		{
			name: "a pane drawing nothing at all",
			want: harness.Observation{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPane(NewPaneParams{
				Terminal:  terminalShowing(tt.frame),
				Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}},
			})

			got, err := p.Observe(t.Context(), target)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestObserveRefusesAHarnessItCannotLookUp pins that an unreadable frame is an error rather
// than a reading.
func TestObserveRefusesAHarnessItCannotLookUp(t *testing.T) {
	pane := NewPane(NewPaneParams{Terminal: terminalShowing(harnesstest.FrameWorking), Harnesses: harnesstest.Registry{}})

	_, err := pane.Observe(t.Context(), target)
	require.Error(t, err)
}

// TestDoesFrameShowHarness covers a pane still at its shell against one its harness has drawn.
func TestDoesFrameShowHarness(t *testing.T) {
	tests := []struct {
		name      string
		harnesses harnesstest.Registry
		frame     string
		want      bool
	}{
		{
			name:      "a harness nothing implements is taken at its word",
			harnesses: harnesstest.Registry{},
			frame:     "$ ",
			want:      true,
		},
		{
			name:      "a frame the harness reads as none of its own has yet to draw",
			harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}},
			frame:     harnesstest.FrameUnrecognized,
			want:      false,
		},
		{
			name:      "a frame the harness is working in has drawn",
			harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}},
			frame:     harnesstest.FrameWorking,
			want:      true,
		},
		{
			name:      "a frame the harness is idle in has drawn",
			harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}},
			frame:     harnesstest.FrameIdle,
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPane(NewPaneParams{
				Terminal:  terminalShowing(tt.frame),
				Harnesses: tt.harnesses,
			})

			assert.Equal(t, tt.want, p.DoesFrameShowHarness(target.Pane, target.HarnessID))
		})
	}
}

// target is the agent every test in this package reads.
var target = harness.Target{
	AgentID:   uuid.New(),
	Pane:      "%1",
	HarnessID: harnesstest.FakeID,
}

// terminalShowing is a tmux whose pane shows the frame under a running CLI.
func terminalShowing(frame string) *tmuxtest.Fake {
	terminal := tmuxtest.NewFake()
	terminal.Captures[target.Pane] = frame
	terminal.Commands[target.Pane] = "claude"

	return terminal
}
