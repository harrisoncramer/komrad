package ui

import (
	"context"
	"errors"
	"testing"

	"weave/config"
	"weave/internal/errui"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"
	hookv1 "weave/proto/gen/hook/v1"
	"weave/proto/gen/hook/v1/hookv1connect"
	workflowv1 "weave/proto/gen/workflow/v1"
	"weave/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestAgentRows checks the tree the list renders, where every workflow is a header
// with its agents beneath it.
func TestAgentRows(t *testing.T) {
	tests := []struct {
		name      string
		workflows []workflowstore.Workflow
		agents    []agentItem
		wantRows  []string
	}{
		{
			name:     "an empty store renders nothing at all",
			wantRows: []string{},
		},
		{
			name:      "an empty workflow keeps its header, so an agent can be added to it",
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			wantRows:  []string{"header:backend"},
		},
		{
			name: "agents nest under the workflow they belong to",
			workflows: []workflowstore.Workflow{
				{ID: "w1", Name: "backend"},
				{ID: "w2", Name: "frontend"},
			},
			agents: []agentItem{
				{agent: agentstore.Agent{Name: "auth", WorkflowID: "w1"}},
				{agent: agentstore.Agent{Name: "ui", WorkflowID: "w2"}},
			},
			wantRows: []string{
				"header:backend",
				"agent:auth",
				"header:frontend",
				"agent:ui",
			},
		},
		{
			name: "an agent whose workflow is gone is not rendered",
			workflows: []workflowstore.Workflow{
				{ID: "w1", Name: "backend"},
			},
			agents: []agentItem{
				{agent: agentstore.Agent{Name: "auth", WorkflowID: "w1"}},
				{agent: agentstore.Agent{Name: "orphan", WorkflowID: "vanished"}},
			},
			wantRows: []string{
				"header:backend",
				"agent:auth",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{listing: listing{workflows: tt.workflows, agents: tt.agents}}

			assert.Equal(t, tt.wantRows, describeRows(m.agentRows()))
		})
	}
}

// TestHoveredWorkflow checks the cursor reports a real workflow only when it sits on that
// workflow's header.
func TestHoveredWorkflow(t *testing.T) {
	tests := []struct {
		name     string
		cursor   int
		wantName string
		wantOK   bool
	}{
		{
			name:   "on an agent",
			cursor: 1,
		},
		{
			name:   "on the no-workflow header, which has no record behind it",
			cursor: 2,
		},
		{
			name:   "on an agent belonging to no workflow",
			cursor: 3,
		},
		{
			name:     "on a workflow header",
			cursor:   0,
			wantName: "backend",
			wantOK:   true,
		},
	}

	m := model{
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}}, agents: []agentItem{
			{agent: agentstore.Agent{Name: "auth", WorkflowID: "w1"}},
			{agent: agentstore.Agent{Name: "loose"}},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.tree.cursor = tt.cursor

			workflow, ok := m.hoveredWorkflow()
			require.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.wantName, workflow.Name)
			}
		})
	}
}

type refusingWorkflows struct {
	workflowv1connect.WorkflowServiceClient
	err    error
	writes int
}

func (r *refusingWorkflows) ListWorkflows(context.Context, *connect.Request[workflowv1.ListWorkflowsRequest]) (*connect.Response[workflowv1.ListWorkflowsResponse], error) {
	return nil, r.err
}

func (r *refusingWorkflows) CreateWorkflow(context.Context, *connect.Request[workflowv1.CreateWorkflowRequest]) (*connect.Response[workflowv1.CreateWorkflowResponse], error) {
	r.writes++

	return nil, r.err
}

func (r *refusingWorkflows) UpdateWorkflow(context.Context, *connect.Request[workflowv1.UpdateWorkflowRequest]) (*connect.Response[emptypb.Empty], error) {
	r.writes++

	return nil, r.err
}

func (r *refusingWorkflows) ArchiveWorkflow(context.Context, *connect.Request[workflowv1.ArchiveWorkflowRequest]) (*connect.Response[emptypb.Empty], error) {
	r.writes++

	return nil, r.err
}

