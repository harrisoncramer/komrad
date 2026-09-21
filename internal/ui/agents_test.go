package ui

import (
	"strings"
	"testing"
	"time"

	"weave/config"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"
	"weave/internal/ui/paint"
	"weave/lib/xslice"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAgentsViewRowsOpenAtTheGutter pins how far in a row starts and that the gutter holds its
// columns on every kind of row, an agent opening on its checkout columns and a workflow, which
// owns no checkout, on the three blank cells those columns leave behind.
func TestAgentsViewRowsOpenAtTheGutter(t *testing.T) {
	m := model{
		frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		width:  120,
		height: 40,
		listing: listing{
			workflows: []workflowstore.Workflow{{
				ID:       "w1",
				Name:     "weave",
				Worktree: workflowstore.WorktreeConfig{Enabled: true},
			}},
			agents: []agentItem{
				{
					agent: agentstore.Agent{
						ID:         "a1",
						Name:       "worker",
						WorkflowID: "w1",
						State:      agentstore.StateIdle,
					},
					alive: true,
				},
				{
					agent: agentstore.Agent{
						ID:         "o1",
						Name:       "orchestrator",
						WorkflowID: "w1",
						Role:       agentstore.RoleOrchestrator,
						State:      agentstore.StateIdle,
					},
					alive: true,
				},
			},
		},
	}
	m.tree.cursor = -1

	rows := strings.Split(m.agentsView(), "\n")
	opening := len(paint.Panel.MarkGutter("")) + 2

	agent, ok := rowNamed(rows, "worker")
	require.True(t, ok)
	stripped := ansi.Strip(agent)
	assert.Equal(t, opening, len(stripped)-len(strings.TrimLeft(stripped, " ")),
		"the worker row opens at the gutter")

	workflow, ok := rowNamed(rows, "weave")
	require.True(t, ok)
	assert.Equal(t, barColumn(t, agent), barColumn(t, workflow),
		"the bar stands in one column whatever the row is")
}

// quietBar is the bar an agent with nothing to say draws.
const quietBar = "│"

// barColumn is how far along a drawn row its status bar stands.
func barColumn(t *testing.T, row string) int {
	t.Helper()

	stripped := ansi.Strip(row)
	at := strings.Index(stripped, quietBar)
	require.GreaterOrEqual(t, at, 0, "the row draws no bar at all")

	return ansi.StringWidth(stripped[:at])
}

// agentsSortModel is the agents list over two workflows, the first holding three families whose
// created, activity and name orders all differ, one of them with two children and one named in
// capitals.
func agentsSortModel() model {
	at := func(minutes int) time.Time {
		return time.Date(2026, 9, 20, 12, minutes, 0, 0, time.UTC)
	}
	alpha := "alpha"

	m := worktreesModel()
	m.screen = screenAgents
	m.width, m.height = 200, 40
	m.listing.workflows = []workflowstore.Workflow{{ID: "w1", Name: "weave"}, {ID: "w2", Name: "chariot"}}
	m.listing.agents = []agentItem{
		{agent: agentstore.Agent{
			ID:             "delta",
			Name:           "delta",
			WorkflowID:     "w1",
			CreatedAt:      at(30),
			StateChangedAt: at(10),
		}},
		{agent: agentstore.Agent{
			ID:             "charlie",
			Name:           "charlie",
			WorkflowID:     "w1",
			ParentAgentID:  &alpha,
			CreatedAt:      at(25),
			StateChangedAt: at(5),
		}},
		{agent: agentstore.Agent{
			ID:             "zulu",
			Name:           "Zulu",
			WorkflowID:     "w1",
			CreatedAt:      at(20),
			StateChangedAt: at(30),
		}},
		{agent: agentstore.Agent{
			ID:             "alpha",
			Name:           "alpha",
			WorkflowID:     "w1",
			CreatedAt:      at(10),
			StateChangedAt: at(20),
		}},
		{agent: agentstore.Agent{
			ID:             "bob",
			Name:           "bob",
			WorkflowID:     "w1",
			ParentAgentID:  &alpha,
			CreatedAt:      at(5),
			StateChangedAt: at(40),
		}},
		{agent: agentstore.Agent{
			ID:             "echo",
			Name:           "echo",
			WorkflowID:     "w2",
			CreatedAt:      at(0),
			StateChangedAt: at(50),
		}},
	}

	return m
}

// TestAgentRowsOrderFamiliesByTheColumn covers the families under a workflow ordered by their
// head in the chosen column and direction, each family's children following their head in the
// same order, created down being the listing's own order, a name in capitals sorting as its
// lowercase, and the workflows keeping theirs.
func TestAgentRowsOrderFamiliesByTheColumn(t *testing.T) {
	tests := []struct {
		name  string
		state tabsState
		want  []string
	}{
		{
			name:  "created down is the fallback and the listing's own order",
			state: tabsState{},
			want:  []string{"workflow:w1", "agent:delta", "agent:zulu", "agent:alpha", "agent:charlie", "agent:bob", "workflow:w2", "agent:echo"},
		},
		{
			name:  "created up is the oldest family first",
			state: tabsState{chosen: "created", direction: tabUp},
			want:  []string{"workflow:w1", "agent:alpha", "agent:bob", "agent:charlie", "agent:zulu", "agent:delta", "workflow:w2", "agent:echo"},
		},
		{
			name:  "activity down is the family whose head changed state last first",
			state: tabsState{chosen: "activity", direction: tabDown},
			want:  []string{"workflow:w1", "agent:zulu", "agent:alpha", "agent:bob", "agent:charlie", "agent:delta", "workflow:w2", "agent:echo"},
		},
		{
			name:  "name up",
			state: tabsState{chosen: "name", direction: tabUp},
			want:  []string{"workflow:w1", "agent:alpha", "agent:bob", "agent:charlie", "agent:delta", "agent:zulu", "workflow:w2", "agent:echo"},
		},
		{
			name:  "name down",
			state: tabsState{chosen: "name", direction: tabDown},
			want:  []string{"workflow:w1", "agent:zulu", "agent:delta", "agent:alpha", "agent:charlie", "agent:bob", "workflow:w2", "agent:echo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := agentsSortModel()
			m.tree.sort = tt.state

			assert.Equal(t, tt.want, xslice.Map(m.agentRows(), agentRow.key))
		})
	}
}

// TestAgentsTabsKeepTheCursorOnItsRow covers the cursor following its row as the tree is
// reordered under it, the name tab opening with its arrow up, and escape leaving the order
// standing with the cursor still on its row.
func TestAgentsTabsKeepTheCursorOnItsRow(t *testing.T) {
	m := agentsSortModel()
	m.tree.cursor = 3
	require.Equal(t, "agent:alpha", agentsList().hovered(m))

	tabbed := send(t, send(t, m, "f"), "tab")
	assert.Equal(t, tabsState{chosen: "activity", focused: true}, tabbed.tree.sort)
	assert.Equal(t, "agent:alpha", agentsList().hovered(tabbed))
	assert.Equal(t, 2, tabbed.tree.cursor)

	named := send(t, tabbed, "tab")
	assert.Equal(t, tabsState{chosen: "name", direction: tabUp, focused: true}, named.tree.sort)
	assert.Equal(t, "agent:alpha", agentsList().hovered(named))
	assert.Equal(t, 1, named.tree.cursor)

	left := send(t, named, "esc")
	assert.Equal(t, tabsState{chosen: "name", direction: tabUp}, left.tree.sort)
	assert.Equal(t, "agent:alpha", agentsList().hovered(left))
	assert.Equal(t, 1, left.tree.cursor)
}
