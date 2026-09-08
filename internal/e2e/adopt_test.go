package e2e

import (
	"testing"

	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Explainer: adoption used to answer a branch that already had an agent with that agent, which
// is a success that started nothing: the panel closed its form and moved to a row that was there
// all along, saying nothing, and read as the call having quietly failed. A refusal is the answer
// the panel already knows how to show.

// TestAdoptingTheSameBranchTwiceIsRefused covers the whole way out to the API, since the code the
// refusal carries is what tells the panel to report it rather than navigate.
func TestAdoptingTheSameBranchTwiceIsRefused(t *testing.T) {
	hermeticGit(t)

	instance := weavetest.Start(t, weavetest.Params{WorktreesDir: t.TempDir()})
	ctx := t.Context()

	repo := initGitRepo(t)
	runGitIn(t, repo, "branch", "someones/work")

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name:     "adopters",
		Cwd:      repo,
		Worktree: &workflowv1.WorktreeConfig{Enabled: true},
	}))
	require.NoError(t, err)

	workflowID := workflow.Msg.GetWorkflow().GetId()

	adopted, err := instance.Agents.Adopt(ctx, connect.NewRequest(&agentv1.AdoptRequest{
		Ref:        "someones/work",
		WorkflowId: workflowID,
	}))
	require.NoError(t, err)

	_, err = instance.Agents.Adopt(ctx, connect.NewRequest(&agentv1.AdoptRequest{
		Ref:        "someones/work",
		WorkflowId: workflowID,
	}))
	require.Error(t, err)

	assert.Equal(t, connect.CodeAlreadyExists, connect.CodeOf(err), "which is the 409 a caller reads as a conflict")
	assert.Contains(t, err.Error(), adopted.Msg.GetAgent().GetName(),
		"and the refusal names the agent holding the branch rather than leaving it to be looked up")

	listed, err := instance.Agents.ListAgents(ctx, connect.NewRequest(&agentv1.ListAgentsRequest{}))
	require.NoError(t, err)
	assert.Len(t, listed.Msg.GetAgents(), 1, "the refused call started nothing")
}
