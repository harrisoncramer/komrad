//go:build integration

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"komrad/internal/komradtest"
	agentstore "komrad/internal/store/agent"
	workflowstore "komrad/internal/store/workflow"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeHook writes a hook named for its moment and returns the request for a worktree workflow
// that hangs it there.
func writeHook(t *testing.T, instance *komradtest.Instance, event, body string) *workflowv1.CreateWorkflowRequest {
	t.Helper()

	name := event + "-hook"
	require.NoError(t, os.WriteFile(filepath.Join(instance.HooksDir, name), []byte(body), 0o755))

	request := buildWorktreeWorkflow(t)
	request.Hooks = []*workflowv1.WorkflowHook{{Event: event, Script: name}}

	return request
}

// TestIntegrationPreDeleteRunsWhileTheWorktreeIsStillThere covers the hook reading the worktree before
// the deprovisioner hands it back.
func TestIntegrationPreDeleteRunsWhileTheWorktreeIsStillThere(t *testing.T) {
	isolateGit(t)

	instance := komradtest.Start(t, komradtest.Params{})
	ctx := t.Context()

	seen := filepath.Join(t.TempDir(), "seen")
	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(writeHook(t,
		instance, workflowstore.HookPreDelete, "#!/bin/sh\nls .git >"+seen+"\n")))
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

// TestIntegrationAFailedPreDeleteHoldsTheDelete covers a teardown that failed stopping the delete
// rather than losing the row and the worktree with it.
func TestIntegrationAFailedPreDeleteHoldsTheDelete(t *testing.T) {
	isolateGit(t)

	instance := komradtest.Start(t, komradtest.Params{})
	ctx := t.Context()

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(writeHook(t,
		instance, workflowstore.HookPreDelete, "#!/bin/sh\necho boom >&2\nexit 1\n")))
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

// TestIntegrationPreDeleteIsSkippedOnceTheDirectoryHasGone covers a row whose worktree has gone being
// deletable rather than stuck on a hook that can never run.
func TestIntegrationPreDeleteIsSkippedOnceTheDirectoryHasGone(t *testing.T) {
	isolateGit(t)

	instance := komradtest.Start(t, komradtest.Params{})
	ctx := t.Context()

	ran := filepath.Join(t.TempDir(), "ran")
	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(writeHook(t,
		instance, workflowstore.HookPreDelete, "#!/bin/sh\ntouch "+ran+"\nexit 1\n")))
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

// TestIntegrationOverlappingDeletesRunThePreDeleteOnce covers two deletes of one agent running the
// hook once between them.
func TestIntegrationOverlappingDeletesRunThePreDeleteOnce(t *testing.T) {
	isolateGit(t)

	instance := komradtest.Start(t, komradtest.Params{})
	ctx := t.Context()

	counted := filepath.Join(t.TempDir(), "counted")
	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(writeHook(t,
		instance, workflowstore.HookPreDelete, "#!/bin/sh\necho run >>"+counted+"\nsleep 1\n")))
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

func createHookedAgent(t *testing.T, instance *komradtest.Instance, request *workflowv1.CreateWorkflowRequest) *agentv1.Agent {
	t.Helper()

	ctx := t.Context()

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(request))
	require.NoError(t, err)

	created, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "fix the flaky login test",
		Title:      "login",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)

	return created.Msg.GetAgent()
}

func realPath(t *testing.T, path string) string {
	t.Helper()

	resolved, err := filepath.EvalSymlinks(path)
	require.NoError(t, err)

	return resolved
}

