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

// hiddenTree is the list the cases below work over, one workflow holding a listed
// agent and two hidden ones, and a second holding nothing hidden at all.
func hiddenTree() model {
	return model{
		frame: layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:  config.DefaultKeybindings(),
		listing: listing{workflows: []workflowstore.Workflow{
			{ID: "w1", Name: "backend"},
			{ID: "w2", Name: "frontend"},
		}, agents: []agentItem{
			{agent: agentstore.Agent{ID: "a1", Name: "auth", WorkflowID: "w1"}},
			{agent: agentstore.Agent{ID: "a2", Name: "chore", WorkflowID: "w1", Hidden: true}},
			{agent: agentstore.Agent{ID: "a3", Name: "sweep", WorkflowID: "w1", Hidden: true}},
			{agent: agentstore.Agent{ID: "a4", Name: "ui", WorkflowID: "w2"}},
		}},
	}
}

// TestAgentRowsHidden covers what the tree does with a hidden agent. It gives up its
// row and is counted against its workflow's header instead, and showing the hidden
// ones puts it back where it belongs rather than in a group of its own. The header's
// count is the same either way, since it is a fact about the workflow rather than
// about what is on screen.
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
			m.tree.showHidden = tt.showHidden

			rows := m.agentRows()
			assert.Equal(t, tt.wantRows, describeRows(rows))

			require.True(t, rows[0].header)
			assert.Equal(t, 2, rows[0].hiddenCount)
		})
	}
}

// TestToggleHiddenAgentsKeepsTheCursor covers where the cursor lands as rows appear
// and vanish above it. It follows the agent it was on, since the rows it was counting
// from are not the rows it is left with.
func TestToggleHiddenAgentsKeepsTheCursor(t *testing.T) {
	m := hiddenTree()
	m.tree.cursor = 3 // frontend's ui, with backend's two hidden agents away

	shown, _ := m.toggleHiddenAgents()
	revealed, ok := shown.(model)
	require.True(t, ok)
	require.True(t, revealed.tree.showHidden)

	hovered, ok := revealed.selectedAgent()
	require.True(t, ok)
	assert.Equal(t, "ui", hovered.agent.Name)
}

// TestHiddenTally covers the count a workflow header carries. It stands in for the
// rows the folder is not drawing, so it goes away once they are drawn.
func TestHiddenTally(t *testing.T) {
	tests := []struct {
		name    string
		count   int
		showing bool
		want    string
	}{
		{
			name: "a workflow hiding nothing says nothing",
		},
		{
			name:    "nor does one whose hidden agents are on screen",
			count:   3,
			showing: true,
		},
		{
			name:  "a workflow with hidden agents carries the count",
			count: 3,
			want:  "+3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hiddenTally(tt.count, tt.showing)
			if tt.want == "" {
				assert.Empty(t, got)
				return
			}

			assert.Contains(t, got, tt.want)
		})
	}
}

// TestHiddenHelpIsOneLine covers the legend for the two hidden keys, which share a line on
// an agent and name the direction each would go. A workflow header takes only the toggle,
// since nothing hides a folder.
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
			m.tree.showHidden = tt.showHidden
			m.tree.cursor = tt.cursor
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

// TestHiddenMark covers the marker on the row itself, which is what tells a revealed
// row apart from the ones that were never hidden.
func TestHiddenMark(t *testing.T) {
	assert.Empty(t, hiddenMark(false))
	assert.Contains(t, hiddenMark(true), hiddenIcon)
}

// TestAgentsViewHidden covers the rendered screen rather than the rows behind it. A
// hidden agent is off it entirely while its workflow reports the count, and showing
// the hidden ones puts the name on screen and the count away.
func TestAgentsViewHidden(t *testing.T) {
	m := hiddenTree()

	away := m.agentsView()
	assert.NotContains(t, away, "chore")
	assert.Contains(t, away, "+2")

	m.tree.showHidden = true
	shown := m.agentsView()
	assert.Contains(t, shown, "chore")
	assert.NotContains(t, shown, "+2")
}
