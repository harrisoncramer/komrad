package ui

import (
	"testing"
	"time"

	"weave/config"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"

	"github.com/stretchr/testify/assert"
)

func unviewedTree() model {
	tmux := &recordingTmux{sessionClient: panelViewer}
	popup := NewPopupWindow(tmux, "test-session")

	return model{
		frame: layout.New(tmux, config.DefaultLayout()),
		popup: popup,
		keys:  config.DefaultKeybindings(),
		listing: listing{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			agents: []agentItem{
				{
					agent: agentstore.Agent{
						ID:         "a1",
						Name:       "auth",
						WorkflowID: "w1",
					},
					alive:    true,
					unviewed: true,
				},
				{agent: agentstore.Agent{
					ID:         "a2",
					Name:       "api",
					WorkflowID: "w1",
				}, alive: true},
				{
					agent: agentstore.Agent{
						ID:         "a3",
						Name:       "db",
						WorkflowID: "w1",
					},
					alive:    true,
					unviewed: true,
				},
			},
		},
	}
}

// TestDwellViewedWaitsForTheCursorToSettle covers the case the whole rule exists for: the panel
// stages a live agent's own pane on hover, so travelling down the list would otherwise mark
// every row a person passed on the way to the one they wanted.
func TestDwellViewedWaitsForTheCursorToSettle(t *testing.T) {
	at := time.Now()

	m := unviewedTree()
	m.tree.cursor = 1

	m, cmd := m.dwellViewed(at)
	assert.Nil(t, cmd, "the tick the cursor arrived on marks nothing")

	m, cmd = m.dwellViewed(at.Add(viewedDwell / 2))
	assert.Nil(t, cmd, "still inside the dwell")

	_, cmd = m.dwellViewed(at.Add(viewedDwell))
	assert.NotNil(t, cmd, "the pane has been on screen long enough to have been read")
}

// TestDwellViewedRestartsWhenTheCursorMoves pins that the clock belongs to the row rather than
// to the panel, so a walk down the list never accumulates a mark for the row it stops on.
func TestDwellViewedRestartsWhenTheCursorMoves(t *testing.T) {
	at := time.Now()

	m := unviewedTree()
	m.tree.cursor = 1

	m, _ = m.dwellViewed(at)

	m.tree.cursor = 3
	m, cmd := m.dwellViewed(at.Add(viewedDwell))
	assert.Nil(t, cmd, "the cursor only just landed here")

	_, cmd = m.dwellViewed(at.Add(2 * viewedDwell))
	assert.NotNil(t, cmd)
}

// TestDwellViewedSkipsRowsThatShowNoPane covers the rows a dwell says nothing about: a header,
// a row already read, and a dormant agent, which stages a placeholder rather than its output.
func TestDwellViewedSkipsRowsThatShowNoPane(t *testing.T) {
	tests := []struct {
		name   string
		cursor int
		screen screen
		mutate func(*model)
	}{
		{name: "a workflow header", cursor: 0},
		{name: "an agent already read", cursor: 2},
		{
			name:   "a dormant agent",
			cursor: 1,
			mutate: func(m *model) { m.listing.agents[0].alive = false },
		},
		{
			name:   "another screen is up",
			cursor: 1,
			screen: screenSchedules,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			at := time.Now()

			m := unviewedTree()
			m.tree.cursor = tt.cursor
			m.screen = tt.screen
			if tt.mutate != nil {
				tt.mutate(&m)
			}

			m, _ = m.dwellViewed(at)
			_, cmd := m.dwellViewed(at.Add(2 * viewedDwell))
			assert.Nil(t, cmd)
		})
	}
}
