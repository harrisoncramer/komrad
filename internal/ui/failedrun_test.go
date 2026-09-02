package ui

import (
	"strings"
	"testing"

	"weave/config"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func failedRunTree() model {
	tmux := &recordingTmux{sessionClient: panelViewer}

	return model{
		frame:  layout.New(tmux, config.DefaultLayout()),
		popup:  NewPopupWindow(tmux, "test-session"),
		keys:   config.DefaultKeybindings(),
		width:  80,
		screen: screenAgents,
		listing: listing{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			agents: []agentItem{
				{
					agent: agentstore.Agent{
						ID:         "a1",
						Name:       "auth",
						WorkflowID: "w1",
					},
					alive:     true,
					failedRun: true,
				},
				{
					agent: agentstore.Agent{
						ID:         "a2",
						Name:       "api",
						WorkflowID: "w1",
					},
					alive: true,
				},
			},
		},
	}
}

// TestFailedRunMarkIsDrawnOnTheRow covers the point of the whole change: a hook that failed
// after the agent came up is visible without opening anything.
func TestFailedRunMarkIsDrawnOnTheRow(t *testing.T) {
	m := failedRunTree()

	rows := strings.Split(m.agentsView(), "\n")

	var carrying, clear string
	for _, row := range rows {
		switch {
		case strings.Contains(row, "auth"):
			carrying = row
		case strings.Contains(row, "api"):
			clear = row
		}
	}

	require.NotEmpty(t, carrying)
	require.NotEmpty(t, clear)
	assert.Contains(t, carrying, failedRunIcon)
	assert.NotContains(t, clear, failedRunIcon)
}

// TestMarkRunsReadOnlyWhileTheRunLogIsUp pins what counts as reading a failed run, since the
// mark is worth nothing if it clears while the person is looking at something else.
func TestMarkRunsReadOnlyWhileTheRunLogIsUp(t *testing.T) {
	tests := []struct {
		name    string
		kind    infoKind
		cursor  int
		wantCmd bool
	}{
		{
			name:   "nothing is open",
			kind:   infoNothing,
			cursor: 1,
		},
		{
			name:   "the agent's own log rather than its runs",
			kind:   infoAgentLog,
			cursor: 1,
		},
		{
			name:   "the run log, on a row carrying no mark",
			kind:   infoRunLog,
			cursor: 2,
		},
		{
			name:    "the run log, on the row that failed",
			kind:    infoRunLog,
			cursor:  1,
			wantCmd: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := failedRunTree()
			m.info.kind = tt.kind
			m.tree.cursor = tt.cursor

			cmd := m.markRunsReadCmd()

			assert.Equal(t, tt.wantCmd, cmd != nil)
		})
	}
}
