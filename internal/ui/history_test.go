package ui

import (
	"slices"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openAgentRow(t *testing.T, m model, id string) model {
	t.Helper()

	idx := rowIndexOfKey(m.agentRows(), agentRowKey(id))
	require.GreaterOrEqual(t, idx, 0, "agent %q is on the list", id)

	m.tree.cursor = idx

	return pressHistory(t, m, m.keys.Agents.Open[0])
}

func pressHistory(t *testing.T, m model, keys ...string) model {
	t.Helper()

	var next tea.Model = m
	for _, key := range keys {
		updated, _ := next.(model).Update(keyMsg(key))
		next = updated
	}

	after, ok := next.(model)
	require.True(t, ok)

	return after
}

func hoveredAgentID(t *testing.T, m model) string {
	t.Helper()

	row, ok := m.hoveredRow()
	require.True(t, ok, "a row is under the cursor")
	require.False(t, row.header, "the cursor is on an agent rather than a workflow")

	return row.item.agent.ID
}

func dropAgent(m *model, id string) {
	m.listing.agents = slices.DeleteFunc(m.listing.agents, func(item agentItem) bool {
		return item.agent.ID == id
	})
}

// TestHistoryWalksTheAgentsOpened pins the two keys moving the cursor over the agents opened
// before it, and back again.
func TestHistoryWalksTheAgentsOpened(t *testing.T) {
	m := selectionTree()
	for _, id := range []string{"a1", "a2", "a3"} {
		m = openAgentRow(t, m, id)
	}

	back := pressHistory(t, m, m.keys.History.Back[0])
	assert.Equal(t, "a2", hoveredAgentID(t, back))

	further := pressHistory(t, back, m.keys.History.Back[0])
	assert.Equal(t, "a1", hoveredAgentID(t, further))

	forward := pressHistory(t, further, m.keys.History.Forward[0])
	assert.Equal(t, "a2", hoveredAgentID(t, forward))
}

// TestHistoryHasNowhereToGo pins the keys doing nothing at either end of the walk.
func TestHistoryHasNowhereToGo(t *testing.T) {
	tests := []struct {
		name   string
		opened []string
		key    func(m model) string
		wantID string
	}{
		{
			name:   "nothing opened leaves the cursor alone",
			opened: nil,
			key:    func(m model) string { return m.keys.History.Back[0] },
			wantID: "a1",
		},
		{
			name:   "one agent opened has nothing behind it",
			opened: []string{"a1"},
			key:    func(m model) string { return m.keys.History.Back[0] },
			wantID: "a1",
		},
		{
			name:   "the agent opened last has nothing ahead of it",
			opened: []string{"a1", "a2"},
			key:    func(m model) string { return m.keys.History.Forward[0] },
			wantID: "a2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := selectionTree()
			m.tree.cursor = rowIndexOfKey(m.agentRows(), agentRowKey("a1"))

			for _, id := range tt.opened {
				m = openAgentRow(t, m, id)
			}

			after := pressHistory(t, m, tt.key(m))
			assert.Equal(t, tt.wantID, hoveredAgentID(t, after))
		})
	}
}

// TestHistoryStepsOverAnAgentThatIsGone pins an archived agent being walked past rather than
// stopping the walk.
func TestHistoryStepsOverAnAgentThatIsGone(t *testing.T) {
	m := selectionTree()
	for _, id := range []string{"a1", "a2", "a3"} {
		m = openAgentRow(t, m, id)
	}

	dropAgent(&m, "a2")

	back := pressHistory(t, m, m.keys.History.Back[0])
	assert.Equal(t, "a1", hoveredAgentID(t, back))

	assert.Equal(t, []string{"a1", "a3"}, back.history.GetEntries(), "the agent that is gone is forgotten")

	forward := pressHistory(t, back, m.keys.History.Forward[0])
	assert.Equal(t, "a3", hoveredAgentID(t, forward))
}

// TestOpeningWhereTheWalkStoppedKeepsTheWayForward pins the case the early return in the history
// package exists for: acting on an agent you stepped back to leaves the forward entries alone.
func TestOpeningWhereTheWalkStoppedKeepsTheWayForward(t *testing.T) {
	m := selectionTree()
	for _, id := range []string{"a1", "a2", "a3"} {
		m = openAgentRow(t, m, id)
	}

	back := pressHistory(t, m, m.keys.History.Back[0])
	require.Equal(t, "a2", hoveredAgentID(t, back))

	reopened := pressHistory(t, back, back.keys.Agents.Open[0])

	forward := pressHistory(t, reopened, m.keys.History.Forward[0])
	assert.Equal(t, "a3", hoveredAgentID(t, forward))
}
