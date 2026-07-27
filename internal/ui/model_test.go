package ui

import (
	"testing"

	"claude-agents/internal/appconfig"

	agentstore "claude-agents/internal/store/agent"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

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

// TestApplyAgents covers what a refresh does to the on-screen list. It keeps the
// cursor on the agent it was on and does not reorder surviving rows, since
// reordering under the cursor defeats an index-based selection.
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
			fresh:      items("c", "b", "a"),
			wantLen:    3,
			wantCursor: "b",
			wantOrder:  []string{"a", "b", "c"},
		},
		{
			name:       "a new agent is appended rather than sorted in",
			start:      items("a"),
			cursor:     0,
			fresh:      items("b", "a"),
			wantLen:    2,
			wantCursor: "a",
			wantOrder:  []string{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{
				screen:      screenAgents,
				agents:      tt.start,
				agentCursor: tt.cursor,
				keys:        appconfig.DefaultKeybindings(),
			}

			m.applyAgents(tt.fresh)

			assert.Equal(t, screenAgents, m.screen, "the panel stays where it was")
			assert.Len(t, m.agents, tt.wantLen)

			if tt.wantLen == 0 {
				// Acting on the empty list must be a safe no-op rather than an index
				// panic, since the handlers resolve a selection through selectedAgent.
				_, _ = m.updateAgents(tea.KeyMsg{Type: tea.KeyEnter})
				assert.Equal(t, screenAgents, m.screen)

				return
			}

			assert.Equal(t, tt.wantCursor, m.agents[m.agentCursor].agent.Name)

			names := make([]string, 0, len(m.agents))
			for _, agent := range m.agents {
				names = append(names, agent.agent.Name)
			}
			assert.Equal(t, tt.wantOrder, names)
		})
	}
}

// items builds a list of agents from names alone, for the cases that only care
// about ordering and selection.
func items(names ...string) []agentItem {
	out := make([]agentItem, 0, len(names))
	for _, name := range names {
		out = append(out, agentItem{agent: agentstore.Agent{Name: name}})
	}

	return out
}
