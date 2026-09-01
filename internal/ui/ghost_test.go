package ui

import (
	"strings"
	"testing"
	"time"

	"weave/config"
	"weave/lib/xptr"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ghostTree(width int) model {
	now := time.Now()

	return model{
		frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		tags:   tags,
		width:  width,
		height: 40,
		listing: listing{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "weave"}},
			agents: []agentItem{
				{
					agent: agentstore.Agent{
						ID:             "a1",
						Name:           "bare",
						WorkflowID:     "w1",
						State:          agentstore.StateThinking,
						StateChangedAt: now.Add(-6 * time.Minute),
					},
					alive: true,
				},
				{
					agent: agentstore.Agent{
						ID:              "a2",
						Name:            "decorated",
						WorkflowID:      "w1",
						State:           agentstore.StateIdle,
						StateChangedAt:  now.Add(-27 * time.Minute),
						Tag:             xptr.Ptr("✓"),
						ScheduleID:      xptr.Ptr("s1"),
						WorktreeRemoved: true,
					},
					alive:      true,
					unreadMail: true,
				},
			},
		},
	}
}

// TestGhostSlotHoldsOneColumn pins that the slot lands at the same column whatever icons a row
// wears, since a slot that moves with the icons is not a column anybody can read down.
func TestGhostSlotHoldsOneColumn(t *testing.T) {
	rows := strings.Split(ghostTree(120).agentsView(), "\n")

	bare, ok := rowNamed(rows, "bare")
	require.True(t, ok)
	decorated, ok := rowNamed(rows, "decorated")
	require.True(t, ok)

	assert.Equal(t, ghostColumn+ghostWidth, lipgloss.Width(strings.TrimRight(bare, " ")))
	assert.Equal(t, ghostColumn+ghostWidth, lipgloss.Width(strings.TrimRight(decorated, " ")))
	assert.True(t, strings.HasSuffix(ansi.Strip(bare), "  6m"), "the bare row ends in its span")
	assert.True(t, strings.HasSuffix(ansi.Strip(decorated), " 27m"), "and so does the decorated one")
}

// TestGhostSlotHiddenInANarrowPane covers the pane too small to hold the row and the slot both,
// where pulling the slot in would only stack it back onto the icons.
func TestGhostSlotHiddenInANarrowPane(t *testing.T) {
	rows := strings.Split(ghostTree(ghostColumn+ghostWidth+1).agentsView(), "\n")

	bare, ok := rowNamed(rows, "bare")
	require.True(t, ok)
	assert.NotContains(t, ansi.Strip(bare), "6m")
}

// TestGhostSlotSkipsHeaders pins that a workflow header carries no span, since a workflow is
// not in a state and has nothing to be counting.
func TestGhostSlotSkipsHeaders(t *testing.T) {
	rows := strings.Split(ghostTree(120).agentsView(), "\n")

	header, ok := rowNamed(rows, "weave")
	require.True(t, ok)
	assert.Equal(t, strings.TrimRight(ansi.Strip(header), " "), ansi.Strip(header))
}

// TestGhostTextWithoutATimestamp covers an agent whose state change never reached the panel,
// which draws no slot rather than a span counted from the zero time.
func TestGhostTextWithoutATimestamp(t *testing.T) {
	assert.Empty(t, ghostText(agentItem{agent: agentstore.Agent{Name: "unknown"}}))
}
