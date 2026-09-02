package agents

import (
	"context"
	"errors"
	"testing"

	"weave/internal/actions"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
)

// TestRunDeprovisionLogsOneThatWouldNotStart covers the deprovision that never becomes a
// process at all, which is what a renamed script leaves behind.
func TestRunDeprovisionLogsOneThatWouldNotStart(t *testing.T) {
	lifecycle := &fakeLifecycle{deprovisionStartErr: actions.ErrActionNotFound}
	ctx, records := loggedContext(t)

	runDeprovision(ctx, lifecycle, agentstore.Agent{ID: "a1", Cwd: "/repo/.worktrees/auth"}, "was-renamed", nil)

	got := recordFor(t, records(), "the deprovision action would not start")
	assert.Equal(t, "ERROR", got["severity_text"])
	assert.Equal(t, "was-renamed", got["weave.action.name"])
	assert.Equal(t, "/repo/.worktrees/auth", got["weave.action.agent.cwd"])
}

// TestDeprovisionActionLogsAWorkflowThatWouldNotRead covers the read that breaks rather than
// answering, which skips a teardown as quietly as a workflow naming none.
func TestDeprovisionActionLogsAWorkflowThatWouldNotRead(t *testing.T) {
	workflows := brokenWorkflowReader{err: errors.New("connection refused")}
	ctx, records := loggedContext(t)

	agent := agentstore.Agent{
		ID:         "a1",
		Cwd:        "/repo/.worktrees/auth",
		WorkflowID: "wf1",
	}
	assert.Empty(t, deprovisionActionFor(ctx, workflows, agent))

	got := recordFor(t, records(), "the workflow behind an agent could not be read")
	assert.Equal(t, "ERROR", got["severity_text"])
	assert.Equal(t, "wf1", got["weave.workflow.id"])
	assert.Equal(t, "a1", got["weave.action.agent.id"])
	assert.Equal(t, "/repo/.worktrees/auth", got["weave.action.agent.cwd"])
}

// TestDeprovisionActionSaysNothingAboutAWorkflowNamingNone covers the quiet half, since a
// workflow with no deprovision half is not a fault and a line about one would be noise on
// every delete.
func TestDeprovisionActionSaysNothingAboutAWorkflowNamingNone(t *testing.T) {
	ctx, records := loggedContext(t)

	assert.Empty(t, deprovisionActionFor(ctx, fakeWorkflowReader{}, agentstore.Agent{ID: "a1", WorkflowID: "wf1"}))
	assert.Empty(t, records())
}

// TestDeprovisionActionOverridesTheWorkflow pins that an agent's own deprovision action is the
// one that runs, rather than the workflow's.
func TestDeprovisionActionOverridesTheWorkflow(t *testing.T) {
	workflows := fakeWorkflowReader{workflow: &workflowstore.Workflow{ID: "wf1", DeprovisionAction: "drop-worktree"}}
	got := deprovisionActionFor(context.Background(), workflows, agentstore.Agent{
		ID:                "a1",
		WorkflowID:        "wf1",
		DeprovisionAction: xptr.Ptr("custom-cleanup"),
	})
	assert.Equal(t, "custom-cleanup", got)
}

// TestDeprovisionActionOverrideToNoneWinsOverTheWorkflow pins that an override set to nothing
// means nothing to give back at all rather than falling back to the workflow.
func TestDeprovisionActionOverrideToNoneWinsOverTheWorkflow(t *testing.T) {
	workflows := fakeWorkflowReader{workflow: &workflowstore.Workflow{ID: "wf1", DeprovisionAction: "drop-worktree"}}
	got := deprovisionActionFor(context.Background(), workflows, agentstore.Agent{
		ID:                "a1",
		WorkflowID:        "wf1",
		DeprovisionAction: xptr.Ptr(""),
	})
	assert.Empty(t, got)
}

// TestProvisionActionOverridesTheWorkflow pins that an agent's own provision action is the one
// that runs, rather than the workflow's.
func TestProvisionActionOverridesTheWorkflow(t *testing.T) {
	workflows := fakeWorkflowReader{workflow: &workflowstore.Workflow{ID: "wf1", ProvisionAction: "make-worktree"}}
	got := provisionActionFor(context.Background(), workflows, agentstore.Agent{
		ID:              "a1",
		WorkflowID:      "wf1",
		ProvisionAction: xptr.Ptr("custom-setup"),
	})
	assert.Equal(t, "custom-setup", got)
}

// TestProvisionActionOverrideToNoneWinsOverTheWorkflow pins that an override set to nothing
// means nothing provisioned at all rather than falling back to the workflow.
func TestProvisionActionOverrideToNoneWinsOverTheWorkflow(t *testing.T) {
	workflows := fakeWorkflowReader{workflow: &workflowstore.Workflow{ID: "wf1", ProvisionAction: "make-worktree"}}
	got := provisionActionFor(context.Background(), workflows, agentstore.Agent{
		ID:              "a1",
		WorkflowID:      "wf1",
		ProvisionAction: xptr.Ptr(""),
	})
	assert.Empty(t, got)
}

// TestARetiredProvisionerRetiresItsDeprovisionHalf pins the rule that makes one field enough
// for the reviewer, adopt and child cases: an agent told to provision nothing keeps the
// directory it was given, so the other half is not consulted whoever set it.
func TestARetiredProvisionerRetiresItsDeprovisionHalf(t *testing.T) {
	workflows := fakeWorkflowReader{workflow: &workflowstore.Workflow{
		ID:                "wf1",
		ProvisionAction:   "make-worktree",
		DeprovisionAction: "drop-worktree",
	}}

	tests := []struct {
		name        string
		deprovision *string
	}{
		{
			name:        "inherited from the workflow",
			deprovision: nil,
		},
		{
			name:        "named by the caller",
			deprovision: xptr.Ptr("drop-worktree"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deprovisionActionFor(context.Background(), workflows, agentstore.Agent{
				ID:                "a1",
				WorkflowID:        "wf1",
				ProvisionAction:   xptr.Ptr(""),
				DeprovisionAction: tt.deprovision,
			})
			assert.Empty(t, got)
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
