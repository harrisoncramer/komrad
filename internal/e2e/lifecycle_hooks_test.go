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
	"komrad/lib/protoenum"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	eventv1 "komrad/proto/gen/event/v1"
	taskv1 "komrad/proto/gen/task/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeHook writes a hook named for its moment and returns the request for a worktree workflow
// that hangs it there.
func writeHook(t *testing.T, instance *komradtest.Instance, event eventv1.Moment, body string) *workflowv1.CreateWorkflowRequest {
	t.Helper()

	name := protoenum.Name(event) + "-hook"
	require.NoError(t, os.WriteFile(filepath.Join(instance.HooksDir, name), []byte(body), 0o755))

	request := buildWorktreeWorkflow(t)
	request.Hooks = []*workflowv1.WorkflowHook{{Event: event, Script: name}}

	return request
}

// TestIntegrationPreDeleteRunsWhileTheWorktreeIsStillThere covers the hook reading the worktree before
// the task's delete hands it back.
func TestIntegrationPreDeleteRunsWhileTheWorktreeIsStillThere(t *testing.T) {
	isolateGit(t)

	instance := komradtest.Start(t, komradtest.Params{})
	ctx := t.Context()

	seen := filepath.Join(t.TempDir(), "seen")
	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(writeHook(t,
		instance, eventv1.Moment_MOMENT_AGENT_PRE_DELETE, "#!/bin/sh\nls .git >"+seen+"\n")))
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

	_, err = instance.Tasks.DeleteTasks(ctx, connect.NewRequest(&taskv1.DeleteTasksRequest{TaskIds: []string{created.Msg.GetAgent().GetTaskId()}, Records: true}))
	require.NoError(t, err)
	instance.WorkQueue()

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		_, err := instance.Stores.Agents.GetByID(ctx, uuid.MustParse(id))
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
		instance, eventv1.Moment_MOMENT_AGENT_PRE_DELETE, "#!/bin/sh\necho boom >&2\nexit 1\n")))
	require.NoError(t, err)

	created, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "fix the flaky login test",
		Title:      "login",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)

	id := created.Msg.GetAgent().GetId()
	tree := created.Msg.GetAgent().GetCwd()

	_, err = instance.Tasks.DeleteTasks(ctx, connect.NewRequest(&taskv1.DeleteTasksRequest{TaskIds: []string{created.Msg.GetAgent().GetTaskId()}, Records: true}))
	require.NoError(t, err)
	instance.WorkQueue()

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		agent, err := instance.Stores.Agents.GetByID(ctx, uuid.MustParse(id))
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
		instance, eventv1.Moment_MOMENT_AGENT_PRE_DELETE, "#!/bin/sh\ntouch "+ran+"\nexit 1\n")))
	require.NoError(t, err)

	created, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "fix the flaky login test",
		Title:      "login",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)

	id := created.Msg.GetAgent().GetId()
	require.NoError(t, os.RemoveAll(created.Msg.GetAgent().GetCwd()))

	_, err = instance.Tasks.DeleteTasks(ctx, connect.NewRequest(&taskv1.DeleteTasksRequest{TaskIds: []string{created.Msg.GetAgent().GetTaskId()}, Records: true}))
	require.NoError(t, err)
	instance.WorkQueue()

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		_, err := instance.Stores.Agents.GetByID(ctx, uuid.MustParse(id))
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
		instance, eventv1.Moment_MOMENT_AGENT_PRE_DELETE, "#!/bin/sh\necho run >>"+counted+"\nsleep 1\n")))
	require.NoError(t, err)

	created, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "fix the flaky login test",
		Title:      "login",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)

	id := created.Msg.GetAgent().GetId()
	request := &taskv1.DeleteTasksRequest{TaskIds: []string{created.Msg.GetAgent().GetTaskId()}, Records: true}

	instance.WorkQueue()

	_, err = instance.Tasks.DeleteTasks(ctx, connect.NewRequest(request))
	require.NoError(t, err)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.FileExists(c, counted, "the first delete has the hook running")
	}, 10*time.Second, 10*time.Millisecond)

	_, err = instance.Tasks.DeleteTasks(ctx, connect.NewRequest(request))
	require.NoError(t, err, "a second delete while the teardown runs is answered rather than refused")

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		_, err := instance.Stores.Agents.GetByID(ctx, uuid.MustParse(id))
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
	agent := createHookedAgent(t, instance, writeHook(t, instance, eventv1.Moment_MOMENT_AGENT_POST_ARCHIVE,
		"#!/bin/sh\npwd -P >"+seen+"\n"))

	_, err := instance.Tasks.ArchiveTask(ctx, connect.NewRequest(&taskv1.ArchiveTaskRequest{Task: &taskv1.ArchiveTaskRequest_TaskId{TaskId: agent.GetTaskId()}}))
	require.NoError(t, err)

	archived, err := instance.Stores.Agents.GetByID(ctx, uuid.MustParse(agent.GetId()))
	require.NoError(t, err)
	assert.NotNil(t, archived.ArchivedAt, "the archive does not wait for the hook")

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		ran, err := os.ReadFile(seen)
		assert.NoError(c, err)
		assert.Equal(c, realPath(t, agent.GetCwd()), strings.TrimSpace(string(ran)), "the hook runs in the worktree")
	}, 10*time.Second, 10*time.Millisecond)
}

