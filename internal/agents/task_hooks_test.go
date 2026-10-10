package agents

import (
	"errors"
	"os"
	"testing"
	"time"

	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/tmuxtest"
	"komrad/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const taskHook = "task-hook"

// taskHookConfig is a workflow hanging taskHook on one of a task's moments, with a worktree or without.
func taskHookConfig(t *testing.T, event string, worktree bool) workflowstore.Config {
	t.Helper()

	config := workflowstore.Config{
		Name:  "backend",
		Cwd:   fakeCheckout(t),
		Hooks: []workflowstore.Hook{{Event: event, Script: taskHook}},
	}
	if worktree {
		config.Worktree = workflowstore.WorktreeConfig{Enabled: true, BaseBranch: "main"}
	}

	return config
}

func launchOnTask(t *testing.T, fx *taskDeleteFixture, params NewAgentParams) (*agentstore.Agent, error) {
	t.Helper()

	params.WorkflowID = fx.workflow.ID
	if params.ParentAgentID == "" {
		params.Dir = t.TempDir()
	}

	return fx.agents.launch.NewAgent(t.Context(), params)
}

func waitForTaskHooks(t *testing.T, lifecycle *fakeLifecycle, want int) []agentstore.Agent {
	t.Helper()

	require.Eventually(t, func() bool { return len(lifecycle.ranTaskHooks()) >= want }, 5*time.Second, 10*time.Millisecond)
	require.Never(t, func() bool { return len(lifecycle.ranTaskHooks()) > want }, 300*time.Millisecond, 20*time.Millisecond)

	return lifecycle.ranTaskHooks()
}

// TestTaskPreOpen covers task.pre-open on a create, in the task's worktree when it has one.
func TestTaskPreOpen(t *testing.T) {
	tests := []struct {
		name           string
		worktree       bool
		hookErr        error
		join           bool
		wantErr        bool
		wantOpens      int
		wantInWorktree bool
	}{
		{
			name:      "a failed pre-open abandons the create",
			worktree:  true,
			hookErr:   errors.New("cache is cold"),
			wantErr:   true,
			wantOpens: 1,
		},
		{
			name:           "a reviewer joining its parent's task does not open it",
			worktree:       true,
			join:           true,
			wantOpens:      1,
			wantInWorktree: true,
		},
		{
			name:      "a task with no worktree opens in the workflow's directory",
			wantOpens: 1,
		},
		{
			name:           "a task with a worktree opens in it",
			worktree:       true,
			wantOpens:      1,
			wantInWorktree: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provisioned := t.TempDir()
			lifecycle := &fakeLifecycle{
				dir:      provisioned,
				hookErrs: map[string]error{taskHook: tt.hookErr},
			}
			fx := newTaskDeleteFixture(t, lifecycle, taskHookConfig(t, workflowstore.HookTaskPreOpen, tt.worktree))

			agent, err := launchOnTask(t, fx, NewAgentParams{Title: "auth"})
			if tt.join {
				require.NoError(t, err)
				_, err = launchOnTask(t, fx, childOf(*agent, "reviewer"))
			}
			if tt.wantErr {
				require.ErrorContains(t, err, "cache is cold")
				listed, listErr := fx.st.Agents.List(t.Context())
				require.NoError(t, listErr)
				require.Len(t, listed, 1)
				assert.Equal(t, agentstore.StateFailedToCreate, listed[0].State)
				assert.Equal(t, []string{worktreeDeprovisioner}, lifecycle.ranDeprovisions(), "what setup built is given back")
			} else {
				require.NoError(t, err)
			}

			ran := lifecycle.ranTaskHooks()
			require.Len(t, ran, tt.wantOpens)
			opened := ran[0]
			if tt.wantInWorktree {
				assert.Equal(t, provisioned, opened.Cwd)
				assert.Nil(t, opened.ProvisionEnabled, "the worktree's variables reach the hook")
			} else if !tt.wantErr {
				assert.Equal(t, fx.workflow.Cwd, opened.Cwd)
				assert.Equal(t, xptr.Ptr(false), opened.ProvisionEnabled, "no worktree's variables reach the hook")
			}
			if agent != nil {
				assert.Equal(t, agent.ID, opened.ID, "the hook runs as the agent that opened the task")
			}
		})
	}
}

