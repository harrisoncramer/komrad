package ui

import (
	"testing"

	"weave/internal/appconfig"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHintsToggle covers the help key, which is the one binding that changes nothing but
// how much of the legend is drawn.
func TestHintsToggle(t *testing.T) {
	m := hintsModel()

	view := m.agentsView()
	assert.Contains(t, view, "archive", "the legend opens naming every binding")
	assert.Contains(t, view, "hide help", "and says the key that puts it away")

	next, _ := m.route(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	collapsed, ok := next.(model)
	require.True(t, ok)
	require.True(t, collapsed.hintsCollapsed)

	view = collapsed.agentsView()
	assert.NotContains(t, view, "archive", "the collapsed legend drops what the row also does")
	assert.Contains(t, view, "open", "it keeps what the row does first")
	assert.Contains(t, view, "show help", "and says the key that brings the rest back")

	assert.Greater(t, collapsed.listLines(collapsed.agentsView()), m.listLines(m.agentsView()),
		"the rows the legend gives up are the list's")

	back, _ := collapsed.route(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	expanded, ok := back.(model)
	require.True(t, ok)
	assert.False(t, expanded.hintsCollapsed, "the key toggles rather than only collapsing")
}

// TestHintsToggleOnSearch covers the one screen the help key is not a binding on. Every key
// on the search screen goes into the query, so a question mark there is a character to
// search transcripts for.
func TestHintsToggleOnSearch(t *testing.T) {
	m := hintsModel()
	m.screen = screenSearch

	next, _ := m.route(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	searched, ok := next.(model)
	require.True(t, ok)

	assert.False(t, searched.hintsCollapsed, "the legend is left as it was")
	assert.Equal(t, "?", searched.search.query, "the key is typed instead")
}

// hintsModel is a panel hovering an agent, which is the row whose legend has the most on it.
func hintsModel() model {
	return model{
		keys:   appconfig.DefaultKeybindings(),
		height: 24,
		width:  46,
		tree: treeState{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "everything"}},
			agents:    []agentItem{{agent: agentstore.Agent{Name: "api-refactor", WorkflowID: "w1"}}},
			cursor:    1,
		},
	}
}