// TestWorkflowWrites covers each write handing its error back rather than discarding it.
func TestWorkflowWrites(t *testing.T) {
	tests := []struct {
		name       string
		call       func(model) error
		wantWrites int
		wantReason string
	}{
		{
			name: "a refused create",
			call: func(m model) error {
				_, err := m.api.createWorkflow(workflowstore.Config{Name: "api"})

				return err
			},
			wantWrites: 1,
			wantReason: "quota reached",
		},
		{
			name:       "a refused update",
			call:       func(m model) error { return m.api.updateWorkflow(workflowstore.Workflow{ID: "w1", Name: "api"}) },
			wantWrites: 1,
			wantReason: "quota reached",
		},
		{
			name:       "a refused archive",
			call:       func(m model) error { return m.api.archiveWorkflow("w1") },
			wantWrites: 1,
			wantReason: "quota reached",
		},
		{
			name: "an update to a blank name never reaches the service",
			call: func(m model) error { return m.api.updateWorkflow(workflowstore.Workflow{ID: "w1", Name: "  "}) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflows := &refusingWorkflows{err: connect.NewError(connect.CodeResourceExhausted, errors.New("quota reached"))}
			m := model{api: api{workflows: workflows}}

			err := tt.call(m)

			assert.Equal(t, tt.wantWrites, workflows.writes)
			if tt.wantReason == "" {
				require.NoError(t, err)

				return
			}

			require.Error(t, err)
			assert.Equal(t, tt.wantReason, errui.GetReason(err), "the service's message is what a banner would carry")
		})
	}
}

// TestAvailableHooksCarryTheWholeDirectory covers what the workflow form's hook fields are
// chosen from, including a file waiting on a chmod.
func TestAvailableHooksCarryTheWholeDirectory(t *testing.T) {
	m := model{api: api{hooks: &fakeHooks{}}}

	got := m.getAvailableHooks()

	assert.Equal(t, []hookChoice{
		{name: "install-deps"},
		{name: "unrunnable"},
		{name: "copy-env"},
	}, got)
}

// TestFailedWorkflowListKeepsTheTree covers a workflow list that does not answer: the tree stays
// as it was.
func TestFailedWorkflowListKeepsTheTree(t *testing.T) {
	m := model{
		frame:   layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:    config.DefaultKeybindings(),
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "api"}, {ID: "w2", Name: "web"}}},
	}

	m.applyRefresh(refreshResult{workflowsErr: errors.New("connection reset")})

	assert.Len(t, m.listing.workflows, 2, "the tree already on screen is kept")
	require.Error(t, m.unreachable, "and the panel says the server is not answering")
}

// TestApplyWorkflows pins the refresh taking the listed workflows as they come, newest
// first.
func TestApplyWorkflows(t *testing.T) {
	tests := []struct {
		name      string
		current   []workflowstore.Workflow
		fresh     []workflowstore.Workflow
		wantNames []string
	}{
		{
			name:    "an edit lands on the workflow without moving it",
			current: []workflowstore.Workflow{{ID: "w1", Name: "api"}, {ID: "w2", Name: "web"}},
			fresh: []workflowstore.Workflow{
				{ID: "w1", Name: "api"},
				{
					ID:      "w2",
					Name:    "web",
					Context: "notes",
				},
			},
			wantNames: []string{"api", "web"},
		},
		{
			name:      "a workflow created elsewhere lands where the listing put it",
			current:   []workflowstore.Workflow{{ID: "w1", Name: "api"}},
			fresh:     []workflowstore.Workflow{{ID: "w2", Name: "web"}, {ID: "w1", Name: "api"}},
			wantNames: []string{"web", "api"},
		},
		{
			name:      "a workflow archived elsewhere drops out",
			current:   []workflowstore.Workflow{{ID: "w1", Name: "api"}, {ID: "w2", Name: "web"}},
			fresh:     []workflowstore.Workflow{{ID: "w2", Name: "web"}},
			wantNames: []string{"web"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{listing: listing{workflows: tt.current}}

			m.applyWorkflows(tt.fresh)

			names := make([]string, 0, len(m.listing.workflows))
			for _, w := range m.listing.workflows {
				names = append(names, w.Name)
			}
			assert.Equal(t, tt.wantNames, names)
		})
	}
}