// TestIntegrationPostDeleteRunsOnceTheWorktreeIsGone covers the last hook of a deleted task's agent running somewhere that still exists.
func TestIntegrationPostDeleteRunsOnceTheWorktreeIsGone(t *testing.T) {
	isolateGit(t)

	instance := komradtest.Start(t, komradtest.Params{})
	ctx := t.Context()

	seen := filepath.Join(t.TempDir(), "seen")
	agent := createHookedAgent(t, instance, writeHook(t, instance, eventv1.Moment_MOMENT_AGENT_POST_DELETE,
		"#!/bin/sh\n{ pwd -P; echo \"$KOMRAD_AGENT_CWD\"; test -d \"$KOMRAD_AGENT_CWD\" && echo there || echo gone; } >"+seen+"\n"))

	_, err := instance.Tasks.DeleteTasks(ctx, connect.NewRequest(&taskv1.DeleteTasksRequest{TaskIds: []string{agent.GetTaskId()}, Records: true}))
	require.NoError(t, err)
	instance.WorkQueue()

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		ran, err := os.ReadFile(seen)
		if !assert.NoError(c, err) {
			return
		}

		assert.Equal(c, []string{realPath(t, instance.HooksDir), agent.GetCwd(), "gone"},
			strings.Split(strings.TrimSpace(string(ran)), "\n"),
			"the hook runs in the hooks directory, told where the worktree was")
	}, 10*time.Second, 10*time.Millisecond)

	_, err = instance.Stores.Agents.GetByID(ctx, uuid.MustParse(agent.GetId()))
	assert.Error(t, err, "the row had gone before the hook ran")
}

func everyMoment() []eventv1.Moment {
	return []eventv1.Moment{
		eventv1.Moment_MOMENT_AGENT_PRE_LAUNCH,
		eventv1.Moment_MOMENT_AGENT_POST_LAUNCH,
		eventv1.Moment_MOMENT_AGENT_PRE_RESUME,
		eventv1.Moment_MOMENT_AGENT_POST_RESUME,
		eventv1.Moment_MOMENT_AGENT_PRE_ARCHIVE,
		eventv1.Moment_MOMENT_AGENT_POST_ARCHIVE,
		eventv1.Moment_MOMENT_AGENT_PRE_RESTORE,
		eventv1.Moment_MOMENT_AGENT_POST_RESTORE,
		eventv1.Moment_MOMENT_AGENT_PRE_DELETE,
		eventv1.Moment_MOMENT_AGENT_POST_DELETE,
	}
}

