package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"weave/config"
	agentstore "weave/internal/store/agent"
	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAdoptingTheSameBranchTwiceSaysItWasAlreadyThere covers what the panel needs to tell a
// second adoption apart from a refusal, which is the answer reading as nothing having happened.
func TestAdoptingTheSameBranchTwiceSaysItWasAlreadyThere(t *testing.T) {
	hermeticGit(t)

	instance := weavetest.Start(t, weavetest.Params{WorktreesDir: t.TempDir()})
	ctx := t.Context()

	repo := initGitRepo(t)
	runGitIn(t, repo, "branch", "someones/work")

	workflowID := adoptWorkflow(t, instance, repo, nil)

	first, err := instance.Agents.Adopt(ctx, connect.NewRequest(&agentv1.AdoptRequest{
		Ref:        "someones/work",
		WorkflowId: workflowID,
	}))
	require.NoError(t, err)
	assert.False(t, first.Msg.GetExisting(), "the first adoption is what put an agent on the branch")

	second, err := instance.Agents.Adopt(ctx, connect.NewRequest(&agentv1.AdoptRequest{
		Ref:        "someones/work",
		WorkflowId: workflowID,
	}))
	require.NoError(t, err)

	assert.Equal(t, first.Msg.GetAgent().GetId(), second.Msg.GetAgent().GetId(),
		"the agent already on the branch comes back rather than a second one being started")
	assert.True(t, second.Msg.GetExisting(),
		"and the caller is told it was already there rather than left to read a success as a no-op")
}

// Explainer: a create that fails during setup leaves its row in failed_to_create still stamped
// with the branch, which is what puts "setup failed, it never launched" on the panel. Counting
// that row as the agent on the branch used to answer every later adoption with the corpse, so
// the branch could never be adopted again and the caller was told it had succeeded.

// TestAdoptingAfterASetupFailureLaunchesRatherThanReturningTheCorpse covers the branch still
// being adoptable once the reason it failed has gone.
func TestAdoptingAfterASetupFailureLaunchesRatherThanReturningTheCorpse(t *testing.T) {
	hermeticGit(t)

	instance := weavetest.Start(t, weavetest.Params{WorktreesDir: t.TempDir()})
	ctx := t.Context()

	repo := initGitRepo(t)
	runGitIn(t, repo, "branch", "someones/work")

	workflowID := adoptWorkflow(t, instance, repo, refuseOnceHook(t, instance))

	_, err := instance.Agents.Adopt(ctx, connect.NewRequest(&agentv1.AdoptRequest{
		Ref:        "someones/work",
		WorkflowId: workflowID,
	}))
	require.Error(t, err, "the hook refused the launch, so the adoption is refused with it")
	assert.Contains(t, err.Error(), "refusing the first launch", "and says what refused it")

	corpse := agentOnBranch(t, instance, "someones/work")
	require.NotNil(t, corpse, "the failed create is left on the list for a person to see")
	assert.Equal(t, agentstore.StateFailedToCreate, corpse.State)

	adopted, err := instance.Agents.Adopt(ctx, connect.NewRequest(&agentv1.AdoptRequest{
		Ref:        "someones/work",
		WorkflowId: workflowID,
	}))
	require.NoError(t, err, "the hook lets the second launch through, so the branch is adoptable again")

	assert.NotEqual(t, corpse.ID, adopted.Msg.GetAgent().GetId(),
		"a fresh agent takes the work rather than the corpse being handed back")
	assert.False(t, adopted.Msg.GetExisting(), "which is an adoption rather than one that was already there")
}

// adoptWorkflow makes a worktree workflow over a repository, hanging the given hooks on it.
func adoptWorkflow(t *testing.T, instance *weavetest.Instance, repo string, hooks []*workflowv1.WorkflowHook) string {
	t.Helper()

	workflow, err := instance.Workflows.CreateWorkflow(t.Context(), connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name:     "adopters",
		Cwd:      repo,
		Worktree: &workflowv1.WorktreeConfig{Enabled: true},
		Hooks:    hooks,
	}))
	require.NoError(t, err)

	return workflow.Msg.GetWorkflow().GetId()
}

// refuseOnceHook is a pre-launch hook that fails the first launch it sees and lets every later
// one through, which is how one workflow holds both halves of a retry.
func refuseOnceHook(t *testing.T, instance *weavetest.Instance) []*workflowv1.WorkflowHook {
	t.Helper()

	const name = "refuse-once"

	marker := filepath.Join(t.TempDir(), "refused")
	script := fmt.Sprintf("#!/bin/sh\nif [ ! -e %q ]; then\n  touch %q\n  echo 'refusing the first launch' >&2\n  exit 1\nfi\n", marker, marker)

	require.NoError(t, os.WriteFile(filepath.Join(instance.HooksDir, name), []byte(script), 0o755))

	return []*workflowv1.WorkflowHook{{Event: config.HookPreLaunch, Script: name}}
}

// agentOnBranch reads back the one agent stamped with a branch, nil when none is.
func agentOnBranch(t *testing.T, instance *weavetest.Instance, branch string) *agentstore.Agent {
	t.Helper()

	listed, err := instance.Stores.Agents.ListAll(t.Context())
	require.NoError(t, err)

	for _, agent := range listed {
		if agent.AdoptedBranch == branch {
			return &agent
		}
	}

	return nil
}
