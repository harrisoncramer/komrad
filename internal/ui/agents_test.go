package ui

import (
	"strings"
	"testing"

	"weave/config"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"
	"weave/internal/ui/paint"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAgentsViewRowsOpenAtTheGutter pins how far in a row starts and that the gutter holds its
// columns on every kind of row, an agent opening on its checkout columns and a workflow, which
// owns no checkout, on the bar those columns leave room for.
func TestAgentsViewRowsOpenAtTheGutter(t *testing.T) {
	m := model{
		frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		width:  120,
		height: 40,
		listing: listing{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "weave", Worktree: workflowstore.WorktreeConfig{Enabled: true}}},
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

// quietBar is the bar a running agent with nothing to say draws.
const quietBar = "│"

// barColumn is how far along a drawn row its status bar stands.
func barColumn(t *testing.T, row string) int {
	t.Helper()

	stripped := ansi.Strip(row)
	at := strings.Index(stripped, quietBar)
	require.GreaterOrEqual(t, at, 0, "the row draws no bar at all")

	return ansi.StringWidth(stripped[:at])
}
