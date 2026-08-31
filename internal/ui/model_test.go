package ui

import (
	"testing"

	"weave/config"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClampCursor pins where the cursor lands for each length of list, an empty one
// included.
func TestClampCursor(t *testing.T) {
	tests := []struct {
		name   string
		idx    int
		length int
		want   int
	}{
		{
			name:   "an empty list pins to zero",
			idx:    0,
			length: 0,
			want:   0,
		},
		{
			name:   "a negative index pins to the first row",
			idx:    -1,
			length: 3,
			want:   0,
		},
		{
			name:   "an index past the end pins to the last row",
			idx:    9,
			length: 3,
			want:   2,
		},
		{
			name:   "an index inside the list is left alone",
			idx:    2,
			length: 5,
			want:   2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, clampCursor(tt.idx, tt.length))
		})
	}
}

// TestApplyAgents pins that a refresh takes the listed order as it comes and leaves the
// cursor on the agent it was on rather than on an index a newer agent would shift.
func TestApplyAgents(t *testing.T) {
	tests := []struct {
		name       string
		start      []agentItem
		cursor     int
		fresh      []agentItem
		wantLen    int
		wantCursor string
		wantOrder  []string
	}{
		{
			name:    "a refresh that empties the list stays on the agents screen",
			start:   items("a"),
			fresh:   nil,
			wantLen: 0,
		},
		{
			name:       "the cursor follows its agent rather than its index",
			start:      items("a", "b", "c"),
			cursor:     1,
			fresh:      items("b", "c", "a"),
			wantLen:    3,
			wantCursor: "a",
			wantOrder:  []string{"b", "c", "a"},
		},
		{
			name:       "an agent created elsewhere lands where the listing put it",
			start:      items("a"),
			cursor:     1,
			fresh:      items("b", "a"),
			wantLen:    2,
			wantCursor: "a",
			wantOrder:  []string{"b", "a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{
				screen:  screenAgents,
				listing: listing{workflows: []workflowstore.Workflow{{ID: testWorkflowID, Name: "backend"}}, agents: tt.start}, tree: treeState{rowCursor: rowCursor{cursor: tt.cursor}, placed: true},
				keys: config.DefaultKeybindings(),
			}

			was := rowKeyAt(m.agentRows(), m.tree.cursor)
			m.applyAgents(tt.fresh)
			m.placeTreeCursor(was)

			assert.Equal(t, screenAgents, m.screen, "the panel stays where it was")
			assert.Len(t, m.listing.agents, tt.wantLen)

			if tt.wantLen == 0 {
				_, _ = m.updateAgents(tea.KeyMsg{Type: tea.KeyEnter})
				assert.Equal(t, screenAgents, m.screen)

				return
			}

			selected, ok := m.selectedAgent()
			require.True(t, ok, "the cursor is on an agent")
			assert.Equal(t, tt.wantCursor, selected.agent.Name)

			names := make([]string, 0, len(m.listing.agents))
			for _, agent := range m.listing.agents {
				names = append(names, agent.agent.Name)
			}
			assert.Equal(t, tt.wantOrder, names)
		})
	}
}

// TestPlaceTreeCursorSkipsHeaderAfterAgentRemoved pins that archiving the last agent under a
// workflow leaves the cursor off the next workflow's header, which only moved into that index.
func TestPlaceTreeCursorSkipsHeaderAfterAgentRemoved(t *testing.T) {
	m := model{
		screen: screenAgents,
		listing: listing{
			workflows: []workflowstore.Workflow{
				{ID: "w1", Name: "backend"},
				{ID: "w2", Name: "frontend"},
			},
			agents: []agentItem{
				{agent: agentstore.Agent{ID: "a1", Name: "a1", WorkflowID: "w1"}},
				{agent: agentstore.Agent{ID: "a2", Name: "a2", WorkflowID: "w2"}},
				{agent: agentstore.Agent{ID: "a3", Name: "a3", WorkflowID: "w2"}},
			},
		},
		tree: treeState{rowCursor: rowCursor{cursor: 1}, placed: true},
		keys: config.DefaultKeybindings(),
	}

	was := rowKeyAt(m.agentRows(), m.tree.cursor)
	require.Equal(t, "agent:a1", was)

	m.applyAgents([]agentItem{
		{agent: agentstore.Agent{ID: "a2", Name: "a2", WorkflowID: "w2"}},
		{agent: agentstore.Agent{ID: "a3", Name: "a3", WorkflowID: "w2"}},
	})
	m.placeTreeCursor(was)

	selected, ok := m.selectedAgent()
	require.True(t, ok, "the cursor lands on an agent, not the next workflow's header")
	assert.Equal(t, "a2", selected.agent.Name)
}

const testWorkflowID = "w1"

func items(names ...string) []agentItem {
	out := make([]agentItem, 0, len(names))
	for _, name := range names {
		out = append(out, agentItem{agent: agentstore.Agent{ID: name, Name: name, WorkflowID: testWorkflowID}})
	}

	return out
}

// TestNewModelWiresTmux pins the tmux dependency, since the model reaches for it only when a
// form opens with a blank directory and a missing one panics on a keypress rather than at
// startup.
func TestNewModelWiresTmux(t *testing.T) {
	tests := []struct {
		name string
		dir  string
		want string
	}{
		{
			name: "a blank directory is answered by the invoking client",
			want: clientPath,
		},
		{
			name: "a directory the caller gave is kept",
			dir:  "/work/api",
			want: "/work/api",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := &recordingTmux{path: clientPath}
			m := newModel(Deps{
				PopupWindow: NewPopupWindow(tmux, "manager"),
				Tmux:        tmux,
				Agents:      refusingAgents{},
				Workflows:   emptyLists{},
				Schedules:   &fakeSchedules{},
				Keys:        config.DefaultKeybindings(),
				Addr:        "127.0.0.1:8787",
			}, true)

			require.NotNil(t, m.tmux, "the panel cannot answer a blank directory without tmux")
			assert.Equal(t, tt.want, m.currentDir(tt.dir))
		})
	}
}

const clientPath = "/work/from-tmux"