// TestRetryOpensTheTaskAFailedPreOpenLeft pins that an agent which failed to create holds no task open.
func TestRetryOpensTheTaskAFailedPreOpenLeft(t *testing.T) {
	lifecycle := &fakeLifecycle{
		dir:      t.TempDir(),
		hookErrs: map[string]error{taskHook: errors.New("cache is cold")},
	}
	fx := newTaskDeleteFixture(t, lifecycle, taskHookConfig(t, workflowstore.HookTaskPreOpen, true))
	_, err := launchOnTask(t, fx, NewAgentParams{Title: "auth"})
	require.Error(t, err)
	failed, err := fx.st.Agents.List(t.Context())
	require.NoError(t, err)
	require.Len(t, failed, 1)

	lifecycle.hookErrs = nil
	_, err = fx.agents.launch.RetryCreate(t.Context(), RetryCreateParams{ID: failed[0].ID})
	require.NoError(t, err)

	ran := lifecycle.ranTaskHooks()
	require.Len(t, ran, 2, "the retry opens the task again")
	assert.Equal(t, failed[0].ID, ran[1].ID)
}

// TestJoinOpensTheTaskItRestores covers a writer joining an archived task's checkout running task.pre-open there.
func TestJoinOpensTheTaskItRestores(t *testing.T) {
	tests := []struct {
		name         string
		hookErr      error
		wantArchived bool
	}{
		{
			name:         "a failed pre-open archives the task again",
			hookErr:      errors.New("cache is cold"),
			wantArchived: true,
		},
		{
			name: "a join that restores the task opens it in the worktree",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			provisioned := t.TempDir()
			lifecycle := &fakeLifecycle{dir: provisioned}
			fx := newTaskDeleteFixture(t, lifecycle, taskHookConfig(t, workflowstore.HookTaskPreOpen, true))
			first, err := launchOnTask(t, fx, NewAgentParams{Title: "auth"})
			require.NoError(t, err)
			require.NoError(t, fx.agents.lifecycle.ArchiveAgents(ctx, ArchiveAgentsParams{IDs: []string{first.ID}}))
			lifecycle.hookErrs = map[string]error{taskHook: tt.hookErr}

			next, err := fx.agents.launch.NewAgent(ctx, NewAgentParams{Title: "next", Prompt: "carry on", WorkflowID: fx.workflow.ID, Task: *first.TaskID})

			task, getErr := fx.st.Tasks.Get(ctx, *first.TaskID)
			require.NoError(t, getErr)
			ran := lifecycle.ranTaskHooks()
			require.Len(t, ran, 2, "the join opens the task again")
			assert.Equal(t, provisioned, ran[1].Cwd)
			if tt.wantArchived {
				require.ErrorContains(t, err, "cache is cold")
				assert.NotNil(t, task.ArchivedAt)

				return
			}
			require.NoError(t, err)
			assert.Nil(t, task.ArchivedAt)
			assert.Equal(t, next.ID, ran[1].ID, "the hook runs as the joining writer")
		})
	}
}

