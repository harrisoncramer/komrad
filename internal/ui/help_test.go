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

// TestHelpToggle covers the help key, which is the one binding that changes nothing but
// how much of the help is drawn.
func TestHelpToggle(t *testing.T) {
	m := helpModel()

	view := m.agentsView()
	assert.Contains(t, view, "archive", "the help opens naming every binding")
	assert.Contains(t, view, "hide help", "and says the key that puts it away")

	next, _ := m.route(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	collapsed, ok := next.(model)
	require.True(t, ok)
	require.True(t, collapsed.helpCollapsed)

	view = collapsed.agentsView()
	assert.NotContains(t, view, "archive", "the collapsed help drops what the row also does")
	assert.Contains(t, view, "open", "it keeps what the row does first")
	assert.Contains(t, view, "show help", "and says the key that brings the rest back")

	assert.Greater(t, collapsed.listLines(collapsed.agentsView()), m.listLines(m.agentsView()),
		"the rows the help gives up are the list's")

	back, _ := collapsed.route(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	expanded, ok := back.(model)
	require.True(t, ok)
	assert.False(t, expanded.helpCollapsed, "the key toggles rather than only collapsing")
}

// TestHelpToggleOnSearch covers the one screen the help key is not a binding on. Every key
// on the search screen goes into the query, so a question mark there is a character to
// search transcripts for.
func TestHelpToggleOnSearch(t *testing.T) {
	m := helpModel()
	m.screen = screenSearch

	next, _ := m.route(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	searched, ok := next.(model)
	require.True(t, ok)

	assert.False(t, searched.helpCollapsed, "the help is left as it was")
	assert.Equal(t, "?", searched.search.query, "the key is typed instead")
}

// helpModel is a panel hovering an agent, which is the row whose help has the most on it.
func helpModel() model {
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
