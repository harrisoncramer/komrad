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
)

// TestRunDeprovisionLogsOneThatWouldNotStart covers the deprovision that never becomes a
// process at all, which is what a renamed script leaves behind.
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

// TestDeprovisionerSaysNothingAboutAWorkflowThatProvisionsNothing covers the quiet half,
// since a workflow whose agents keep the directory they are given is not a fault and a line
// about one would be noise on every delete.
func TestDeprovisionerSaysNothingAboutAWorkflowThatProvisionsNothing(t *testing.T) {
	ctx, records := loggedContext(t)

	assert.Empty(t, deprovisionerFor(ctx, fakeWorkflowReader{}, agentstore.Agent{ID: "a1", WorkflowID: "wf1"}))
	assert.Empty(t, records())
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
			workflows := fakeWorkflowReader{workflow: &workflowstore.Workflow{
				ID:       "wf1",
				Worktree: workflowstore.WorktreeConfig{Enabled: tt.worktree},
			}}
			agent := agentstore.Agent{
				ID:               "a1",
				WorkflowID:       "wf1",
				ProvisionEnabled: tt.provision,
			}

			assert.Equal(t, tt.wantProvision, provisionerFor(context.Background(), workflows, agent))
			assert.Equal(t, tt.wantDeprovision, deprovisionerFor(context.Background(), workflows, agent))
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