// TestTaskPostArchive covers task.post-archive firing once a task's last live agent is archived.
func TestTaskPostArchive(t *testing.T) {
	tests := []struct {
		name        string
		worktree    bool
		archive     func(t *testing.T, fx *taskDeleteFixture, lifecycle *fakeLifecycle, ids []string) error
		wantCwd     func(fx *taskDeleteFixture, writer agentstore.Agent) string
		wantNoneRun bool
	}{
		{
			name:     "archiving the task's reviewer leaves it open",
			worktree: true,
			archive: func(t *testing.T, fx *taskDeleteFixture, _ *fakeLifecycle, ids []string) error {
				t.Helper()

				return fx.agents.lifecycle.ArchiveAgents(t.Context(), ArchiveAgentsParams{IDs: ids[1:]})
			},
			wantNoneRun: true,
		},
		{
			name: "a task with no worktree runs agent.post-archive in the workflow's directory",
			archive: func(t *testing.T, fx *taskDeleteFixture, _ *fakeLifecycle, ids []string) error {
				t.Helper()

				return fx.agents.lifecycle.ArchiveAgents(t.Context(), ArchiveAgentsParams{IDs: ids})
			},
			wantCwd: func(fx *taskDeleteFixture, _ agentstore.Agent) string { return fx.workflow.Cwd },
		},
		{
			name:     "archiving the last agents runs agent.post-archive once, in the worktree",
			worktree: true,
			archive: func(t *testing.T, fx *taskDeleteFixture, _ *fakeLifecycle, ids []string) error {
				t.Helper()

				return fx.agents.lifecycle.ArchiveAgents(t.Context(), ArchiveAgentsParams{IDs: ids})
			},
			wantCwd: func(_ *taskDeleteFixture, writer agentstore.Agent) string { return writer.Cwd },
		},
		{
			name:     "archiving the workflow runs agent.post-archive",
			worktree: true,
			archive: func(t *testing.T, fx *taskDeleteFixture, lifecycle *fakeLifecycle, _ []string) error {
				t.Helper()

				return fx.st.workflowService(workflowServiceOptions{tmux: tmuxtest.NewFake(), lifecycle: lifecycle}).ArchiveWorkflow(t.Context(), fx.workflow.ID)
			},
			wantCwd: func(_ *taskDeleteFixture, writer agentstore.Agent) string { return writer.Cwd },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lifecycle := &fakeLifecycle{dir: t.TempDir()}
			fx := newTaskDeleteFixture(t, lifecycle, taskHookConfig(t, workflowstore.HookTaskPostArchive, tt.worktree))
			writer, err := launchOnTask(t, fx, NewAgentParams{Title: "auth"})
			require.NoError(t, err)
			reviewer, err := launchOnTask(t, fx, childOf(*writer, "reviewer"))
			require.NoError(t, err)

			require.NoError(t, tt.archive(t, fx, lifecycle, []string{writer.ID, reviewer.ID}))

			if tt.wantNoneRun {
				waitForTaskHooks(t, lifecycle, 0)
				return
			}
			ran := waitForTaskHooks(t, lifecycle, 1)
			assert.Equal(t, writer.ID, ran[0].ID, "the hook runs as the task's writer rather than its reviewer")
			assert.Equal(t, tt.wantCwd(fx, *writer), ran[0].Cwd)
		})
	}
}

// TestRestoreOpensTheTask pins that a restore runs task.pre-open before it brings an archived task back.
func TestRestoreOpensTheTask(t *testing.T) {
	tests := []struct {
		name         string
		hookErr      error
		wantRestored bool
	}{
		{
			name:    "a failed pre-open refuses the restore",
			hookErr: errors.New("cache is cold"),
		},
		{
			name:         "restoring the task's agent opens it",
			wantRestored: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lifecycle := &fakeLifecycle{dir: t.TempDir()}
			fx := newTaskDeleteFixture(t, lifecycle, taskHookConfig(t, workflowstore.HookTaskPreOpen, true))
			agent, err := launchOnTask(t, fx, NewAgentParams{Title: "auth"})
			require.NoError(t, err)
			require.NoError(t, fx.agents.lifecycle.ArchiveAgents(t.Context(), ArchiveAgentsParams{IDs: []string{agent.ID}}))

			lifecycle.hookErrs = map[string]error{taskHook: tt.hookErr}
			_, err = fx.agents.lifecycle.RestoreAgents(t.Context(), []string{agent.ID})
			if tt.wantRestored {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "cache is cold")
			}

			assert.Equal(t, !tt.wantRestored, agentByID(t, fx.st, agent.ID).ArchivedAt != nil)
			assert.Equal(t, !tt.wantRestored, isTaskArchived(t, fx.st, *agent.TaskID))
			ran := lifecycle.ranTaskHooks()
			require.Len(t, ran, 2, "the create and the restore each open the task")
			assert.Equal(t, agent.ID, ran[1].ID)
			assert.Equal(t, agent.Cwd, ran[1].Cwd)
		})
	}
}

