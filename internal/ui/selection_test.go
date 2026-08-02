package ui

import (
	"strings"
	"testing"

	"weave/internal/appconfig"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// selectionTree is the list the cases below work over, whose rows are 0 backend, 1 auth,
// 2 api, 3 db, 4 frontend, 5 ui. backend holds a hidden agent as well, which takes no row
// while the hidden ones are away and so is not the folder's to select.
func selectionTree() model {
	return model{
		stage: NewStage(&recordingTmux{}, appconfig.DefaultSplit),
		keys:  appconfig.DefaultKeybindings(),
		tree: treeState{
			workflows: []workflowstore.Workflow{
				{ID: "w1", Name: "backend"},
				{ID: "w2", Name: "frontend"},
			},
			agents: []agentItem{
				{agent: agentstore.Agent{ID: "a1", Name: "auth", WorkflowID: "w1"}},
				{agent: agentstore.Agent{ID: "a2", Name: "api", WorkflowID: "w1"}},
				{agent: agentstore.Agent{ID: "a3", Name: "db", WorkflowID: "w1"}},
				{agent: agentstore.Agent{ID: "a5", Name: "sweep", WorkflowID: "w1", Hidden: true}},
				{agent: agentstore.Agent{ID: "a4", Name: "ui", WorkflowID: "w2"}},
			},
		},
	}
}

// press sends a key to the agents screen and hands back the model it left behind.
func press(t *testing.T, m model, key tea.KeyMsg) model {
	t.Helper()

	next, _ := m.updateAgents(key)
	updated, ok := next.(model)
	require.True(t, ok)

	return updated
}

// TestToggleSelection covers what the select key does from each kind of row. An agent is a
// plain toggle, and a header is a checkbox over the folder: not wholly selected takes the
// whole of it, wholly selected drops it.
func TestToggleSelection(t *testing.T) {
	tests := []struct {
		name   string
		cursor int
		start  []string
		want   []string
	}{
		{
			name:   "an unselected agent is taken in",
			cursor: 2,
			want:   []string{"a2"},
		},
		{
			name:   "and a selected one drops",
			cursor: 2,
			start:  []string{"a1", "a2"},
			want:   []string{"a1"},
		},
		{
			name:   "a header takes its whole folder, less the hidden agent with no row",
			cursor: 0,
			want:   []string{"a1", "a2", "a3"},
		},
		{
			name:   "a partly selected folder is filled in rather than emptied",
			cursor: 0,
			start:  []string{"a2"},
			want:   []string{"a1", "a2", "a3"},
		},
		{
			name:   "a wholly selected folder drops, leaving what is outside it alone",
			cursor: 0,
			start:  []string{"a1", "a2", "a3", "a4"},
			want:   []string{"a4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := selectionTree()
			m.tree.cursor = tt.cursor
			m.tree.selected = selection{ids: tt.start}

			assert.Equal(t, tt.want, press(t, m, keyMsg("v")).tree.selected.ids)
		})
	}
}

// TestToggleSelectionEmptyFolder covers the folder with no rows under it. Selecting it would
// read as wholly selected and put the panel in a mode holding nothing.
func TestToggleSelectionEmptyFolder(t *testing.T) {
	m := selectionTree()
	m.tree.workflows = append(m.tree.workflows, workflowstore.Workflow{ID: "w3", Name: "chores"})
	m.tree.cursor = len(m.agentRows()) - 1

	after := press(t, m, keyMsg("v"))
	assert.Empty(t, after.tree.selected.ids)
	assert.False(t, after.tree.selected.active())
}

// TestSelectRange covers the key that takes in everything between the cursor and the
// selection nearest it, which is how a run is chosen without pressing select down it. From
// a list with nothing selected it is the select key, since a range has nothing to reach
// back to until the mode is up.
func TestSelectRange(t *testing.T) {
	tests := []struct {
		name   string
		cursor int
		start  []string
		want   []string
	}{
		{
			name:   "from a list at rest it opens the mode on the agent under the cursor",
			cursor: 3,
			want:   []string{"a3"},
		},
		{
			name:   "and on a header it takes the folder, the way the select key does",
			cursor: 0,
			want:   []string{"a1", "a2", "a3"},
		},
		{
			name:   "a header takes no range once the mode is up, since only agents are selectable",
			cursor: 0,
			start:  []string{"a3"},
			want:   []string{"a3"},
		},
		{
			name:   "a selection above reaches down to the cursor",
			cursor: 3,
			start:  []string{"a1"},
			want:   []string{"a1", "a2", "a3"},
		},
		{
			name:   "a selection below reaches up to it",
			cursor: 1,
			start:  []string{"a3"},
			want:   []string{"a1", "a2", "a3"},
		},
		{
			name:   "a header inside the range is passed over rather than ending it",
			cursor: 5,
			start:  []string{"a3"},
			want:   []string{"a3", "a4"},
		},
		{
			name:   "a cursor the same distance from a selection either side takes the one above",
			cursor: 2,
			start:  []string{"a1", "a3"},
			want:   []string{"a1", "a2", "a3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := selectionTree()
			m.tree.cursor = tt.cursor
			m.tree.selected = selection{ids: tt.start}

			assert.Equal(t, tt.want, press(t, m, keyMsg("V")).tree.selected.ids)
		})
	}
}

// TestSelectionKeysStandDown covers the mode itself. While a selection is up the keys that
// act on one agent do nothing, since nothing acts on a selection yet, and the clear key is
// the way back out.
func TestSelectionKeysStandDown(t *testing.T) {
	m := selectionTree()
	m.tree.cursor = 1
	m.tree.selected = selection{ids: []string{"a1"}}

	renamed := press(t, m, keyMsg("r"))
	assert.Equal(t, screenAgents, renamed.screen, "rename does not open its form")
	assert.Nil(t, renamed.form)

	moved := press(t, m, keyMsg("j"))
	assert.Equal(t, 2, moved.tree.cursor, "the cursor still moves")

	cleared := press(t, m, keyMsg("esc"))
	assert.Empty(t, cleared.tree.selected.ids)
	assert.False(t, cleared.tree.selected.active())
}

// TestPruneSelection covers a selected agent losing its row. Held on it would keep the panel
// in multi-select mode over something nobody can see, so a listing that drops an agent and
// the hidden toggle that puts one away both drop it from the selection.
func TestPruneSelection(t *testing.T) {
	t.Run("an agent gone from the listing drops", func(t *testing.T) {
		m := selectionTree()
		m.tree.placed = true
		m.tree.selected = selection{ids: []string{"a1", "a4"}}

		m.applyAgents([]agentItem{{agent: agentstore.Agent{ID: "a4", Name: "ui", WorkflowID: "w2"}}})
		m.placeTreeCursor("")

		assert.Equal(t, []string{"a4"}, m.tree.selected.ids)
	})

	t.Run("so does one the hidden toggle puts away", func(t *testing.T) {
		m := selectionTree()
		m.tree.showHidden = true
		m.tree.selected = selection{ids: []string{"a1", "a5"}}

		away := press(t, m, keyMsg("H"))
		assert.Equal(t, []string{"a1"}, away.tree.selected.ids)
	})
}

// archiveSelectionTree is the archive the cases below work over, one live folder holding
// three agents put away out of it. Its rows are the header at 0 and the agents at 1 to 3.
func archiveSelectionTree() model {
	return model{
		stage:  NewStage(&recordingTmux{}, appconfig.DefaultSplit),
		keys:   appconfig.DefaultKeybindings(),
		screen: screenArchive,
		tree:   treeState{workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}}},
		archive: archiveState{
			agents: []agentstore.Agent{
				{ID: "a1", Name: "auth", WorkflowID: "w1"},
				{ID: "a2", Name: "api", WorkflowID: "w1"},
				{ID: "a3", Name: "db", WorkflowID: "w1"},
			},
		},
	}
}

