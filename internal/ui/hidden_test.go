package ui

import (
	"strings"
	"testing"

	"weave/config"
	"weave/internal/ui/layout"
	"weave/internal/ui/panel"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func hiddenTree() model {
	return model{
		frame: layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:  config.DefaultKeybindings(),
		listing: listing{workflows: []panel.Workflow{
			{ID: "w1", Name: "backend"},
			{ID: "w2", Name: "frontend"},
		}, agents: []panel.AgentRow{
			{Agent: panel.Agent{
				ID:         "a1",
				Name:       "auth",
				WorkflowID: "w1",
			}},
			{Agent: panel.Agent{
				ID:         "a2",
				Name:       "chore",
				WorkflowID: "w1",
				Hidden:     true,
			}},
			{Agent: panel.Agent{
				ID:         "a3",
				Name:       "sweep",
				WorkflowID: "w1",
				Hidden:     true,
			}},
			{Agent: panel.Agent{
				ID:         "a4",
				Name:       "ui",
				WorkflowID: "w2",
			}},
		}},
	}
}

// TestAgentRowsHidden pins that a hidden agent gives up its row to its workflow's count and
// returns to its own place among the others once the hidden ones are shown.
func TestAgentRowsHidden(t *testing.T) {
	tests := []struct {
		name       string
		showHidden bool
		wantRows   []string
	}{
		{
			name: "a hidden agent takes no row",
			wantRows: []string{
				"header:backend",
				"agent:auth",
				"header:frontend",
				"agent:ui",
			},
		},
		{
			name:       "and comes back under its own workflow when the hidden ones are shown",
			showHidden: true,
			wantRows: []string{
				"header:backend",
				"agent:auth",
				"agent:chore",
				"agent:sweep",
				"header:frontend",
				"agent:ui",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := hiddenTree()
			m.agents.showHidden = tt.showHidden

			rows := m.agents.rows(m.shared())
			assert.Equal(t, tt.wantRows, describeRows(rows))

			require.True(t, rows[0].header)
			assert.Equal(t, 2, rows[0].hiddenCount)
		})
	}
}

// TestToggleHiddenAgentsKeepsTheCursor pins that the cursor follows the agent it was on.
func TestToggleHiddenAgentsKeepsTheCursor(t *testing.T) {
	m := hiddenTree()
	m.agents.cursor = 3

	m.agents.toggleHidden(&m, m.shared(), "")
	revealed := m
	require.True(t, revealed.agents.showHidden)

	hovered, ok := revealed.agents.selectedAgent(revealed.shared())
	require.True(t, ok)
	assert.Equal(t, "ui", hovered.Agent.Name)
}

// TestHiddenHelpIsOneLine pins the two hidden keys sharing a line on an agent and only the
// toggle on a workflow header.
func TestHiddenHelpIsOneLine(t *testing.T) {
	tests := []struct {
		name        string
		showHidden  bool
		cursor      int
		want        string
		wantsAbsent []string
	}{
		{
			name:        "on a workflow header",
			want:        "show hidden",
			wantsAbsent: []string{"hide/show hidden"},
		},
		{
			name:        "on a listed agent",
			cursor:      1,
			want:        "hide/show hidden",
			wantsAbsent: []string{"unhide"},
		},
		{
			name:       "on a revealed agent",
			showHidden: true,
			cursor:     2,
			want:       "unhide/put hidden away",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := hiddenTree()
			m.agents.showHidden = tt.showHidden
			m.agents.cursor = tt.cursor
			m.helpExpanded = true

			got := ansi.Strip(m.renderHelp(m.agentHelp()))
			assert.Contains(t, got, tt.want)
			assert.Equal(t, 1, strings.Count(got, "hidden away")+strings.Count(got, "show hidden"))

			for _, absent := range tt.wantsAbsent {
				assert.NotContains(t, got, absent)
			}
		})
	}
}

// TestAgentsViewHidden pins the rendered screen, where a hidden agent is absent while its
// workflow reports the count and present once the hidden ones are shown.
func TestAgentsViewHidden(t *testing.T) {
	m := hiddenTree()

	away := m.agents.view(m.shared())
	assert.NotContains(t, away, "chore")
	assert.Contains(t, away, "+2")

	m.agents.showHidden = true
	shown := m.agents.view(m.shared())
	assert.Contains(t, shown, "chore")
	assert.NotContains(t, shown, "+2")
}
