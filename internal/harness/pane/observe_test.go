package pane

import (
	"testing"

	"komrade/internal/harness"
	"komrade/internal/harnesstest"

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
