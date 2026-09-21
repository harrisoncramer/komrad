package ui

import (
	"strings"
	"testing"

	"weave/config"
	"weave/internal/ui/layout"
	"weave/internal/ui/panel"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAgentsViewRowWidthIsStableRegardlessOfBranch covers the fixed slot left where the
// branch glyph used to draw: an agent with a branch and one without must produce rows of
// the same width, so nothing jitters as the cwd syncer catches up.
func TestAgentsViewRowWidthIsStableRegardlessOfBranch(t *testing.T) {
	withBranch := model{
		frame: layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:  config.DefaultKeybindings(),
		listing: listing{workflows: []panel.Workflow{{ID: "w1", Name: "acmecorp"}}, agents: []agentItem{{agent: panel.Agent{
			ID:         "a1",
			Name:       "worker",
			WorkflowID: "w1",
			Branch:     xptr.Ptr("agent/a1"),
		}}}},
	}
	withoutBranch := withBranch
	withoutBranch.listing.agents = []agentItem{{agent: panel.Agent{
		ID:         "a1",
		Name:       "worker",
		WorkflowID: "w1",
	}}}

	rowWithBranch, ok := rowNamed(strings.Split(withBranch.agents.view(withBranch.shared()), "\n"), "worker")
	require.True(t, ok)
	assert.NotContains(t, rowWithBranch, "agent/a1", "the branch name itself is never drawn")

	rowWithoutBranch, ok := rowNamed(strings.Split(withoutBranch.agents.view(withoutBranch.shared()), "\n"), "worker")
	require.True(t, ok)

	assert.Len(t, rowWithoutBranch, len(rowWithBranch))
}

func rowNamed(rows []string, name string) (string, bool) {
	for _, row := range rows {
		if strings.Contains(row, name) {
			return row, true
		}
	}

	return "", false
}

// TestFocusMovesTheMarkers covers one cursor drawn at a time: the record draws none while the
// list has the keys, and the list gives its own up once the record has them.
func TestFocusMovesTheMarkers(t *testing.T) {
	m := detailsModel(&recordingTmux{})

	assert.NotContains(t, m.detailsView(), "›", "the record draws no cursor while the list has the keys")
	assert.Equal(t, 1, strings.Count(m.agents.view(m.shared()), "›"), "the list's is the one on screen")

	m.details.focused = true

	assert.Contains(t, m.detailsView(), "›", "the field cursor arrives with the keys")
	assert.Equal(t, 1, strings.Count(m.agents.view(m.shared()), "›"), "and the row cursor goes")
}
