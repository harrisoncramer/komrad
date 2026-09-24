package ui

import (
	"strings"
	"testing"

	"weave/config"
	"weave/internal/ui/layout"
	"weave/internal/ui/paint"
	"weave/internal/ui/panel"
	"weave/lib/xslice"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func selectionTree() model {
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
				Name:       "api",
				WorkflowID: "w1",
			}},
			{Agent: panel.Agent{
				ID:         "a3",
				Name:       "db",
				WorkflowID: "w1",
			}},
			{Agent: panel.Agent{
				ID:         "a5",
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

// withColor forces a profile that writes escapes, which a selection needs to show its
// background.
func withColor(t *testing.T) {
	t.Helper()
}

func archiveSelectionTree() model {
	return model{
		frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenArchive,
		listing: listing{
			workflows: []panel.Workflow{{ID: "w1", Name: "backend"}},
			archivedAgents: archivedItems([]panel.Agent{
				{
					ID:         "a1",
					Name:       "auth",
					WorkflowID: "w1",
				},
				{
					ID:         "a2",
					Name:       "api",
					WorkflowID: "w1",
				},
				{
					ID:         "a3",
					Name:       "db",
					WorkflowID: "w1",
				},
			}),
		},
	}
}

func pressArchive(t *testing.T, m model, key tea.KeyPressMsg) model {
	t.Helper()

	next, _ := m.routeScreenMessage(key)
	updated, ok := next.(model)
	require.True(t, ok)

	return updated
}

// TestArchiveSelection covers the same gesture on the archive screen, which shares the
// component and so is only worth checking for being wired to its own rows and state.
func TestArchiveSelection(t *testing.T) {
	t.Run("an agent is taken in and the archive enters the mode", func(t *testing.T) {
		m := archiveSelectionTree()
		m.archive.cursor = 2

		after := pressArchive(t, m, keyMsg("v"))
		assert.Equal(t, []string{"a2"}, selectedIDs(after.archive.selected))
		assert.Empty(t, selectedIDs(after.agents.selected), "the agents tree is left alone")
	})

	t.Run("a header takes its whole folder", func(t *testing.T) {
		m := archiveSelectionTree()

		after := pressArchive(t, m, keyMsg("v"))
		assert.Equal(t, []string{"a1", "a2", "a3"}, selectedIDs(after.archive.selected))
	})

	t.Run("the range key reaches back to the nearest selection", func(t *testing.T) {
		m := archiveSelectionTree()
		m.archive.cursor = 3
		m.archive.selected = selectAgents([]string{"a1"}...)

		after := pressArchive(t, m, keyMsg("V"))
		assert.Equal(t, []string{"a1", "a2", "a3"}, selectedIDs(after.archive.selected))
	})

	t.Run("restore takes the whole selection rather than the hovered row", func(t *testing.T) {
		agents := &fakeArchiveAgents{}
		m := archiveSelectionTree()
		m.api = api{agents: agents}
		m.archive.cursor = 3
		m.archive.selected = selectAgents([]string{"a1", "a2"}...)

		next, cmd := m.routeScreenMessage(keyMsg("r"))
		require.NotNil(t, cmd)
		cmd()

		assert.Equal(t, []string{"a1", "a2"}, agents.restored, "the row under the cursor is not one of them")

		after, ok := next.(model)
		require.True(t, ok)
		assert.Equal(t, []string{"a1", "a2"}, selectedIDs(after.archive.selected), "the selection outlives a refusal")
	})

	t.Run("delete opens the prompt on the whole selection", func(t *testing.T) {
		agents := &fakeArchiveAgents{}
		m := archiveSelectionTree()
		m.api = api{
			agents:    agents,
			workflows: emptyLists{},
			schedules: emptyLists{},
		}
		m.archive.cursor = 3
		m.archive.selected = selectAgents([]string{"a1", "a2"}...)

		opened := pressArchive(t, m, keyMsg("d"))
		assert.Equal(t, formArchiveDelete, opened.form.kind)
		assert.Equal(t, screenArchive, opened.screen, "the prompt is drawn over the archive it was opened from")

		opened.form.form.State = huh.StateCompleted
		answered, cmd := opened.form.update(opened, keyMsg("enter"))
		settle(t, requireModel(t, answered), cmd)

		assert.Equal(t, []string{"a1", "a2"}, agents.deleted, "and it is the whole selection it acts on")
	})

	t.Run("a folder restore stands down, since a hovered header is not what was chosen", func(t *testing.T) {
		workflows := &fakeArchiveWorkflows{}
		m := archiveSelectionTree()
		m.api = api{agents: &fakeArchiveAgents{}, workflows: workflows}
		m.listing.workflows = nil
		m.listing.archivedWorkflows = []panel.Workflow{archivedWorkflow("w1", "backend")}
		m.archive.cursor = 0
		m.archive.selected = selectAgents([]string{"a1"}...)

		_, cmd := m.routeScreenMessage(keyMsg("r"))
		require.NotNil(t, cmd)
		cmd()

		assert.Empty(t, workflows.restored, "the folder under the cursor is left alone")
	})

	t.Run("esc clears the selection rather than leaving the screen", func(t *testing.T) {
		m := archiveSelectionTree()
		m.archive.selected = selectAgents([]string{"a1"}...)

		cleared := pressArchive(t, m, keyMsg("esc"))
		assert.Empty(t, selectedIDs(cleared.archive.selected))
		assert.Equal(t, screenArchive, cleared.screen)

		left := pressArchive(t, cleared, keyMsg("esc"))
		assert.Equal(t, screenAgents, left.screen, "and leaves once the mode is down")
	})

	t.Run("an agent gone from the listing drops", func(t *testing.T) {
		m := archiveSelectionTree()
		m.archive.selected = selectAgents([]string{"a1", "a3"}...)

		m.withListing(func(l *listing) {
			l.archivedAgents = archivedItems([]panel.Agent{{
				ID:         "a3",
				Name:       "db",
				WorkflowID: "w1",
			}})
		})

		assert.Equal(t, []string{"a3"}, selectedIDs(m.archive.selected))
	})

	t.Run("a chosen row takes the bar", func(t *testing.T) {
		withColor(t)

		m := archiveSelectionTree()
		m.width = rowPaneWidth

		rest := m.archive.view(m.shared())
		assert.NotContains(t, line(t, rest, "api"), paint.BackgroundSequence(paint.SelectionBackground))

		m.archive.selected = selectAgents([]string{"a2"}...)
		view := m.archive.view(m.shared())
		assert.Contains(t, line(t, view, "api"), paint.BackgroundSequence(paint.SelectionBackground))
		assert.NotContains(t, line(t, view, "db"), paint.BackgroundSequence(paint.SelectionBackground), "and only that row")
		assert.Equal(t, rowPaneWidth, lineWidth(t, view, "api"), "the bar spans the pane")
		assert.Equal(t, lineWidth(t, rest, "db"), lineWidth(t, view, "db"), "and the row below it is left as it was")
	})
}

// rowPaneWidth is a pane wide enough that a painted row is padded out rather than already
// filling the width it is drawn at.
const rowPaneWidth = 60

func lineWidth(t *testing.T, view, text string) int {
	t.Helper()

	return lipgloss.Width(line(t, view, text))
}

func line(t *testing.T, view, text string) string {
	t.Helper()

	for _, row := range strings.Split(view, "\n") {
		if strings.Contains(row, text) {
			return row
		}
	}

	t.Fatalf("no rendered row carries %q", text)

	return ""
}

// TestSelectionKeepsItsBarWhileAFormHasTheKeys covers the screen the selection exists for: a
// confirm stands beside the list it is about, taking its keys, and the rows it is about have to
// stay picked out while somebody reads the question.
func TestSelectionKeepsItsBarWhileAFormHasTheKeys(t *testing.T) {
	withColor(t)

	m := selectionTree()
	m.width = rowPaneWidth
	m.agents.selected = selectAgents([]string{"a2"}...)

	require.True(t, m.isListFocused(), "the list has the keys to begin with")
	require.Contains(t, line(t, m.agents.view(m.shared()), "api"), paint.BackgroundSequence(paint.SelectionBackground))

	form, _ := confirmForm("Archive 1 agent?", "", "Archive", config.FormKeys{})
	m.form.form = form

	require.False(t, m.isListFocused(), "and gives them up to the confirm")
	assert.Contains(t, line(t, m.agents.view(m.shared()), "api"), paint.BackgroundSequence(paint.SelectionBackground),
		"but the bar under it does not follow the keys")
}

// selectAgents is a selection holding the named agents, spelled by id as the tables read.
func selectAgents(ids ...string) selection {
	return selection{keys: xslice.Map(ids, agentRowKey)}
}

// selectedIDs is the agents a selection holds, by id.
func selectedIDs(sel selection) []string {
	if len(sel.keys) == 0 {
		return nil
	}

	return xslice.Map(sel.keys, func(key string) string { return strings.TrimPrefix(key, "agent:") })
}