// TestIntegrationPostArchiveRunsInTheWorktreeOnceTheAgentIsArchived covers the hook that gives back what an agent brought up.
func TestIntegrationPostArchiveRunsInTheWorktreeOnceTheAgentIsArchived(t *testing.T) {
	isolateGit(t)

	instance := komradtest.Start(t, komradtest.Params{})
	ctx := t.Context()

	seen := filepath.Join(t.TempDir(), "seen")
	agent := createHookedAgent(t, instance, writeHook(t, instance, workflowstore.HookPostArchive,
		"#!/bin/sh\npwd -P >"+seen+"\n"))

	_, err := instance.Agents.ArchiveAgents(ctx, connect.NewRequest(&agentv1.ArchiveAgentsRequest{Ids: []string{agent.GetId()}}))
	require.NoError(t, err)

	archived, err := instance.Stores.Agents.GetByID(ctx, agent.GetId())
	require.NoError(t, err)
	assert.NotNil(t, archived.ArchivedAt, "the archive does not wait for the hook")

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		ran, err := os.ReadFile(seen)
		assert.NoError(c, err)
		assert.Equal(c, realPath(t, agent.GetCwd()), strings.TrimSpace(string(ran)), "the hook runs in the worktree")
	}, 10*time.Second, 10*time.Millisecond)
}

// TestIntegrationAFailedPreArchiveKeepsTheAgentLive covers a pre-archive hook refusing the archive.
func TestIntegrationAFailedPreArchiveKeepsTheAgentLive(t *testing.T) {
	isolateGit(t)

	instance := komradtest.Start(t, komradtest.Params{})
	ctx := t.Context()

	agent := createHookedAgent(t, instance, writeHook(t, instance, workflowstore.HookPreArchive,
		"#!/bin/sh\necho uncommitted work >&2\nexit 1\n"))

	_, err := instance.Agents.ArchiveAgents(ctx, connect.NewRequest(&agentv1.ArchiveAgentsRequest{Ids: []string{agent.GetId()}}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	assert.Contains(t, err.Error(), "uncommitted work", "the refusal carries what the hook said")

	live, err := instance.Stores.Agents.GetByID(ctx, agent.GetId())
	require.NoError(t, err)
	assert.Nil(t, live.ArchivedAt, "the agent stays live")
	assert.NotEmpty(t, live.Pane, "with its pane")
}

// TestIntegrationADeleteWaitsForThePostArchiveStillRunning covers archive and delete pressed together.
func TestIntegrationADeleteWaitsForThePostArchiveStillRunning(t *testing.T) {
	isolateGit(t)

	instance := komradtest.Start(t, komradtest.Params{})
	ctx := t.Context()

	seen := filepath.Join(t.TempDir(), "seen")
	agent := createHookedAgent(t, instance, writeHook(t, instance, workflowstore.HookPostArchive,
		"#!/bin/sh\nsleep 1\nls .git >"+seen+"\n"))
	ids := []string{agent.GetId()}

	_, err := instance.Agents.ArchiveAgents(ctx, connect.NewRequest(&agentv1.ArchiveAgentsRequest{Ids: ids}))
	require.NoError(t, err)
	_, err = instance.Agents.DeleteAgents(ctx, connect.NewRequest(&agentv1.DeleteAgentsRequest{Ids: ids}))
	require.NoError(t, err)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		_, err := instance.Stores.Agents.GetByID(ctx, agent.GetId())
		assert.Error(c, err, "the row goes once the hook and the deprovisioner have both finished")
		assert.NoDirExists(c, agent.GetCwd())
	}, 20*time.Second, 10*time.Millisecond)

	landed, err := os.ReadFile(seen)
	require.NoError(t, err, "the post-archive hook finished")
	assert.NotEmpty(t, landed, "with the worktree still under it")
}

