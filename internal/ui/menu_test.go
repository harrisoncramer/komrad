package ui_test

import (
	"testing"

	"komrad/internal/panes"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMenu covers the menu space opens, the hovered row's entries over the screen's own.
func TestMenu(t *testing.T) {
	t.Parallel()

	t.Run("a screen with no hovered row opens its own band alone", func(t *testing.T) {
		t.Parallel()

		harness := openSchedules(t, newLiveWorkflow("w1", "api"))
		harness.press("space")

		frame := harness.readFrame()
		assert.Contains(t, frame, "› new schedule")
		assert.NotContains(t, frame, "logs")
	})

	t.Run("an agent's entries sit over the agents list's own", func(t *testing.T) {
		t.Parallel()

		harness := openSubpages(t, "quiet")

		frame := harness.readFrame()
		assert.Contains(t, frame, "› logs")
		assert.Contains(t, frame, "───")
		assert.Contains(t, frame, "new task")
	})

	t.Run("a screen with no band of its own opens nothing", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())
		before := harness.readFrame()

		harness.press("space")

		assert.Equal(t, before, harness.readFrame())
	})

	t.Run("the legend offers it only where it opens", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name  string
			open  func(*testing.T) *panelHarness
			wants bool
		}{
			{
				name: "off an agent",
				open: func(t *testing.T) *panelHarness {
					harness := openSchedules(t, buildSchedules()...)
					harness.press(widenListPresses...)

					return harness
				},
				wants: true,
			},
			{
				name:  "on the logs",
				open:  func(t *testing.T) *panelHarness { return openLogs(t, tailOfTwo()) },
				wants: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				harness := tt.open(t)
				harness.press("?")

				if tt.wants {
					assert.Regexp(t, `space +menu`, readLegend(harness.readFrame()))
				} else {
					assert.NotRegexp(t, `space +menu`, readLegend(harness.readFrame()))
				}
			})
		}
	})

	t.Run("an entry that closes it hands the window back with the stage in one move", func(t *testing.T) {
		t.Parallel()

		harness := openTriage(t, 3, buildTriage()...)
		harness.press("t")
		harness.fireTimers()
		harness.press("space")
		sent := len(harness.panel.ReadMoveBatches())

		harness.press("z")

		batches := harness.panel.ReadMoveBatches()[sent:]
		require.Len(t, batches, 1)
		assert.Contains(t, batches[0], panes.KindZoom)
		assert.Contains(t, batches[0], panes.KindRespawn)
	})
}
