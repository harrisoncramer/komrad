package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"weave/config"
	"weave/internal/errui"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"
	actionv1 "weave/proto/gen/action/v1"
	"weave/proto/gen/action/v1/actionv1connect"
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

// TestHoveredWorkflow checks the cursor reports a real workflow only when it
// sits on that workflow's header, since the workflow keys act on what is hovered.
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
				assert.NoError(t, err)

				return
			}

			require.Error(t, err)
			assert.Equal(t, tt.wantReason, errui.Reason(err), "the service's message is what a banner would carry")
		})
	}
}

// TestAvailableActionsSkipUnderscoredNames covers what the workflow form's setup and cleanup
// fields are chosen from: files meant only to be sourced are excluded, since naming one
// would run a fragment on its own.
func TestAvailableActionsSkipUnderscoredNames(t *testing.T) {
	m := model{api: api{actions: &fakeActions{}}}

	got := m.getAvailableActions()

	assert.Equal(t, []actionChoice{
		{name: "open-pr"},
		{name: "unrunnable"},
		{name: "compiled"},
	}, got)
}

// TestFailedWorkflowListKeepsTheTree covers a workflow list that does not answer: the tree
// stays as it was, since blanking it would say every workflow had just been archived.
func TestFailedWorkflowListKeepsTheTree(t *testing.T) {
	m := model{
		frame:   layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:    config.DefaultKeybindings(),
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "api"}, {ID: "w2", Name: "web"}}},
	}

	m.applyRefresh(refreshResult{workflowsErr: errors.New("connection reset")})

	assert.Len(t, m.listing.workflows, 2, "the tree already on screen is kept")
	assert.Error(t, m.unreachable, "and the panel says the server is not answering")
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
			name:      "an edit lands on the workflow without moving it",
			current:   []workflowstore.Workflow{{ID: "w1", Name: "api"}, {ID: "w2", Name: "web"}},
			fresh:     []workflowstore.Workflow{{ID: "w1", Name: "api"}, {ID: "w2", Name: "web", Context: "notes"}},
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

// TestRefreshRestagesWorkflowContext pins a context edited elsewhere reaching the stage,
// without restaging identical text and respawning the pane every tick.
func TestRefreshRestagesWorkflowContext(t *testing.T) {
	tmux := &recordingTmux{}
	frame := layout.New(tmux, config.DefaultLayout())
	frame.Attach("%0")

	m := model{
		frame:   frame,
		keys:    config.DefaultKeybindings(),
		screen:  screenAgents,
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "api", Context: "old notes"}}},
	}
	m.stageSelection()
	staged := len(tmux.respawns)

	fresh := refreshResult{workflows: []workflowstore.Workflow{{ID: "w1", Name: "api", Context: "new notes"}}}
	updated, _ := m.Update(fresh)
	m, ok := updated.(model)
	require.True(t, ok)

	assert.Equal(t, "new notes", m.listing.workflows[0].Context)
	require.Greater(t, len(tmux.respawns), staged, "the edit is restaged without the cursor moving")

	shown, err := os.ReadFile(filepath.Join(os.TempDir(), "weave-stage.ansi"))
	require.NoError(t, err)

	assert.Contains(t, ansi.Strip(string(shown)), "new notes")

	staged = len(tmux.respawns)
	updated, _ = m.Update(fresh)
	_, ok = updated.(model)
	require.True(t, ok)

	assert.Equal(t, staged, len(tmux.respawns), "an unchanged refresh leaves the stage pane alone")
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
	require.NotNil(t, editing.values.workflow)
	assert.True(t, editing.values.workflow.ArchiveOnPRClosed, "the form opens on what is stored")

	editing.form.Init()
	editing.values.workflow.Cwd = "/work/api-v2"

	updated, cmd := editing.submitWorkflowForm()
	submitted, ok := updated.(model)
	require.True(t, ok)
	require.Nil(t, settle(t, submitted, cmd).banner, "the service accepted the write")

	require.NotNil(t, sent.updatedWorkflow)
	assert.Equal(t, "/work/api-v2", sent.updatedWorkflow.GetCwd(), "the field that was edited")
	assert.True(t, sent.updatedWorkflow.GetArchiveOnPrClosed(), "and the one that was not")
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

	opened, _ := m.renameWorkflowForm(m.listing.workflows[0])
	renaming, ok := opened.(model)
	require.True(t, ok)
	renaming.form.Init()
	renaming.values.workflow.Name = "backend"

	updated, cmd := renaming.submitWorkflowForm()
	submitted, ok := updated.(model)
	require.True(t, ok)
	require.Nil(t, settle(t, submitted, cmd).banner, "the service accepted the write")

	require.NotNil(t, sent.updatedWorkflow)
	assert.Equal(t, "backend", sent.updatedWorkflow.GetName())
	assert.Equal(t, "/work/api", sent.updatedWorkflow.GetCwd(), "a rename carries the config it is not changing")
	assert.True(t, sent.updatedWorkflow.GetArchiveOnPrClosed())
}

type fakeActions struct {
	actionv1connect.ActionServiceClient
}

func (f *fakeActions) ListActions(_ context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[actionv1.ListActionsResponse], error) {
	return connect.NewResponse(&actionv1.ListActionsResponse{Actions: []*actionv1.Action{
		{Name: "open-pr", Executable: true},
		{Name: "unrunnable"},
		{Name: "compiled", Executable: true},
		{Name: "_internal", Executable: true},
	}}), nil
}