// TestRefreshDrawsAnEditedWorkflowContext pins a context edited elsewhere reaching the column on
// the next listing, which it does by being drawn from the row the cursor is on rather than pushed
// into a pane that had to be told the text had changed.
func TestRefreshDrawsAnEditedWorkflowContext(t *testing.T) {
	tmux := &recordingTmux{width: 200, height: 40}
	frame := layout.New(tmux, config.DefaultLayout())
	frame.Attach("%0")

	m := model{
		frame:  frame,
		keys:   config.DefaultKeybindings(),
		width:  200,
		height: 40,
		screen: screenAgents,
		listing: listing{
			workflows: []workflowstore.Workflow{
				{
					ID:      "w1",
					Name:    "api",
					Context: "old notes",
				},
			},
		},
	}
	m.stageSelection()
	require.Contains(t, ansi.Strip(m.View()), "old notes", "the folder's notes stand beside the list")

	staged := len(tmux.respawns)

	fresh := refreshResult{
		workflows: []workflowstore.Workflow{
			{
				ID:      "w1",
				Name:    "api",
				Context: "new notes",
			},
		},
	}
	updated, _ := m.Update(fresh)
	m, ok := updated.(model)
	require.True(t, ok)

	assert.Equal(t, "new notes", m.listing.workflows[0].Context)

	view := ansi.Strip(m.View())
	assert.Contains(t, view, "new notes", "the edit is on screen without the cursor moving")
	assert.NotContains(t, view, "old notes")
	assert.Len(t, tmux.respawns, staged, "and nothing was pushed into a pane to put it there")
}

func describeRows(rows []agentRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.header {
			out = append(out, "header:"+row.workflow.Name)
			continue
		}

		out = append(out, "agent:"+row.item.agent.Name)
	}

	return out
}

// TestWorkflowEditFormRoundTripsTheStoredConfig covers the edit path whole, from the
// workflow the service reports, through the form, to the update written back.
func TestWorkflowEditFormRoundTripsTheStoredConfig(t *testing.T) {
	sent := &formRecorder{storedWorkflow: &workflowv1.Workflow{
		Id:                "w1",
		Name:              "api",
		Harness:           "claude",
		Cwd:               "/work/api",
		Flags:             "--model opus",
		ArchiveOnPrClosed: true,
		Worktree:          &workflowv1.WorktreeConfig{Enabled: true, BaseBranch: "develop"},
	}}

	opened, _ := formModel(sent).editWorkflowForm(workflowstore.Workflow{ID: "w1", Name: "api"})
	editing, ok := opened.(model)
	require.True(t, ok)
	require.Equal(t, formWorkflow, editing.form.kind)

	// Submitting it untouched is what says the form opened on the stored config rather than on
	// the thin row the key was pressed over.
	updated, cmd := editing.answerForm()
	submitted, ok := updated.(model)
	require.True(t, ok)
	require.Empty(t, settle(t, submitted, cmd).messages.items, "the service accepted the write")

	require.NotNil(t, sent.updatedWorkflow)
	assert.Equal(t, "/work/api", sent.updatedWorkflow.GetCwd())
	assert.Equal(t, "--model opus", sent.updatedWorkflow.GetFlags())
	assert.Equal(t, "claude", sent.updatedWorkflow.GetHarness())
	assert.True(t, sent.updatedWorkflow.GetArchiveOnPrClosed())
	require.NotNil(t, sent.updatedWorkflow.GetWorktree())
	assert.Equal(t, "develop", sent.updatedWorkflow.GetWorktree().GetBaseBranch())
}

// TestWorkflowRenameKeepsTheRestOfTheConfig covers the rename path, which takes the
// workflow out of the listing and writes it back whole under a new name.
func TestWorkflowRenameKeepsTheRestOfTheConfig(t *testing.T) {
	sent := &formRecorder{}
	m := formModel(sent)
	m.listing.workflows = workflowsFrom([]*workflowv1.Workflow{{
		Id:                "w1",
		Name:              "api",
		Cwd:               "/work/api",
		ArchiveOnPrClosed: true,
	}})

	values := workflowFormValues{Name: "backend"}

	opened, _ := m.openWorkflowRename(m.listing.workflows[0], &values)
	renaming, ok := opened.(model)
	require.True(t, ok)

	updated, cmd := renaming.answerForm()
	submitted, ok := updated.(model)
	require.True(t, ok)
	require.Empty(t, settle(t, submitted, cmd).messages.items, "the service accepted the write")

	require.NotNil(t, sent.updatedWorkflow)
	assert.Equal(t, "backend", sent.updatedWorkflow.GetName())
	assert.Equal(t, "/work/api", sent.updatedWorkflow.GetCwd(), "a rename carries the config it is not changing")
	assert.True(t, sent.updatedWorkflow.GetArchiveOnPrClosed())
}

type fakeHooks struct {
	hookv1connect.HookServiceClient
}

func (f *fakeHooks) ListHooks(_ context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[hookv1.ListHooksResponse], error) {
	return connect.NewResponse(&hookv1.ListHooksResponse{Hooks: []*hookv1.Hook{
		{Name: "install-deps", Executable: true},
		{Name: "unrunnable"},
		{Name: "copy-env", Executable: true},
	}}), nil
}
