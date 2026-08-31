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

// TestRunCleanupLogsOneThatWouldNotStart covers the cleanup that never becomes a process at
// all, which is what a renamed script leaves behind.
func TestRunCleanupLogsOneThatWouldNotStart(t *testing.T) {
	lifecycle := &fakeLifecycle{cleanupStartErr: actions.ErrActionNotFound}
	ctx, records := loggedContext(t)

	runCleanup(ctx, lifecycle, agentstore.Agent{ID: "a1", Cwd: "/repo/.worktrees/auth"}, "was-renamed", nil)

	got := recordFor(t, records(), "the cleanup action would not start")
	assert.Equal(t, "ERROR", got["severity_text"])
	assert.Equal(t, "was-renamed", got["weave.action.name"])
	assert.Equal(t, "/repo/.worktrees/auth", got["weave.action.agent.cwd"])
}

// TestCleanupActionLogsAWorkflowThatWouldNotRead covers the read that breaks rather than
// answering, which skips a teardown as quietly as a workflow naming none.
func TestCleanupActionLogsAWorkflowThatWouldNotRead(t *testing.T) {
	workflows := brokenWorkflowReader{err: errors.New("connection refused")}
	ctx, records := loggedContext(t)

	agent := agentstore.Agent{ID: "a1", Cwd: "/repo/.worktrees/auth", WorkflowID: "wf1"}
	assert.Empty(t, cleanupActionFor(ctx, workflows, agent))

	got := recordFor(t, records(), "the workflow behind an agent could not be read")
	assert.Equal(t, "ERROR", got["severity_text"])
	assert.Equal(t, "wf1", got["weave.workflow.id"])
	assert.Equal(t, "a1", got["weave.action.agent.id"])
	assert.Equal(t, "/repo/.worktrees/auth", got["weave.action.agent.cwd"])
}

// TestCleanupActionSaysNothingAboutAWorkflowNamingNone covers the quiet half, since a
// workflow with no cleanup is not a fault and a line about one would be noise on every
// delete.
func TestCleanupActionSaysNothingAboutAWorkflowNamingNone(t *testing.T) {
	ctx, records := loggedContext(t)

	assert.Empty(t, cleanupActionFor(ctx, fakeWorkflowReader{}, agentstore.Agent{ID: "a1", WorkflowID: "wf1"}))
	assert.Empty(t, records())
}

func TestCleanupActionOverridesTheWorkflow(t *testing.T) {
	workflows := fakeWorkflowReader{workflow: &workflowstore.Workflow{ID: "wf1", CleanupAction: "drop-worktree"}}
	got := cleanupActionFor(context.Background(), workflows, agentstore.Agent{ID: "a1", WorkflowID: "wf1", CleanupAction: xptr.Ptr("custom-cleanup")})
	assert.Equal(t, "custom-cleanup", got)
}

func TestCleanupActionOverrideToNoneWinsOverTheWorkflow(t *testing.T) {
	workflows := fakeWorkflowReader{workflow: &workflowstore.Workflow{ID: "wf1", CleanupAction: "drop-worktree"}}
	got := cleanupActionFor(context.Background(), workflows, agentstore.Agent{ID: "a1", WorkflowID: "wf1", CleanupAction: xptr.Ptr("")})
	assert.Empty(t, got)
}

func TestSetupActionOverridesTheWorkflow(t *testing.T) {
	workflows := fakeWorkflowReader{workflow: &workflowstore.Workflow{ID: "wf1", SetupAction: "make-worktree"}}
	got := setupActionFor(context.Background(), workflows, agentstore.Agent{ID: "a1", WorkflowID: "wf1", SetupAction: xptr.Ptr("custom-setup")})
	assert.Equal(t, "custom-setup", got)
}

func TestSetupActionOverrideToNoneWinsOverTheWorkflow(t *testing.T) {
	workflows := fakeWorkflowReader{workflow: &workflowstore.Workflow{ID: "wf1", SetupAction: "make-worktree"}}
	got := setupActionFor(context.Background(), workflows, agentstore.Agent{ID: "a1", WorkflowID: "wf1", SetupAction: xptr.Ptr("")})
	assert.Empty(t, got)
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
