package pane

import (
	"testing"

	"weave/internal/harness"

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
			frame: busyFrame,
			want:  harness.Observation{Activity: harness.ActivityWorking, Frame: busyFrame},
		},
		{
			name:  "an empty input box",
			frame: idleFrame,
			want:  harness.Observation{Activity: harness.ActivityIdle, Frame: idleFrame},
		},
		{
			name:  "a box holding a draft",
			frame: composingFrame + ": half a thought",
			want: harness.Observation{
				Activity: harness.ActivityComposing,
				Frame:    composingFrame + ": half a thought",
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
				Terminal:  &fakeTerminal{frame: tt.frame},
				Harnesses: fakeHarnesses{harness: &fakeHarness{}},
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
	p := NewPane(NewPaneParams{Terminal: &fakeTerminal{frame: busyFrame}, Harnesses: fakeHarnesses{}})

	_, err := p.Observe(t.Context(), target)
	require.Error(t, err)
}