// pressArchive sends a key to the archive and hands back the model it left behind.
func pressArchive(t *testing.T, m model, key tea.KeyMsg) model {
	t.Helper()

	next, _ := m.updateArchive(key)
	updated, ok := next.(model)
	require.True(t, ok)

	return updated
}

// TestArchiveSelection covers the same gesture on the other tree. The archive runs the
// selection through the same component, so what is worth checking here is that it is wired
// to the archive's own rows and its own state rather than the agents tree's.
func TestArchiveSelection(t *testing.T) {
	t.Run("an agent is taken in and the archive enters the mode", func(t *testing.T) {
		m := archiveSelectionTree()
		m.archive.cursor = 2

		after := pressArchive(t, m, keyMsg("v"))
		assert.Equal(t, []string{"a2"}, after.archive.selected.ids)
		assert.Empty(t, after.tree.selected.ids, "the agents tree is left alone")
	})

	t.Run("a header takes its whole folder", func(t *testing.T) {
		m := archiveSelectionTree()

		after := pressArchive(t, m, keyMsg("v"))
		assert.Equal(t, []string{"a1", "a2", "a3"}, after.archive.selected.ids)
	})

	t.Run("the range key reaches back to the nearest selection", func(t *testing.T) {
		m := archiveSelectionTree()
		m.archive.cursor = 3
		m.archive.selected = selection{ids: []string{"a1"}}

		after := pressArchive(t, m, keyMsg("V"))
		assert.Equal(t, []string{"a1", "a2", "a3"}, after.archive.selected.ids)
	})

	t.Run("restore takes the whole selection rather than the hovered row", func(t *testing.T) {
		agents := &fakeArchiveAgents{}
		m := archiveSelectionTree()
		m.api = api{agents: agents}
		m.archive.cursor = 3
		m.archive.selected = selection{ids: []string{"a1", "a2"}}

		next, cmd := m.updateArchive(keyMsg("r"))
		require.NotNil(t, cmd)
		cmd()

		assert.Equal(t, []string{"a1", "a2"}, agents.restored, "the row under the cursor is not one of them")

		after, ok := next.(model)
		require.True(t, ok)
		assert.Equal(t, []string{"a1", "a2"}, after.archive.selected.ids, "the selection outlives a refusal")
	})

	t.Run("delete opens the prompt on the whole selection", func(t *testing.T) {
		m := archiveSelectionTree()
		m.archive.cursor = 3
		m.archive.selected = selection{ids: []string{"a1", "a2"}}

		opened := pressArchive(t, m, keyMsg("d"))
		assert.Equal(t, screenArchiveDelete, opened.screen)
		assert.Equal(t, []string{"a1", "a2"}, agentIDs(opened.target.deleteAgents))
	})

	t.Run("a folder restore stands down, since a hovered header is not what was chosen", func(t *testing.T) {
		workflows := &fakeArchiveWorkflows{}
		m := archiveSelectionTree()
		m.api = api{agents: &fakeArchiveAgents{}, workflows: workflows}
		m.tree.workflows = nil
		m.archive.workflows = []workflowstore.Workflow{archivedWorkflow("w1", "backend")}
		m.archive.cursor = 0
		m.archive.selected = selection{ids: []string{"a1"}}

		_, cmd := m.updateArchive(keyMsg("r"))
		require.NotNil(t, cmd)
		cmd()

		assert.Empty(t, workflows.restored, "the folder under the cursor is left alone")
	})

	t.Run("esc clears the selection rather than leaving the screen", func(t *testing.T) {
		m := archiveSelectionTree()
		m.archive.selected = selection{ids: []string{"a1"}}

		cleared := pressArchive(t, m, keyMsg("esc"))
		assert.Empty(t, cleared.archive.selected.ids)
		assert.Equal(t, screenArchive, cleared.screen)

		left := pressArchive(t, cleared, keyMsg("esc"))
		assert.Equal(t, screenAgents, left.screen, "and leaves once the mode is down")
	})

	t.Run("an agent gone from the listing drops", func(t *testing.T) {
		m := archiveSelectionTree()
		m.archive.selected = selection{ids: []string{"a1", "a3"}}

		m.applyArchive(
			[]agentstore.Agent{{ID: "a3", Name: "db", WorkflowID: "w1"}},
			nil,
			"",
		)

		assert.Equal(t, []string{"a3"}, m.archive.selected.ids)
	})

	t.Run("the gutter is drawn without shifting the rows", func(t *testing.T) {
		m := archiveSelectionTree()

		rest := m.archiveView()
		assert.NotContains(t, rest, selectionIcon)

		m.archive.selected = selection{ids: []string{"a2"}}
		view := m.archiveView()
		assert.Equal(t, 1, countRows(view, selectionIcon))
		assert.Equal(t, lineWidth(t, rest, "api"), lineWidth(t, view, "api"), "the chosen row keeps its width")
		assert.Equal(t, lineWidth(t, rest, "db"), lineWidth(t, view, "db"), "and so does the one below it")
	})
}

