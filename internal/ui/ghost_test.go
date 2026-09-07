package ui

import (
	"strings"
	"testing"

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
	return model{
		frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		width:  width,
		height: 40,
		listing: listing{
			marks:     map[string]string{agentRowKey("a2"): "a"},
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "weave"}},
			agents: []agentItem{
				{
					agent: agentstore.Agent{
						ID:         "a1",
						Name:       "bare",
						WorkflowID: "w1",
						State:      agentstore.StateThinking,
					},
					alive: true,
				},
				{
					agent: agentstore.Agent{
						ID:              "a2",
						Name:            "decorated",
						WorkflowID:      "w1",
						State:           agentstore.StateIdle,
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

// TestGhostSlotHoldsOneColumn pins that the slot opens every row at the same width whatever
// icons the row wears, since a column that moves with the row is not one anybody can read down.
func TestGhostSlotHoldsOneColumn(t *testing.T) {
	rows := strings.Split(ghostTree(120).agentsView(), "\n")
	blank := strings.Repeat(" ", ghostWidth+ghostGap)

	for _, name := range []string{"weave", "bare", "decorated"} {
		row, ok := rowNamed(rows, name)
		require.True(t, ok)
		assert.True(t, strings.HasPrefix(ansi.Strip(row), blank), "the %s row opens with the slot", name)
	}
}

// TestGhostSlotKeepsTheWidestRow renders an agent wearing every icon under a full-width name,
// which is the row closest to overrunning a pane and so the one that would lose its slot first.
func TestGhostSlotKeepsTheWidestRow(t *testing.T) {
	m := ghostTree(120)
	m.tree.showHidden = true
	m.listing.agents = append(m.listing.agents, agentItem{
		agent: agentstore.Agent{
			ID:              "a3",
			Name:            strings.Repeat("x", nameWidth),
			WorkflowID:      "w1",
			State:           agentstore.StateIdle,
			ScheduleID:      xptr.Ptr("s1"),
			Hidden:          true,
			WorktreeRemoved: true,
		},
		alive:      true,
		unreadMail: true,
	})

	rows := strings.Split(m.agentsView(), "\n")
	widest, ok := rowNamed(rows, strings.Repeat("x", nameWidth))
	require.True(t, ok)

	assert.True(t, strings.HasPrefix(ansi.Strip(widest), strings.Repeat(" ", ghostWidth+ghostGap)),
		"the slot still opens the row")
}

// TestGhostSlotHiddenWhenTheRowWouldOverrun pins that the pane is measured against the row in
// hand rather than against the widest one an agent could draw, so a long name loses its slot in
// a pane a short one keeps it in rather than wrapping onto the row below.
func TestGhostSlotHiddenWhenTheRowWouldOverrun(t *testing.T) {
	short := "      • short"
	long := "      • " + strings.Repeat("x", nameWidth)
	width := lipgloss.Width(long) + ghostGap + ghostWidth - 1

	assert.NotEqual(t, short, ghostSlot(short, width))
	assert.Equal(t, long, ghostSlot(long, width))
}

// TestGhostSlotNeverOverrunsThePane walks a long row across every pane width the slot could be
// drawn at, since a row wider than its pane wraps and shifts every row under it.
func TestGhostSlotNeverOverrunsThePane(t *testing.T) {
	row := strings.Repeat("x", nameWidth+ghostGap)

	for width := range nameWidth + ghostWidth + 2*ghostGap + 4 {
		assert.LessOrEqual(t, lipgloss.Width(ghostSlot(row, width)), max(width, len(row)),
			"a row drawn at width %d", width)
	}
}
