package ui

import (
	"strings"
	"testing"

	"weave/config"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
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

// TestHelpToggleOnSearch pins that a question mark typed into the search query is a character
// to search for rather than the key that draws the legend.
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

// TestHelpColumnsFollowTheWidth pins that a sidebar wide enough for two columns draws the
// same bindings in half the lines.
func TestHelpColumnsFollowTheWidth(t *testing.T) {
	narrow := helpModel()
	narrow.helpExpanded = true

	wide := narrow
	wide.width = 74

	bindings := byLabel(narrow.agentHelp().full)
	require.Len(t, helpColumns(bindings, narrow.width), 1, "a narrow sidebar holds one column")
	require.Len(t, helpColumns(bindings, wide.width), 2, "a wider one holds two")

	narrowHelp := ansi.Strip(narrow.renderHelp(narrow.agentHelp()))
	wideHelp := ansi.Strip(wide.renderHelp(wide.agentHelp()))

	assert.Equal(t, len(bindings), strings.Count(narrowHelp, "\n"), "one binding a line down the one column")
	assert.Less(t, strings.Count(wideHelp, "\n"), strings.Count(narrowHelp, "\n"), "two columns are shorter")
	for _, binding := range bindings {
		assert.Contains(t, wideHelp, binding.Help().Desc, "and drop nothing")
	}
}

func helpModel() model {
	return model{
		keys:    config.DefaultKeybindings(),
		height:  24,
		width:   46,
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "everything"}}, agents: []agentItem{{agent: agentstore.Agent{Name: "api-refactor", WorkflowID: "w1"}}}}, tree: treeState{rowCursor: rowCursor{cursor: 1}},
	}
}