func hangOnEvery(t *testing.T, instance *komradtest.Instance, name, body string) *workflowv1.CreateWorkflowRequest {
	t.Helper()

	require.NoError(t, os.WriteFile(filepath.Join(instance.HooksDir, name), []byte(body), 0o755))

	request := buildWorktreeWorkflow(t)
	request.Hooks = xslice.Map(everyMoment(), func(moment eventv1.Moment) *workflowv1.WorkflowHook {
		return &workflowv1.WorkflowHook{Event: moment, Script: name}
	})

	return request
}

func waitForMoments(t *testing.T, path string, want ...eventv1.Moment) {
	t.Helper()

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		landed, err := os.ReadFile(path)
		if !assert.NoError(c, err) {
			return
		}

		assert.Equal(c, protoenum.Names(want), strings.Fields(string(landed)))
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

	seen := []eventv1.Moment{eventv1.Moment_MOMENT_AGENT_PRE_LAUNCH, eventv1.Moment_MOMENT_AGENT_POST_LAUNCH}
	waitForMoments(t, record, seen...)

	_, err := instance.Tasks.ArchiveTask(ctx, connect.NewRequest(&taskv1.ArchiveTaskRequest{Task: &taskv1.ArchiveTaskRequest_TaskId{TaskId: agent.GetTaskId()}}))
	require.NoError(t, err)
	seen = append(seen, eventv1.Moment_MOMENT_AGENT_PRE_ARCHIVE, eventv1.Moment_MOMENT_AGENT_POST_ARCHIVE)
	waitForMoments(t, record, seen...)

	_, err = instance.Agents.RestoreAgents(ctx, connect.NewRequest(&agentv1.RestoreAgentsRequest{Ids: ids}))
	require.NoError(t, err)
	seen = append(seen, eventv1.Moment_MOMENT_AGENT_PRE_RESTORE, eventv1.Moment_MOMENT_AGENT_POST_RESTORE)
	waitForMoments(t, record, seen...)

	_, err = instance.Agents.ResumeAgent(ctx, connect.NewRequest(&agentv1.ResumeAgentRequest{Id: agent.GetId()}))
	require.NoError(t, err)
	seen = append(seen, eventv1.Moment_MOMENT_AGENT_PRE_RESUME, eventv1.Moment_MOMENT_AGENT_POST_RESUME)
	waitForMoments(t, record, seen...)

	_, err = instance.Tasks.DeleteTasks(ctx, connect.NewRequest(&taskv1.DeleteTasksRequest{TaskIds: []string{agent.GetTaskId()}, Records: true}))
	require.NoError(t, err)
	instance.WorkQueue()
	seen = append(seen, eventv1.Moment_MOMENT_AGENT_PRE_ARCHIVE, eventv1.Moment_MOMENT_AGENT_POST_ARCHIVE,
		eventv1.Moment_MOMENT_AGENT_PRE_DELETE, eventv1.Moment_MOMENT_AGENT_POST_DELETE)
	waitForMoments(t, record, seen...)
}

// TestIntegrationAFailedPostArchiveOnlyMarksItsRun covers a post hook that cannot undo its step.
func TestIntegrationAFailedPostArchiveOnlyMarksItsRun(t *testing.T) {
	isolateGit(t)

	instance := komradtest.Start(t, komradtest.Params{})
	ctx := t.Context()

	agent := createHookedAgent(t, instance, writeHook(t, instance, eventv1.Moment_MOMENT_AGENT_POST_ARCHIVE,
		"#!/bin/sh\necho could not give the slot back >&2\nexit 1\n"))

	_, err := instance.Tasks.ArchiveTask(ctx, connect.NewRequest(&taskv1.ArchiveTaskRequest{Task: &taskv1.ArchiveTaskRequest_TaskId{TaskId: agent.GetTaskId()}}))
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