// TestIntegrationPostDeleteRunsOnceTheWorktreeIsGone covers the last hook of an agent's life running somewhere that still exists.
func TestIntegrationPostDeleteRunsOnceTheWorktreeIsGone(t *testing.T) {
	isolateGit(t)

	instance := komradtest.Start(t, komradtest.Params{})
	ctx := t.Context()

	seen := filepath.Join(t.TempDir(), "seen")
	agent := createHookedAgent(t, instance, writeHook(t, instance, workflowstore.HookPostDelete,
		"#!/bin/sh\n{ pwd -P; echo \"$KOMRAD_AGENT_CWD\"; test -d \"$KOMRAD_AGENT_CWD\" && echo there || echo gone; } >"+seen+"\n"))

	_, err := instance.Agents.DeleteAgents(ctx, connect.NewRequest(&agentv1.DeleteAgentsRequest{Ids: []string{agent.GetId()}}))
	require.NoError(t, err)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		ran, err := os.ReadFile(seen)
		if !assert.NoError(c, err) {
			return
		}

		assert.Equal(c, []string{realPath(t, instance.HooksDir), agent.GetCwd(), "gone"},
			strings.Split(strings.TrimSpace(string(ran)), "\n"),
			"the hook runs in the hooks directory, told where the worktree was")
	}, 10*time.Second, 10*time.Millisecond)

	_, err = instance.Stores.Agents.GetByID(ctx, agent.GetId())
	assert.Error(t, err, "the row had gone before the hook ran")
}

func everyMoment() []string {
	return []string{
		workflowstore.HookPreLaunch,
		workflowstore.HookPostLaunch,
		workflowstore.HookPreResume,
		workflowstore.HookPostResume,
		workflowstore.HookPreArchive,
		workflowstore.HookPostArchive,
		workflowstore.HookPreRestore,
		workflowstore.HookPostRestore,
		workflowstore.HookPreDelete,
		workflowstore.HookPostDelete,
	}
}

func hangOnEvery(t *testing.T, instance *komradtest.Instance, name, body string) *workflowv1.CreateWorkflowRequest {
	t.Helper()

	require.NoError(t, os.WriteFile(filepath.Join(instance.HooksDir, name), []byte(body), 0o755))

	request := buildWorktreeWorkflow(t)
	request.Hooks = xslice.Map(everyMoment(), func(moment string) *workflowv1.WorkflowHook {
		return &workflowv1.WorkflowHook{Event: moment, Script: name}
	})

	return request
}

func waitForMoments(t *testing.T, path string, want ...string) {
	t.Helper()

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		landed, err := os.ReadFile(path)
		if !assert.NoError(c, err) {
			return
		}

		assert.Equal(c, want, strings.Fields(string(landed)))
	}, 20*time.Second, 10*time.Millisecond)
}

// TestIntegrationEveryMomentFiresInOrderOverAnAgentsLife walks one agent from launch to delete.
func TestIntegrationEveryMomentFiresInOrderOverAnAgentsLife(t *testing.T) {
	isolateGit(t)

	instance := komradtest.Start(t, komradtest.Params{})
	ctx := t.Context()

	record := filepath.Join(t.TempDir(), "moments")
	agent := createHookedAgent(t, instance, hangOnEvery(t, instance, "record",
		"#!/bin/sh\necho \"$KOMRAD_TRIGGER\" >>"+record+"\n"))
	ids := []string{agent.GetId()}

	seen := []string{workflowstore.HookPreLaunch, workflowstore.HookPostLaunch}
	waitForMoments(t, record, seen...)

	_, err := instance.Agents.ArchiveAgents(ctx, connect.NewRequest(&agentv1.ArchiveAgentsRequest{Ids: ids}))
	require.NoError(t, err)
	seen = append(seen, workflowstore.HookPreArchive, workflowstore.HookPostArchive)
	waitForMoments(t, record, seen...)

	_, err = instance.Agents.RestoreAgents(ctx, connect.NewRequest(&agentv1.RestoreAgentsRequest{Ids: ids}))
	require.NoError(t, err)
	seen = append(seen, workflowstore.HookPreRestore, workflowstore.HookPostRestore)
	waitForMoments(t, record, seen...)

	_, err = instance.Agents.ResumeAgent(ctx, connect.NewRequest(&agentv1.ResumeAgentRequest{Id: agent.GetId()}))
	require.NoError(t, err)
	seen = append(seen, workflowstore.HookPreResume, workflowstore.HookPostResume)
	waitForMoments(t, record, seen...)

	_, err = instance.Agents.DeleteAgents(ctx, connect.NewRequest(&agentv1.DeleteAgentsRequest{Ids: ids}))
	require.NoError(t, err)
	seen = append(seen, workflowstore.HookPreDelete, workflowstore.HookPostDelete)
	waitForMoments(t, record, seen...)
}