// TestAgentsViewGutter covers the column down the left of the tree. It is drawn on every
// row whether or not anything is selected, and the bar goes into the space a blank would
// have taken, so taking a row in cannot shift the rows around it.
func TestAgentsViewGutter(t *testing.T) {
	m := selectionTree()

	rest := m.agentsView()
	assert.NotContains(t, rest, selectionIcon)

	m.tree.selected = selection{ids: []string{"a2"}}
	view := m.agentsView()
	assert.Equal(t, 1, countRows(view, selectionIcon), "only the selected row carries the bar")
	assert.Equal(t, lineWidth(t, rest, "api"), lineWidth(t, view, "api"), "the chosen row keeps its width")
	assert.Equal(t, lineWidth(t, rest, "db"), lineWidth(t, view, "db"), "and so does the one below it")
}

// lineWidth is the printable width of the rendered line carrying the text, which is what a
// gutter opening beside it would change. It reads one named row rather than the whole view,
// since the legend under the rows does differ between the two modes.
func lineWidth(t *testing.T, view, text string) int {
	t.Helper()

	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, text) {
			return lipgloss.Width(line)
		}
	}

	t.Fatalf("no rendered row carries %q", text)

	return 0
}

// countRows is how many of a rendered view's lines carry the marker.
func countRows(view, marker string) int {
	n := 0
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, marker) {
			n++
		}
	}

	return n
}
