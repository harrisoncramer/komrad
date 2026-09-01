package ui

import (
	"context"
	"strings"
	"testing"

	"weave/config"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

func selectionTree() model {
	return model{
		frame: layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:  config.DefaultKeybindings(),
		listing: listing{workflows: []workflowstore.Workflow{
			{ID: "w1", Name: "backend"},
			{ID: "w2", Name: "frontend"},
		}, agents: []agentItem{
			{agent: agentstore.Agent{
				ID:         "a1",
				Name:       "auth",
				WorkflowID: "w1",
			}},
			{agent: agentstore.Agent{
				ID:         "a2",
				Name:       "api",
				WorkflowID: "w1",
			}},
			{agent: agentstore.Agent{
				ID:         "a3",
				Name:       "db",
				WorkflowID: "w1",
			}},
			{agent: agentstore.Agent{
				ID:         "a5",
				Name:       "sweep",
				WorkflowID: "w1",
				Hidden:     true,
			}},
			{agent: agentstore.Agent{
				ID:         "a4",
				Name:       "ui",
				WorkflowID: "w2",
			}},
		}},
	}
}

// withColor forces a profile that writes escapes, since a selection now shows in the colour of
// the name and nothing renders one under the bare profile a test binary reports.
func withColor(t *testing.T) {
	t.Helper()

	restore := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })
	lipgloss.SetColorProfile(termenv.TrueColor)
}

func press(t *testing.T, m model, key tea.KeyMsg) model {
	t.Helper()

	next, _ := m.updateAgents(key)
	updated, ok := next.(model)
	require.True(t, ok)

	return updated
}

// TestToggleSelection covers what the select key does from each kind of row, where a header
// acts as a checkbox over the whole folder.
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

// TestToggleSelectionEmptyFolder covers a folder with no rows under it, which would otherwise
// read as wholly selected and put the panel in a mode holding nothing.
func TestToggleSelectionEmptyFolder(t *testing.T) {
	m := selectionTree()
	m.listing.workflows = append(m.listing.workflows, workflowstore.Workflow{ID: "w3", Name: "chores"})
	m.tree.cursor = len(m.agentRows()) - 1

	after := press(t, m, keyMsg("v"))
	assert.Empty(t, after.tree.selected.ids)
	assert.False(t, after.tree.selected.active())
}

// TestSelectRange covers the key that takes in everything between the cursor and the nearest
// selection, which from a list with nothing selected acts as the plain select key.
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

// TestSelectionKeysStandDown covers a selection being up, where the keys with nothing to say
// about a set of agents do nothing and the clear key is the way out.
func TestSelectionKeysStandDown(t *testing.T) {
	m := selectionTree()
	m.tree.cursor = 1
	m.tree.selected = selection{ids: []string{"a1"}}

	renamed := press(t, m, keyMsg("r"))
	assert.Equal(t, screenAgents, renamed.screen, "rename does not open its form")
	assert.False(t, renamed.form.active())

	moved := press(t, m, keyMsg("j"))
	assert.Equal(t, 2, moved.tree.cursor, "the cursor still moves")

	cleared := press(t, m, keyMsg("esc"))
	assert.Empty(t, cleared.tree.selected.ids)
	assert.False(t, cleared.tree.selected.active())
}

// TestPruneSelection covers a selected agent losing its row, which would otherwise hold the
// panel in multi-select mode over something nobody can see.
func TestPruneSelection(t *testing.T) {
	t.Run("an agent gone from the listing drops", func(t *testing.T) {
		m := selectionTree()
		m.tree.placed = true
		m.tree.selected = selection{ids: []string{"a1", "a4"}}

		m.applyAgents([]agentItem{{agent: agentstore.Agent{
			ID:         "a4",
			Name:       "ui",
			WorkflowID: "w2",
		}}})
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

func archiveSelectionTree() model {
	return model{
		frame:   layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:    config.DefaultKeybindings(),
		screen:  screenArchive,
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}}},
		archive: archiveState{
			agents: []agentstore.Agent{
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
			},
		},
	}
}

func pressArchive(t *testing.T, m model, key tea.KeyMsg) model {
	t.Helper()

	next, _ := m.updateArchive(key)
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
		agents := &fakeArchiveAgents{}
		m := archiveSelectionTree()
		m.api = api{agents: agents}
		m.archive.cursor = 3
		m.archive.selected = selection{ids: []string{"a1", "a2"}}

		opened := pressArchive(t, m, keyMsg("d"))
		assert.Equal(t, formArchiveDelete, opened.form.kind)
		assert.Equal(t, screenArchive, opened.screen, "the prompt is drawn over the archive it was opened from")

		opened.form.form.State = huh.StateCompleted
		answered, cmd := opened.form.update(opened, keyMsg("enter"))
		settle(t, asModel(t, answered), cmd)

		assert.Equal(t, []string{"a1", "a2"}, agents.deleted, "and it is the whole selection it acts on")
	})

	t.Run("a folder restore stands down, since a hovered header is not what was chosen", func(t *testing.T) {
		workflows := &fakeArchiveWorkflows{}
		m := archiveSelectionTree()
		m.api = api{agents: &fakeArchiveAgents{}, workflows: workflows}
		m.listing.workflows = nil
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
			[]agentstore.Agent{{
				ID:         "a3",
				Name:       "db",
				WorkflowID: "w1",
			}},
			nil,
			"",
		)

		assert.Equal(t, []string{"a3"}, m.archive.selected.ids)
	})

	t.Run("a chosen row turns without shifting the rows", func(t *testing.T) {
		withColor(t)

		m := archiveSelectionTree()

		rest := m.archiveView()
		assert.NotContains(t, rest, selectionStyle.Render("api"))

		m.archive.selected = selection{ids: []string{"a2"}}
		view := m.archiveView()
		assert.Contains(t, view, selectionStyle.Render("api"))
		assert.NotContains(t, view, selectionStyle.Render("db"), "and only that row")
		assert.Equal(t, lineWidth(t, rest, "api"), lineWidth(t, view, "api"), "the chosen row keeps its width")
		assert.Equal(t, lineWidth(t, rest, "db"), lineWidth(t, view, "db"), "and so does the one below it")
	})
}

