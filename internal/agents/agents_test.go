package agents

import (
	"context"
	"errors"
	"testing"

	"weave/internal/scripts"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunDeprovisionLogsOneThatWouldNotStart covers the deprovision that never becomes a process at
// all.
func TestRunDeprovisionLogsOneThatWouldNotStart(t *testing.T) {
	lifecycle := &fakeLifecycle{deprovisionStartErr: scripts.ErrScriptNotFound}
	ctx, records := loggedContext(t)

	runDeprovision(ctx, lifecycle, agentstore.Agent{ID: "a1", Cwd: "/repo/.worktrees/auth"}, "was-renamed", nil)

	got := recordFor(t, records(), "the deprovisioner would not start")
	assert.Equal(t, "ERROR", got["severity_text"])
	assert.Equal(t, "was-renamed", got["weave.script.name"])
	assert.Equal(t, "/repo/.worktrees/auth", got["weave.script.agent.cwd"])
}

// TestDeprovisionerLogsAWorkflowThatWouldNotRead covers the read that breaks rather than
// answering, which skips a teardown as quietly as a workflow naming none.
func TestDeprovisionerLogsAWorkflowThatWouldNotRead(t *testing.T) {
	workflows := brokenWorkflowReader{err: errors.New("connection refused")}
	ctx, records := loggedContext(t)

	agent := agentstore.Agent{
		ID:         "a1",
		Cwd:        "/repo/.worktrees/auth",
		WorkflowID: "wf1",
	}
	assert.Empty(t, deprovisionerFor(ctx, workflows, agent))

	got := recordFor(t, records(), "the workflow behind an agent could not be read")
	assert.Equal(t, "ERROR", got["severity_text"])
	assert.Equal(t, "wf1", got["weave.workflow.id"])
	assert.Equal(t, "a1", got["weave.script.agent.id"])
	assert.Equal(t, "/repo/.worktrees/auth", got["weave.script.agent.cwd"])
}

// TestDeprovisionerSaysNothingWhenThereIsNothingToTearDown pins that no teardown is named and
// nothing is logged.
func TestDeprovisionerSaysNothingWhenThereIsNothingToTearDown(t *testing.T) {
	tests := []struct {
		name       string
		workflowID func(t *testing.T, st *testStore) string
	}{
		{
			name: "a workflow no row holds",
			workflowID: func(*testing.T, *testStore) string {
				return "b9b1a1de-0000-7000-8000-000000000000"
			},
		},
		{
			name:       "a workflow that makes no worktrees",
			workflowID: seedWorkflow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			agent := agentstore.Agent{
				ID:         "a1",
				WorkflowID: tt.workflowID(t, st),
			}
			ctx, records := loggedContext(t)

			assert.Empty(t, deprovisionerFor(ctx, st.Workflows, agent))
			assert.Empty(t, records())
		})
	}
}

// TestTheWorktreeToggleIsTheWholeProvisioner pins what a workflow can say about an agent's
// directory now that it cannot name a script: the built-in pair when the toggle is on, and
// nothing at all otherwise.
func TestTheWorktreeToggleIsTheWholeProvisioner(t *testing.T) {
	tests := []struct {
		name            string
		worktree        bool
		provision       *bool
		wantProvision   string
		wantDeprovision string
	}{
		{
			name:     "a workflow that provisions nothing has nothing to run either way",
			worktree: false,
		},
		{
			name:            "the toggle on names the built-in pair",
			worktree:        true,
			wantProvision:   worktreeProvisioner,
			wantDeprovision: worktreeDeprovisioner,
		},
		{
			name:      "an agent told to keep its own directory runs neither half",
			worktree:  true,
			provision: xptr.Ptr(false),
		},
		{
			name:            "an agent following its workflow runs both",
			worktree:        true,
			provision:       xptr.Ptr(true),
			wantProvision:   worktreeProvisioner,
			wantDeprovision: worktreeDeprovisioner,
		},
		{
			name:      "the override cannot conjure a worktree a workflow does not make",
			worktree:  false,
			provision: xptr.Ptr(true),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			config := worktreeConfig(t, "backend")
			config.Worktree.Enabled = tt.worktree
			workflow, err := st.Workflows.Create(t.Context(), config)
			require.NoError(t, err)

			agent := agentstore.Agent{
				ID:               "a1",
				WorkflowID:       workflow.ID,
				ProvisionEnabled: tt.provision,
			}

			assert.Equal(t, tt.wantProvision, provisionerFor(t.Context(), st.Workflows, agent))
			assert.Equal(t, tt.wantDeprovision, deprovisionerFor(t.Context(), st.Workflows, agent))
		})
	}
}

type brokenWorkflowReader struct {
	err error
}

func (b brokenWorkflowReader) Get(ctx context.Context, id string) (*workflowstore.Workflow, error) {
	return b.GetIncludingArchived(ctx, id)
}

func (b brokenWorkflowReader) GetIncludingArchived(context.Context, string) (*workflowstore.Workflow, error) {
	return nil, b.err
}
