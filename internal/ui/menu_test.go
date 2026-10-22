package ui_test

import (
	"strings"
	"testing"
	"time"

	"komrad/internal/panes"
	"komrad/internal/ui/paneltest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMenu covers the menu space opens, the hovered row's entries over the screen's own.
func TestMenu(t *testing.T) {
	t.Parallel()

	t.Run("a screen with no hovered row opens its own band alone", func(t *testing.T) {
		t.Parallel()

		harness := openSchedules(t, paneltest.Workflow("w1", "api"))
		harness.Press("space")

		frame := harness.Frame()
		assert.Contains(t, frame, "› new schedule")
		assert.NotContains(t, frame, "logs")
	})

	t.Run("an agent's entries sit over the agents list's own, with no new task", func(t *testing.T) {
		t.Parallel()

		harness := paneltest.OpenAgents(t, buildSubpagesAgents()...)
		harness.MoveToRow("reviewer")
		harness.Press("space")

		frame := harness.Frame()
		assert.Contains(t, frame, "› logs")
		assert.Contains(t, frame, "edit files")
		assert.NotContains(t, frame, "new task")
		assert.Less(t, strings.Index(frame, "edit files"), strings.Index(frame, "new workflow"))
	})

	t.Run("a workflow's entries, new task among them, sit over the agents list's own", func(t *testing.T) {
		t.Parallel()

		harness := paneltest.OpenAgents(t, buildSubpagesAgents()...)
		harness.Press("g", "g", "space")

		frame := harness.Frame()
		require.Contains(t, frame, "new task")
		require.Contains(t, frame, "───")
		assert.Less(t, strings.Index(frame, "new task"), strings.Index(frame, "───"))
		assert.Less(t, strings.Index(frame, "edit workflow"), strings.Index(frame, "───"))
		assert.Less(t, strings.Index(frame, "───"), strings.Index(frame, "new workflow"))
	})

	t.Run("a screen with no band of its own opens nothing", func(t *testing.T) {
		t.Parallel()

		harness := paneltest.OpenAgents(t, paneltest.Workflow("w1", "komrad"), paneltest.Reviewer("reviewer", "reviewer", "writer"))
		harness.MoveToRow("reviewer")
		harness.Press("space", "l")
		before := harness.Frame()

		harness.Press("space")

		assert.Equal(t, before, harness.Frame())
	})

	t.Run("the legend offers it only where it opens", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name  string
			open  func(*testing.T) *paneltest.Panel
			wants bool
		}{
			{
				name: "off an agent",
				open: func(t *testing.T) *paneltest.Panel {
					harness := openSchedules(t, buildSchedules()...)
					harness.Press(paneltest.WidenListPresses...)

					return harness
				},
				wants: true,
			},
			{
				name: "on the logs",
				open: func(t *testing.T) *paneltest.Panel {
					harness := paneltest.OpenAgents(t, paneltest.Workflow("w1", "komrad"), paneltest.Reviewer("reviewer", "reviewer", "writer"))
					harness.MoveToRow("reviewer")
					harness.Press("space", "l")

					return harness
				},
				wants: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				harness := tt.open(t)
				harness.Press("?")

				if tt.wants {
					assert.Regexp(t, `space +menu`, paneltest.Legend(harness.Frame()))
				} else {
					assert.NotRegexp(t, `space +menu`, paneltest.Legend(harness.Frame()))
				}
			})
		}
	})

	t.Run("an entry that closes it hands the window back with the stage in one move", func(t *testing.T) {
		t.Parallel()

		harness := openTriage(t, 3, buildTriage()...)
		harness.Press("t")
		harness.Advance(time.Second)
		harness.Press("space")
		sent := len(harness.Tmux().MoveBatches())

		harness.Press("z")

		batches := harness.Tmux().MoveBatches()[sent:]
		require.Len(t, batches, 1)
		assert.Contains(t, batches[0], panes.KindZoom)
		assert.Contains(t, batches[0], panes.KindRespawn)
	})
}
