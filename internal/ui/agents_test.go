package ui

import (
	"strings"
	"testing"

	"weave/config"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAgentsViewRowsOpenAtTheGutter pins how far in a row starts, which is the cursor and the
// mark column and nothing else, since a column held open for something unwritten reads as the
// whole list having been nudged sideways.
func TestAgentsViewRowsOpenAtTheGutter(t *testing.T) {
	m := model{
		frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		width:  120,
		height: 40,
		listing: listing{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "weave"}},
			agents: []agentItem{{
				agent: agentstore.Agent{
					ID:         "a1",
					Name:       "worker",
					WorkflowID: "w1",
					State:      agentstore.StateIdle,
				},
				alive: true,
			}},
		},
	}
	m.tree.cursor = -1

	rows := strings.Split(m.agentsView(), "\n")
	opening := len(markGutter("")) + 2

	for _, name := range []string{"weave", "worker"} {
		row, ok := rowNamed(rows, name)
		require.True(t, ok)

		stripped := ansi.Strip(row)
		assert.Equal(t, opening, len(stripped)-len(strings.TrimLeft(stripped, " ")),
			"the %s row opens at the gutter", name)
	}
}
