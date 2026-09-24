package ui

import (
	"context"
	"errors"
	"testing"

	"weave/config"
	"weave/internal/errui"
	"weave/internal/ui/layout"
	"weave/internal/ui/panel"
	hookv1 "weave/proto/gen/hook/v1"
	"weave/proto/gen/hook/v1/hookv1connect"
	workflowv1 "weave/proto/gen/workflow/v1"
	"weave/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

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
				_, err := m.api.createWorkflow(panel.WorkflowConfig{Name: "api"})

				return err
			},
			wantWrites: 1,
			wantReason: "quota reached",
		},
		{
			name:       "a refused update",
			call:       func(m model) error { return m.api.updateWorkflow(panel.Workflow{ID: "w1", Name: "api"}) },
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
			call: func(m model) error { return m.api.updateWorkflow(panel.Workflow{ID: "w1", Name: "  "}) },
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
		listing: listing{workflows: []panel.Workflow{{ID: "w1", Name: "api"}, {ID: "w2", Name: "web"}}},
	}

	m.applyWatchLost(watchLostMsg{err: errors.New("connection reset")})

	assert.Len(t, m.listing.workflows, 2, "the tree already on screen is kept")
	require.Error(t, m.unreachable, "and the panel says the server is not answering")
}

func describeRows(rows []agentRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.header {
			out = append(out, "header:"+row.workflow.Name)
			continue
		}

		out = append(out, "agent:"+row.item.Agent.Name)
	}

	return out
}

// TestWorkflowEditFormRoundTripsTheStoredConfig covers the edit path whole, from the
// workflow the watch carries, through the form, to the update written back.
func TestWorkflowEditFormRoundTripsTheStoredConfig(t *testing.T) {
	sent := &formRecorder{}
	watched := workflowFrom(&workflowv1.Workflow{
		Id:       "w1",
		Name:     "api",
		Harness:  "claude",
		Cwd:      "/work/api",
		Flags:    "--model opus",
		Worktree: &workflowv1.WorktreeConfig{Enabled: true, BaseBranch: "develop"},
	})

	opened, _ := newFormModel(sent).editWorkflowForm(watched)
	editing, ok := opened.(model)
	require.True(t, ok)
	require.Equal(t, formWorkflow, editing.form.kind)

	// Submitting it untouched is what says the form opened on the whole row the watch carried.
	updated, cmd := editing.answerForm()
	submitted, ok := updated.(model)
	require.True(t, ok)
	require.Empty(t, settle(t, submitted, cmd).messages.items, "the service accepted the write")

	require.NotNil(t, sent.updatedWorkflow)
	assert.Equal(t, "/work/api", sent.updatedWorkflow.GetCwd())
	assert.Equal(t, "--model opus", sent.updatedWorkflow.GetFlags())
	assert.Equal(t, "claude", sent.updatedWorkflow.GetHarness())
	require.NotNil(t, sent.updatedWorkflow.GetWorktree())
	assert.Equal(t, "develop", sent.updatedWorkflow.GetWorktree().GetBaseBranch())
}

// TestWorkflowRenameKeepsTheRestOfTheConfig covers the rename path, which takes the
// workflow out of the listing and writes it back whole under a new name.
func TestWorkflowRenameKeepsTheRestOfTheConfig(t *testing.T) {
	sent := &formRecorder{}
	m := newFormModel(sent)
	m.listing.workflows = workflowsFrom([]*workflowv1.Workflow{{
		Id:   "w1",
		Name: "api",
		Cwd:  "/work/api",
	}})

	values := workflowFormValues{Name: "backend"}

	renaming := m.openWorkflowRename(m.listing.workflows[0], &values)

	updated, cmd := renaming.answerForm()
	submitted, ok := updated.(model)
	require.True(t, ok)
	require.Empty(t, settle(t, submitted, cmd).messages.items, "the service accepted the write")

	require.NotNil(t, sent.updatedWorkflow)
	assert.Equal(t, "backend", sent.updatedWorkflow.GetName())
	assert.Equal(t, "/work/api", sent.updatedWorkflow.GetCwd(), "a rename carries the config it is not changing")
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