// TestIntegrationAFailedPreResumeLeavesTheAgentDormant covers a veto on the way back into a pane.
func TestIntegrationAFailedPreResumeLeavesTheAgentDormant(t *testing.T) {
	isolateGit(t)

	instance := komradtest.Start(t, komradtest.Params{})
	ctx := t.Context()

	agent := createHookedAgent(t, instance, writeHook(t, instance, workflowstore.HookPreResume,
		"#!/bin/sh\necho no slot free >&2\nexit 1\n"))
	ids := []string{agent.GetId()}

	_, err := instance.Agents.ArchiveAgents(ctx, connect.NewRequest(&agentv1.ArchiveAgentsRequest{Ids: ids}))
	require.NoError(t, err)
	_, err = instance.Agents.RestoreAgents(ctx, connect.NewRequest(&agentv1.RestoreAgentsRequest{Ids: ids}))
	require.NoError(t, err)

	_, err = instance.Agents.ResumeAgent(ctx, connect.NewRequest(&agentv1.ResumeAgentRequest{Id: agent.GetId()}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no slot free", "the refusal carries what the hook said")
	assert.Empty(t, instance.GetOwnedPane(agent.GetId()), "no pane was opened")
}

// TestIntegrationAFailedPreRestoreKeepsTheAgentArchived covers a veto on the way out of the archive.
func TestIntegrationAFailedPreRestoreKeepsTheAgentArchived(t *testing.T) {
	isolateGit(t)

	instance := komradtest.Start(t, komradtest.Params{})
	ctx := t.Context()

	agent := createHookedAgent(t, instance, writeHook(t, instance, workflowstore.HookPreRestore,
		"#!/bin/sh\necho slot taken >&2\nexit 1\n"))
	ids := []string{agent.GetId()}

	_, err := instance.Agents.ArchiveAgents(ctx, connect.NewRequest(&agentv1.ArchiveAgentsRequest{Ids: ids}))
	require.NoError(t, err)

	_, err = instance.Agents.RestoreAgents(ctx, connect.NewRequest(&agentv1.RestoreAgentsRequest{Ids: ids}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "slot taken")

	archived, err := instance.Stores.Agents.GetByID(ctx, agent.GetId())
	require.NoError(t, err)
	assert.NotNil(t, archived.ArchivedAt, "the agent stays in the archive")
}

// TestIntegrationAFailedPostArchiveOnlyMarksItsRun covers a post hook that cannot undo its step.
func TestIntegrationAFailedPostArchiveOnlyMarksItsRun(t *testing.T) {
	isolateGit(t)

	instance := komradtest.Start(t, komradtest.Params{})
	ctx := t.Context()

	agent := createHookedAgent(t, instance, writeHook(t, instance, workflowstore.HookPostArchive,
		"#!/bin/sh\necho could not give the slot back >&2\nexit 1\n"))

	_, err := instance.Agents.ArchiveAgents(ctx, connect.NewRequest(&agentv1.ArchiveAgentsRequest{Ids: []string{agent.GetId()}}))
	require.NoError(t, err)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		got, err := instance.Agents.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: agent.GetId()}))
		if !assert.NoError(c, err) {
			return
		}

		assert.True(c, got.Msg.GetAgent().GetFailedRun(), "the run is marked failed")
		assert.NotEmpty(c, got.Msg.GetAgent().GetArchivedAt(), "and the agent stays archived")
	}, 10*time.Second, 10*time.Millisecond)
}
