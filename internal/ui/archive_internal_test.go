package ui

import (
	"context"
	"testing"
	"time"

	"weave/config"
	"weave/internal/ui/layout"
	"weave/internal/ui/panel"
	"weave/internal/view"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	workflowv1 "weave/proto/gen/workflow/v1"
	"weave/proto/gen/workflow/v1/workflowv1connect"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

type fakeArchiveWorkflows struct {
	workflowv1connect.WorkflowServiceClient
	restored []string
}

type fakeArchiveAgents struct {
	agentv1connect.AgentServiceClient
	restored []string
	deleted  []string
}

func (f *fakeArchiveWorkflows) RestoreWorkflow(_ context.Context, req *connect.Request[workflowv1.RestoreWorkflowRequest]) (*connect.Response[workflowv1.RestoreWorkflowResponse], error) {
	f.restored = append(f.restored, req.Msg.GetId())

	return connect.NewResponse(&workflowv1.RestoreWorkflowResponse{}), nil
}

func (f *fakeArchiveAgents) RestoreAgents(_ context.Context, req *connect.Request[agentv1.RestoreAgentsRequest]) (*connect.Response[agentv1.RestoreAgentsResponse], error) {
	f.restored = append(f.restored, req.Msg.GetIds()...)

	response := &agentv1.RestoreAgentsResponse{Agents: xslice.Map(req.Msg.GetIds(), func(id string) *agentv1.Agent {
		return &agentv1.Agent{Id: id, Name: "api-refactor"}
	})}

	return connect.NewResponse(response), nil
}

func (f *fakeArchiveAgents) DeleteAgents(_ context.Context, req *connect.Request[agentv1.DeleteAgentsRequest]) (*connect.Response[emptypb.Empty], error) {
	f.deleted = append(f.deleted, req.Msg.GetIds()...)

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// TestArchiveHandsTheWindowBackWhenAWriteLands covers a write landing while the archive holds the window.
func TestArchiveHandsTheWindowBackWhenAWriteLands(t *testing.T) {
	m := splitArchiveModel(t)
	require.True(t, m.frame.IsSolo(), "the archive holds the window to begin with")

	landed, _ := m.applyWrite(writeResult{written: &written{row: agentRowKey("a1"), pane: "%9"}})
	got, ok := landed.(model)
	require.True(t, ok)

	assert.Equal(t, screenAgents, got.screen, "the write puts the reader on the row it wrote")
	assert.False(t, got.frame.IsSolo(), "and the pane it made is not left under the archive's own zoom")
}

// TestArchiveGreysWhatIsPutAway covers greying the rows that were put away.
func TestArchiveGreysWhatIsPutAway(t *testing.T) {
	m := archiveModel(&fakeArchiveAgents{}, &fakeArchiveWorkflows{}, config.DefaultKeybindings())
	m.listing.workflows = []panel.Workflow{{ID: "w1", Name: "backend"}}
	m.listing.archivedAgents = archivedItems([]panel.Agent{archivedAgent("a1", "auth", "w1"), archivedAgent("a2", "ui", "w2")})
	m.listing.archivedWorkflows = []panel.Workflow{archivedWorkflow("w2", "frontend")}
	// Parked on the last row, where the cursor's own colour beats greying.
	m.archive.cursor = 3

	lines := archiveLines(t, m)
	require.Len(t, lines, 4)

	assert.Equal(t, "backend", lines[0].Name)
	assert.Equal(t, view.EmphasisNormal, lines[0].Emphasis, "a live folder is not put away")
	assert.Equal(t, view.WeightBold, lines[0].Weight, "and it is still a folder")

	assert.Equal(t, "auth", lines[1].Name)
	assert.Equal(t, view.EmphasisDim, lines[1].Emphasis, "an archived agent is greyed")

	assert.Equal(t, "frontend", lines[2].Name)
	assert.Equal(t, view.EmphasisDim, lines[2].Emphasis, "an archived folder is greyed too")
	assert.Equal(t, view.WeightBold, lines[2].Weight)

	assert.NotEqual(t, lines[0].Emphasis, lines[2].Emphasis,
		"which is what tells the folder restore acts on from the one it does not")
}

// TestArchiveDrawsTheSameColumns pins the archive drawing every column the agents list draws.
func TestArchiveDrawsTheSameColumns(t *testing.T) {
	m := archiveModel(&fakeArchiveAgents{}, &fakeArchiveWorkflows{}, config.DefaultKeybindings())

	rows := m.archive.listRows(m.shared())
	require.NotEmpty(t, rows)

	live := view.Prepare(agentsSnapshot(snapshotParams{rows: m.agents.rows(m.shared()), pane: 200}))
	assert.Equal(t, live.Columns, view.Prepare(m.archive.snapshot(m.shared(), listPane{width: 200})).Columns)
}

// TestArchiveDrawsTheQuietBarExceptWhereADeleteFailed covers the archive's one state cell.
func TestArchiveDrawsTheQuietBarExceptWhereADeleteFailed(t *testing.T) {
	m := archiveModel(&fakeArchiveAgents{}, &fakeArchiveWorkflows{}, config.DefaultKeybindings())
	broken := archivedAgent("a2", "stuck", "w1")
	broken.State = panel.StateFailedToCleanup
	m.listing.archivedAgents = archivedItems([]panel.Agent{archivedAgent("a1", "auth", "w1"), broken})

	lines := archiveLines(t, m)
	require.Len(t, lines, 3)

	assert.Equal(t, view.GlyphQuiet, lines[1].Status.Glyph, "an agent merely put away is quiet")
	assert.Equal(t, view.GlyphBusy, lines[2].Status.Glyph, "and one a delete could not destroy says so")
}

func answerPrompt(t *testing.T, m model, key string) model {
	t.Helper()

	kind := m.form.kind

	var msg tea.Msg = keyMsg(key)
	for range 8 {
		updated, cmd := m.form.update(m, msg)
		next, ok := updated.(model)
		require.True(t, ok)

		m = next
		if cmd == nil {
			return m
		}
		if m.form.kind != kind {
			return settle(t, m, cmd)
		}

		msg = cmd()
	}

	t.Fatal("the prompt never closed")

	return m
}

func archiveModel(agents agentv1connect.AgentServiceClient, workflows workflowv1connect.WorkflowServiceClient, keys config.Keybindings) model {
	tmux := &recordingTmux{}

	return model{
		api: api{
			agents:    agents,
			workflows: workflows,
			schedules: emptyLists{},
		},
		tmux:   tmux,
		popup:  NewPopupWindow(tmux, "test-session"),
		frame:  layout.New(tmux, config.DefaultLayout()),
		keys:   keys,
		screen: screenArchive,
		listing: listing{
			workflows: []panel.Workflow{{ID: "w1", Name: "backend"}},
			archivedAgents: archivedItems([]panel.Agent{{
				ID:         "a1",
				Name:       "api-refactor",
				WorkflowID: "w1",
				State:      panel.StateIdle,
			}}),
		},
		archive: archiveScreen{rowCursor: rowCursor{cursor: 1}},
	}
}

func archivedWorkflow(id, name string) panel.Workflow {
	at := time.Date(2026, time.July, 30, 9, 0, 0, 0, time.UTC)

	return panel.Workflow{
		ID:         id,
		Name:       name,
		ArchivedAt: &at,
	}
}

func splitArchiveModel(t *testing.T) model {
	t.Helper()

	m := archiveModel(&fakeArchiveAgents{}, &fakeArchiveWorkflows{}, config.DefaultKeybindings())
	m.width, m.height = 140, 24
	m.frame.Attach("%0")
	m.frame.Solo(true)

	return m
}

func archivedAgent(id, name, workflowID string) panel.Agent {
	at := time.Date(2026, time.July, 30, 9, 0, 0, 0, time.UTC)

	return panel.Agent{
		ID:         id,
		Name:       name,
		WorkflowID: workflowID,
		State:      panel.StateIdle,
		ArchivedAt: &at,
	}
}

func archiveLines(t *testing.T, m model) []view.Line {
	t.Helper()

	rows := m.archive.listRows(m.shared())
	list := view.Prepare(m.archive.snapshot(m.shared(), listPane{width: 80}))
	require.Len(t, list.Lines, len(rows))

	return list.Lines
}

func archivedItems(agents []panel.Agent) []panel.AgentRow {
	return xslice.Map(agents, func(agent panel.Agent) panel.AgentRow {
		return panel.AgentRow{Agent: agent}
	})
}

func (m model) archiveHelp() keyMap {
	return m.listHelpOf(m.archive.listParams(m.shared()))
}
