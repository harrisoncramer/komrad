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

// TestGhostSlotClearsTheWidestRow renders an agent wearing every icon under a full-width name,
// which is the row the column has to clear and so the case that catches an icon added without
// the count positioning the column being moved with it.
func TestGhostSlotClearsTheWidestRow(t *testing.T) {
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

	assert.Equal(t, ghostColumn+ghostWidth, lipgloss.Width(strings.TrimRight(widest, " ")))
	assert.True(t, strings.HasSuffix(ansi.Strip(widest), " 27m"), "the span still sits in its slot")
}

// TestGhostSlotHiddenWhenTheRowWouldOverrun pins that the pane is measured against the row in
// hand rather than against the widest one an agent could draw, so a long name loses its span in
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
	row := strings.Repeat("x", ghostColumn)

	for width := range ghostColumn + ghostWidth + ghostGap + 4 {
		assert.LessOrEqual(t, lipgloss.Width(ghostSlot(row, "27m", width)), max(width, len(row)),
			"a row drawn at width %d", width)
	}
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