// TestTaskPreOpenWaitsForTheTasksPostArchive pins a task's hooks running one at a time.
func TestTaskPreOpenWaitsForTheTasksPostArchive(t *testing.T) {
	lifecycle := &fakeLifecycle{dir: t.TempDir()}
	config := taskHookConfig(t, workflowstore.HookTaskPreOpen, true)
	config.Hooks = append(config.Hooks, workflowstore.Hook{Event: workflowstore.HookTaskPostArchive, Script: "shrink"})
	fx := newTaskDeleteFixture(t, lifecycle, config)
	agent, err := launchOnTask(t, fx, NewAgentParams{Title: "auth"})
	require.NoError(t, err)

	hold := make(chan struct{})
	lifecycle.holdHooks = hold
	require.NoError(t, fx.agents.lifecycle.ArchiveAgents(t.Context(), ArchiveAgentsParams{IDs: []string{agent.ID}}))

	restored := make(chan error, 1)
	go func() {
		_, err := fx.agents.lifecycle.RestoreAgents(t.Context(), []string{agent.ID})
		restored <- err
	}()
	require.Never(t, func() bool { return len(restored) > 0 }, 200*time.Millisecond, 20*time.Millisecond)

	close(hold)
	require.NoError(t, <-restored)
	assert.Equal(t, []string{
		workflowstore.HookTaskPreOpen,
		workflowstore.HookTaskPostArchive,
		workflowstore.HookTaskPreOpen,
	}, ranEvents(lifecycle))
}

// TestTaskPreDelete covers task.pre-delete running once no agent is left on a task, before its worktree goes.
func TestTaskPreDelete(t *testing.T) {
	tests := []struct {
		name            string
		worktree        bool
		hookErr         error
		deleteAgentOnly bool
		removeWorktree  bool
		wantDeleted     bool
		wantRan         bool
		wantInWorktree  bool
	}{
		{
			name:           "a failed agent.pre-delete keeps the task and its worktree",
			worktree:       true,
			hookErr:        errors.New("unpushed work"),
			wantRan:        true,
			wantInWorktree: true,
		},
		{
			name:            "deleting the agent of a task it leaves behind runs no agent.pre-delete",
			worktree:        true,
			deleteAgentOnly: true,
		},
		{
			name:           "a task whose worktree has gone runs agent.pre-delete in the workflow's directory",
			worktree:       true,
			removeWorktree: true,
			wantDeleted:    true,
			wantRan:        true,
		},
		{
			name:        "a task with no worktree runs agent.pre-delete in the workflow's directory",
			wantDeleted: true,
			wantRan:     true,
		},
		{
			name:           "a task with a worktree runs agent.pre-delete in it before giving it back",
			worktree:       true,
			wantDeleted:    true,
			wantRan:        true,
			wantInWorktree: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lifecycle := &fakeLifecycle{
				dir:      t.TempDir(),
				hookErrs: map[string]error{taskHook: tt.hookErr},
			}
			fx := newTaskDeleteFixture(t, lifecycle, taskHookConfig(t, workflowstore.HookTaskPreDelete, tt.worktree))
			agent, err := launchOnTask(t, fx, NewAgentParams{Title: "auth"})
			require.NoError(t, err)

			if tt.removeWorktree {
				require.NoError(t, os.RemoveAll(agent.Cwd))
			}
			if tt.deleteAgentOnly {
				require.NoError(t, fx.agents.lifecycle.deleteNow(t.Context(), []string{agent.ID}))
			} else {
				require.NoError(t, fx.agents.lifecycle.DeleteTasks(t.Context(), []string{*agent.TaskID}))
			}

			kept, err := fx.st.Tasks.Get(t.Context(), *agent.TaskID)
			if tt.wantDeleted {
				require.ErrorIs(t, err, store.ErrNotFound)
			} else {
				require.NoError(t, err)
			}
			if tt.hookErr != nil {
				assert.Contains(t, kept.FailureReason, "unpushed work")
				assert.Empty(t, lifecycle.ranTaskDeprovisions(), "the worktree stays")
			}

			ran := lifecycle.ranTaskHooks()
			if !tt.wantRan {
				assert.Empty(t, ran)
				return
			}
			require.Len(t, ran, 1)
			assert.Empty(t, ran[0].ID, "no agent is left to run it as")
			if tt.wantInWorktree {
				assert.Equal(t, agent.Cwd, ran[0].Cwd)
				assert.Nil(t, ran[0].ProvisionEnabled)
			} else {
				assert.Equal(t, fx.workflow.Cwd, ran[0].Cwd)
				assert.Equal(t, xptr.Ptr(false), ran[0].ProvisionEnabled)
			}
		})
	}
}

