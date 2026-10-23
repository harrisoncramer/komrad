package ui_test

import (
	"fmt"
	"strings"
	"testing"

	"komrad/internal/ui/paneltest"
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
		open func(t *testing.T) *paneltest.Panel
	}{
		{
			name: "a workflow's context beside the agents",
			open: func(t *testing.T) *paneltest.Panel {
				t.Helper()

				harness := paneltest.OpenAgents(t, withLongContext(paneltest.Workflow("w1", "backend")), newAgentAt("auth", "w1", 0))
				harness.Press("k")

				return harness
			},
		},
		{
			name: "an archived workflow's context beside the archive",
			open: func(t *testing.T) *paneltest.Panel {
				t.Helper()

				return openArchivedAgents(t,
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
				before := harness.Frame()
				require.Contains(t, before, "context line 0")

				harness.WheelOver(paneltest.DefaultWidth - 1)

				frame := harness.Frame()
				assert.NotContains(t, paneltest.Stage(frame), "context line 0")
				assert.Equal(t, paneltest.HoveredRow(before), paneltest.HoveredRow(frame))
			})

			t.Run("over the block it hands the block the keys", func(t *testing.T) {
				harness := tt.open(t)
				before := harness.Frame()
				harness.WheelOver(paneltest.DefaultWidth - 1)

				harness.Press("G")

				frame := harness.Frame()
				assert.Contains(t, frame, "context line 79")
				assert.Equal(t, paneltest.HoveredRow(before), paneltest.HoveredRow(frame))
			})

			t.Run("the block opens at its top again on another row", func(t *testing.T) {
				harness := tt.open(t)
				top := paneltest.Stage(harness.Frame())
				harness.WheelOver(paneltest.DefaultWidth - 1)
				harness.WheelOver(1)

				harness.Press("k")

				assert.Equal(t, top, paneltest.Stage(harness.Frame()))
			})

			t.Run("turns the filter added up scroll the block as far as the same turns one by one", func(t *testing.T) {
				paced := tt.open(t)
				for range 5 {
					paced.WheelOver(paneltest.DefaultWidth - 1)
				}

				harness := tt.open(t)
				for range 5 {
					harness.Send(tea.MouseWheelMsg{X: paneltest.DefaultWidth - 1, Y: 5, Button: tea.MouseWheelDown})
				}
				harness.Advance(paneltest.WheelFrame)

				assert.Equal(t, paneltest.Stage(paced.Frame()), paneltest.Stage(harness.Frame()))
			})

			t.Run("over the list it moves the list and leaves the block", func(t *testing.T) {
				harness := tt.open(t)
				before := harness.Frame()

				harness.WheelOver(1)

				frame := harness.Frame()
				assert.NotEqual(t, paneltest.HoveredRow(before), paneltest.HoveredRow(frame))
			})
		})
	}
}

func withLongContext(workflow *workflowv1.Workflow) *workflowv1.Workflow {
	var context strings.Builder
	for i := range 80 {
		fmt.Fprintf(&context, "context line %d\n\n", i)
	}

	workflow.Context = context.String()

	return workflow
}
