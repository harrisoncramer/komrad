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

// TestGhostSlotHoldsOneColumn pins that the slot opens the row and holds the same width whatever
// icons the row wears, since a column that moves with the row is not one anybody can read down.
func TestGhostSlotHoldsOneColumn(t *testing.T) {
	rows := strings.Split(ghostTree(120).agentsView(), "\n")

	bare, ok := rowNamed(rows, "bare")
	require.True(t, ok)
	decorated, ok := rowNamed(rows, "decorated")
	require.True(t, ok)

	assert.True(t, strings.HasPrefix(ansi.Strip(bare), "  6m "), "the bare row opens with its span")
	assert.True(t, strings.HasPrefix(ansi.Strip(decorated), " 27m "), "and so does the decorated one")
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
			StateChangedAt:  time.Now().Add(-27 * time.Minute),
			Tag:             xptr.Ptr("✓"),
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

	assert.True(t, strings.HasPrefix(ansi.Strip(widest), " 27m "), "the span still opens the row")
}

// TestGhostSlotHiddenWhenTheRowWouldOverrun pins that the pane is measured against the row in
// hand rather than against the widest one an agent could draw, so a long name loses its slot in
// a pane a short one keeps it in rather than wrapping onto the row below.
func TestGhostSlotHiddenWhenTheRowWouldOverrun(t *testing.T) {
	short := "      • short"
	long := "      • " + strings.Repeat("x", nameWidth)
	width := lipgloss.Width(long) + ghostGap + ghostWidth - 1

	assert.Contains(t, ghostSlot(short, "6m", width), "6m")
	assert.Equal(t, long, ghostSlot(long, "27m", width))
}

// TestGhostSlotNeverOverrunsThePane walks a long row across every pane width the slot could be
// drawn at, since a row wider than its pane wraps and shifts every row under it.
func TestGhostSlotNeverOverrunsThePane(t *testing.T) {
	row := strings.Repeat("x", nameWidth+ghostGap)

	for width := range nameWidth + ghostWidth + 2*ghostGap + 4 {
		assert.LessOrEqual(t, lipgloss.Width(ghostSlot(row, "27m", width)), max(width, len(row)),
			"a row drawn at width %d", width)
	}
}

// TestGhostSlotPadsHeaders pins that a workflow header holds the column open without a span,
// since a workflow is not in a state and has nothing to be counting, but the rows under it read
// down the same column it starts in.
func TestGhostSlotPadsHeaders(t *testing.T) {
	rows := strings.Split(ghostTree(120).agentsView(), "\n")

	header, ok := rowNamed(rows, "weave")
	require.True(t, ok)

	blank := strings.Repeat(" ", ghostWidth+ghostGap)
	assert.Equal(t, blank, ansi.Strip(header)[:len(blank)])
	assert.Equal(t, strings.TrimRight(ansi.Strip(header), " "), ansi.Strip(header))
}

// TestGhostTextWithoutATimestamp covers an agent whose state change never reached the panel,
// which draws no span rather than one counted from the zero time.
func TestGhostTextWithoutATimestamp(t *testing.T) {
	assert.Empty(t, ghostText(agentItem{agent: agentstore.Agent{Name: "unknown"}}))
}