// TestIsOpening covers which agents starting on a task take it from no live agent to one.
func TestIsOpening(t *testing.T) {
	taskID := "task"
	parentID := "parent"
	starting := agentstore.Agent{ID: "starting", TaskID: &taskID}

	tests := []struct {
		name  string
		agent agentstore.Agent
		live  []agentstore.Agent
		want  bool
	}{
		{
			name:  "an agent on no task opens none",
			agent: agentstore.Agent{ID: "starting"},
		},
		{
			name:  "a child opens nothing",
			agent: agentstore.Agent{ID: "starting", TaskID: &taskID, ParentAgentID: &parentID},
		},
		{
			name:  "another live agent holds the task open",
			agent: starting,
			live:  []agentstore.Agent{starting, {ID: "other", State: agentstore.StateIdle}},
		},
		{
			name:  "an agent that failed to create holds nothing open",
			agent: starting,
			live:  []agentstore.Agent{starting, {ID: "other", State: agentstore.StateFailedToCreate}},
			want:  true,
		},
		{
			name:  "the only agent on the task opens it",
			agent: starting,
			live:  []agentstore.Agent{starting},
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isOpening(tt.agent, tt.live))
		})
	}
}

// TestJoiningAgentWaitsForTheTaskToOpen pins that a joiner waits out the opener's task.pre-open and opens the task itself when that failed.
func TestJoiningAgentWaitsForTheTaskToOpen(t *testing.T) {
	tests := []struct {
		name        string
		hookErr     error
		wantOpeners int
	}{
		{
			name:        "a joiner opens the task itself once the agent opening it fails",
			hookErr:     errors.New("cache is cold"),
			wantOpeners: 2,
		},
		{
			name:        "a joiner starts once the task has opened, without opening it again",
			wantOpeners: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hold := make(chan struct{})
			lifecycle := &fakeLifecycle{
				dir:          t.TempDir(),
				holdHooks:    hold,
				hookErrsOnce: map[string]error{taskHook: tt.hookErr},
			}
			fx := newTaskDeleteFixture(t, lifecycle, taskHookConfig(t, workflowstore.HookTaskPreOpen, false))

			opened := make(chan error, 1)
			go func() {
				_, err := launchOnTask(t, fx, NewAgentParams{
					Title:    "auth",
					TaskSlug: "work",
				})
				opened <- err
			}()
			require.Eventually(t, func() bool {
				_, err := fx.st.Tasks.GetBySlug(t.Context(), "work")
				return err == nil
			}, 5*time.Second, 10*time.Millisecond)

			joined := make(chan error, 1)
			var joiner *agentstore.Agent
			go func() {
				agent, err := launchOnTask(t, fx, NewAgentParams{
					Title: "follow-up",
					Task:  "work",
				})
				joiner = agent
				joined <- err
			}()
			require.Never(t, func() bool { return len(joined) > 0 }, 200*time.Millisecond, 20*time.Millisecond)

			close(hold)
			openErr := <-opened
			require.NoError(t, <-joined)
			assert.Equal(t, tt.hookErr != nil, openErr != nil)

			ran := lifecycle.ranTaskHooks()
			require.Len(t, ran, tt.wantOpeners)
			if tt.hookErr != nil {
				assert.Equal(t, joiner.ID, ran[1].ID)
			}
		})
	}
}
