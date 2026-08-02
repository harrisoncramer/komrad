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
	assert.NotContains(t, view, "archive", "the help opens on its one line, dropping what the row also does")
	assert.Contains(t, view, "open", "it keeps what the row does first")
	assert.Contains(t, view, "show help", "and says the key that brings the rest out")

	next, _ := m.route(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	expanded, ok := next.(model)
	require.True(t, ok)
	require.True(t, expanded.helpExpanded)

	view = expanded.agentsView()
	assert.Contains(t, view, "archive", "the expanded help names every binding")
	assert.Contains(t, view, "hide help", "and says the key that puts it away")

	assert.Greater(t, m.listLines(m.agentsView()), expanded.listLines(expanded.agentsView()),
		"the rows the help takes are the list's")

	back, _ := expanded.route(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	collapsed, ok := back.(model)
	require.True(t, ok)
	assert.False(t, collapsed.helpExpanded, "the key toggles rather than only expanding")
}

// TestHelpToggleOnSearch covers the help key while the search screen's query line is open.
// Every key there goes into the query, so a question mark is a character to search transcripts
// for rather than the key that draws the legend.
func TestHelpToggleOnSearch(t *testing.T) {
	m := helpModel()
	m.screen = screenSearch
	m.search.typing = true

	next, _ := m.route(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	searched, ok := next.(model)
	require.True(t, ok)

	assert.False(t, searched.helpExpanded, "the help is left as it was")
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
