package ui_test

import (
	"fmt"
	"strings"
	"testing"

	workflowv1 "komrad/proto/gen/workflow/v1"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWheelScrollsTheHalfUnderTheMouse covers the block beside the list and the list each taking
// the wheel when the mouse is over them.
func TestWheelScrollsTheHalfUnderTheMouse(t *testing.T) {
	tests := []struct {
		name string
		open func(t *testing.T) *panelHarness
	}{
		{
			name: "a workflow's context beside the agents",
			open: func(t *testing.T) *panelHarness {
				t.Helper()

				harness := openAgents(t, withLongContext(newLiveWorkflow("w1", "backend")), newAgentAt("auth", "w1", 0))
				harness.press("k")

				return harness
			},
		},
		{
			name: "an archived workflow's context beside the archive",
			open: func(t *testing.T) *panelHarness {
				t.Helper()

				return openArchive(t,
					withLongContext(newArchivedWorkflow("w1", "backend")), newArchivedAgent("a1", "auth", "w1"),
					withLongContext(newArchivedWorkflow("w2", "billing")), newArchivedAgent("a2", "ledger", "w2"),
				)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Run("over the block it scrolls the block and leaves the list", func(t *testing.T) {
				harness := tt.open(t)
				before := harness.readFrame()
				require.Contains(t, before, "context line 0")

				wheelOver(harness, defaultHarnessWidth-1, false)

				frame := harness.readFrame()
				assert.NotEqual(t, readStage(before), readStage(frame))
				assert.Equal(t, readHoveredRow(before), readHoveredRow(frame))
			})

			t.Run("the block opens at its top again on another row", func(t *testing.T) {
				harness := tt.open(t)
				top := readStage(harness.readFrame())
				wheelOver(harness, defaultHarnessWidth-1, false)

				harness.press("j", "k")

				assert.Equal(t, top, readStage(harness.readFrame()))
			})

			t.Run("over the list it moves the list and leaves the block", func(t *testing.T) {
				harness := tt.open(t)
				before := harness.readFrame()

				wheelOver(harness, 1, false)

				frame := harness.readFrame()
				assert.NotEqual(t, readHoveredRow(before), readHoveredRow(frame))
			})
		})
	}
}

func wheelOver(harness *panelHarness, x int, up bool) {
	button := tea.MouseWheelDown
	if up {
		button = tea.MouseWheelUp
	}

	harness.send(tea.MouseWheelMsg{
		X:      x,
		Y:      5,
		Button: button,
	})
}

func withLongContext(workflow *workflowv1.Workflow) *workflowv1.Workflow {
	var context strings.Builder
	for i := range 80 {
		fmt.Fprintf(&context, "context line %d\n\n", i)
	}

	workflow.Context = context.String()

	return workflow
}
