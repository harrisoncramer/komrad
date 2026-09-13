//go:build integration

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTeardownHook writes a pre-teardown hook and returns the request for a worktree workflow
// that hangs it on the moment.
func writeTeardownHook(t *testing.T, instance *weavetest.Instance, body string) *workflowv1.CreateWorkflowRequest {
	t.Helper()

	const name = "take-the-stack-down"
	require.NoError(t, os.WriteFile(filepath.Join(instance.HooksDir, name), []byte(body), 0o755))

	request := worktreeWorkflow(t)
	request.Hooks = []*workflowv1.WorkflowHook{{Event: workflowstore.HookPreTeardown, Script: name}}

	return request
}

// TestPreTeardownRunsWhileTheWorktreeIsStillThere covers the hook reading the worktree before
// the deprovisioner hands it back.
func TestPreTeardownRunsWhileTheWorktreeIsStillThere(t *testing.T) {
	hermeticGit(t)

	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	seen := filepath.Join(t.TempDir(), "seen")
	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(writeTeardownHook(t,
		instance, "#!/bin/sh\nls .git >"+seen+"\n")))
	require.NoError(t, err)

	created, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "fix the flaky login test",
		Title:      "login",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)

	id := created.Msg.GetAgent().GetId()
	tree := created.Msg.GetAgent().GetCwd()
	require.DirExists(t, tree)

	_, err = instance.Agents.DeleteAgents(ctx, connect.NewRequest(&agentv1.DeleteAgentsRequest{Ids: []string{id}}))
	require.NoError(t, err)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		_, err := instance.Stores.Agents.GetByID(ctx, id)
		assert.Error(c, err, "the row goes once the teardown and the deprovisioner have both finished")
		assert.NoDirExists(c, tree)
	}, 10*time.Second, 10*time.Millisecond)

	landed, err := os.ReadFile(seen)
	require.NoError(t, err, "the hook ran")
	assert.NotEmpty(t, landed, "and read the worktree the deprovisioner had not taken yet")
}

// TestAFailedPreTeardownHoldsTheDelete covers a teardown that failed stopping the delete
// rather than losing the row and the worktree with it.
func TestAFailedPreTeardownHoldsTheDelete(t *testing.T) {
	hermeticGit(t)

	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(writeTeardownHook(t,
		instance, "#!/bin/sh\necho boom >&2\nexit 1\n")))
	require.NoError(t, err)

	created, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "fix the flaky login test",
		Title:      "login",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)

	id := created.Msg.GetAgent().GetId()
	tree := created.Msg.GetAgent().GetCwd()

	_, err = instance.Agents.DeleteAgents(ctx, connect.NewRequest(&agentv1.DeleteAgentsRequest{Ids: []string{id}}))
	require.NoError(t, err)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		agent, err := instance.Stores.Agents.GetByID(ctx, id)
		assert.NoError(c, err)
		if !assert.NotNil(c, agent) {
			return
		}

		assert.Equal(c, agentstore.StateFailedToCleanup, agent.State)
	}, 10*time.Second, 10*time.Millisecond)

	assert.DirExists(t, tree, "the directory the next attempt needs is left where it was")
}

// TestPreTeardownIsSkippedOnceTheDirectoryHasGone covers a row whose worktree has gone being
// deletable rather than stuck on a hook that can never run.
func TestPreTeardownIsSkippedOnceTheDirectoryHasGone(t *testing.T) {
	hermeticGit(t)

	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	ran := filepath.Join(t.TempDir(), "ran")
	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(writeTeardownHook(t,
		instance, "#!/bin/sh\ntouch "+ran+"\nexit 1\n")))
	require.NoError(t, err)

	created, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "fix the flaky login test",
		Title:      "login",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)

	id := created.Msg.GetAgent().GetId()
	require.NoError(t, os.RemoveAll(created.Msg.GetAgent().GetCwd()))

	_, err = instance.Agents.DeleteAgents(ctx, connect.NewRequest(&agentv1.DeleteAgentsRequest{Ids: []string{id}}))
	require.NoError(t, err)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		_, err := instance.Stores.Agents.GetByID(ctx, id)
		assert.Error(c, err, "the row goes rather than sticking on a hook that can never run")
	}, 10*time.Second, 10*time.Millisecond)

	assert.NoFileExists(t, ran, "and the hook was never started")
}

// TestOverlappingDeletesRunThePreTeardownOnce covers two deletes of one agent running the
// hook once between them.
func TestOverlappingDeletesRunThePreTeardownOnce(t *testing.T) {
	hermeticGit(t)

	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	counted := filepath.Join(t.TempDir(), "counted")
	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(writeTeardownHook(t,
		instance, "#!/bin/sh\necho run >>"+counted+"\nsleep 1\n")))
	require.NoError(t, err)

	created, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "fix the flaky login test",
		Title:      "login",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)

	id := created.Msg.GetAgent().GetId()
	request := &agentv1.DeleteAgentsRequest{Ids: []string{id}}

	_, err = instance.Agents.DeleteAgents(ctx, connect.NewRequest(request))
	require.NoError(t, err)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.FileExists(c, counted, "the first delete has the hook running")
	}, 10*time.Second, 10*time.Millisecond)

	_, err = instance.Agents.DeleteAgents(ctx, connect.NewRequest(request))
	require.NoError(t, err, "a second delete while the teardown runs is answered rather than refused")

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		_, err := instance.Stores.Agents.GetByID(ctx, id)
		assert.Error(c, err)
	}, 20*time.Second, 10*time.Millisecond)

	runs, err := os.ReadFile(counted)
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(runs), "run"), "the hook ran once for the two deletes")
}