// TestAgentsViewSelectionColour covers how a row taken into a selection is drawn, which is its
// own name in the selection colour, so that taking a row in shifts nothing around it.
func TestAgentsViewSelectionColour(t *testing.T) {
	withColor(t)

	m := selectionTree()

	rest := m.agentsView()
	assert.NotContains(t, rest, selectionStyle.Render("api"))

	m.tree.selected = selection{ids: []string{"a2"}}
	view := m.agentsView()
	assert.Contains(t, view, selectionStyle.Render("api"), "the selected row turns")
	assert.NotContains(t, view, selectionStyle.Render("db"), "and only that row")
	assert.Equal(t, lineWidth(t, rest, "api"), lineWidth(t, view, "api"), "the chosen row keeps its width")
	assert.Equal(t, lineWidth(t, rest, "db"), lineWidth(t, view, "db"), "and so does the one below it")
}

// TestSelectionBeatsTheCursor pins that the row under the cursor still shows whether it is in,
// since the cursor colour would otherwise hide the one row a person is toggling.
func TestSelectionBeatsTheCursor(t *testing.T) {
	withColor(t)

	m := selectionTree()
	m.tree.selected = selection{ids: []string{"a2"}}

	for i, r := range m.agentRows() {
		if !r.header && r.item.agent.ID == "a2" {
			m.tree.cursor = i
		}
	}

	view := m.agentsView()
	assert.Contains(t, view, selectionStyle.Render("api"))
	assert.NotContains(t, view, m.rowCursorStyle().Render("api"), "the cursor does not take the name back")
}

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

// TestArchiveSelectionFromTheTree covers the archive key over a selection, which only opens
// the confirm prompt and archives nothing until that prompt is answered yes.
func TestArchiveSelectionFromTheTree(t *testing.T) {
	tests := []struct {
		name     string
		selected []string
		cursor   int
		wantIDs  []string
	}{
		{
			name:    "with nothing selected the key still takes the hovered row",
			cursor:  1,
			wantIDs: []string{"a1"},
		},
		{
			name:     "a selection is archived whole rather than the row under the cursor",
			selected: []string{"a1", "a3"},
			cursor:   2,
			wantIDs:  []string{"a1", "a3"},
		},
		{
			name:     "a selection spanning folders goes in one call",
			selected: []string{"a1", "a4"},
			cursor:   1,
			wantIDs:  []string{"a1", "a4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agents := &fakeTreeAgents{}
			stage := &recordingTmux{}

			m := selectionTree()
			m.frame = layout.New(stage, config.DefaultLayout())
			m.api = api{agents: agents}
			m.tree.cursor = tt.cursor
			m.tree.selected = selection{ids: tt.selected}

			for i := range m.listing.agents {
				m.listing.agents[i].agent.Pane = "%" + m.listing.agents[i].agent.ID
			}

			opened, _ := m.updateAgents(keyMsg("z"))
			pressed, ok := opened.(model)
			require.True(t, ok)
			assert.Equal(t, formAgentArchive, pressed.form.kind, "the key opens the prompt rather than archiving outright")
			assert.Empty(t, agents.archived, "nothing is sent until the prompt is confirmed")

			pressed.form.form.State = huh.StateCompleted

			next, cmd := pressed.form.update(pressed, keyMsg("enter"))
			require.NotNil(t, cmd)
			cmd()

			assert.Equal(t, tt.wantIDs, agents.archived)

			after, ok := next.(model)
			require.True(t, ok)
			assert.Equal(t, tt.selected, after.tree.selected.ids,
				"the rows come off the selection on the next listing rather than on the keypress")
		})
	}
}

type fakeTreeAgents struct {
	agentv1connect.AgentServiceClient
	archived []string
}

func (f *fakeTreeAgents) ArchiveAgents(_ context.Context, req *connect.Request[agentv1.ArchiveAgentsRequest]) (*connect.Response[emptypb.Empty], error) {
	f.archived = append(f.archived, req.Msg.GetIds()...)

	return connect.NewResponse(&emptypb.Empty{}), nil
}
