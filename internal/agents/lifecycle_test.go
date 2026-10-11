package agents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"komrad/config"
	"komrad/internal/agenterr"
	"komrad/internal/frames"
	"komrad/internal/harness/pane"
	"komrad/internal/harnesstest"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	reviewstore "komrad/internal/store/review"
	rowstore "komrad/internal/store/row"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/tmuxtest"
	"komrad/lib/xslice"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeleteAgentLeavesItsTasksCheckout covers the task's worktree outliving every agent on it.
func TestDeleteAgentLeavesItsTasksCheckout(t *testing.T) {
	tests := []struct {
		name     string
		archive  bool
		reviewer bool
	}{
		{
			name:     "deleting a reviewer",
			reviewer: true,
		},
		{
			name: "deleting the task's writer",
		},
		{
			name:    "archiving one",
			archive: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			st := newStore(t)
			lifecycle := &fakeLifecycle{}
			manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

			created, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), worktreeConfig(t, "backend"))
			require.NoError(t, err)

			agent, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
				Title:      "agent",
				Dir:        t.TempDir(),
				WorkflowID: created.ID,
			})
			require.NoError(t, err)

			target := agent
			if tt.reviewer {
				target, err = manager.launch.NewAgent(t.Context(), reviewerOf(*agent, "reviewer"))
				require.NoError(t, err)
			}

			if tt.archive {
				require.NoError(t, archiveWriters(t.Context(), manager.lifecycle, []string{target.ID}))
			} else {
				require.NoError(t, manager.lifecycle.deleteNow(t.Context(), []string{target.ID}))
			}

			assert.Empty(t, lifecycle.ranDeprovisions())
			assert.Empty(t, lifecycle.ranTaskDeprovisions())
			task, err := st.Tasks.Get(t.Context(), *agent.TaskID)
			require.NoError(t, err)
			assert.False(t, task.Checkout.WorktreeRemoved)
		})
	}
}

// TestDeleteAgentGivesBackTheCheckoutsOfAnArchivedWorkflow covers the last agent of an archived
// workflow taking the workflow, its tasks and their worktrees with it.
func TestDeleteAgentGivesBackTheCheckoutsOfAnArchivedWorkflow(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	lifecycle := &fakeLifecycle{}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)
	workflows := newWorkflowService(st, tmux)

	created, err := workflows.NewWorkflow(t.Context(), worktreeConfig(t, "backend"))
	require.NoError(t, err)

	agent, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "agent",
		Dir:        t.TempDir(),
		WorkflowID: created.ID,
	})
	require.NoError(t, err)
	task, err := st.Tasks.Get(t.Context(), *agent.TaskID)
	require.NoError(t, err)
	require.NotEmpty(t, task.Checkout.Cwd)

	require.NoError(t, workflows.ArchiveWorkflow(t.Context(), created.ID))
	require.NoError(t, manager.lifecycle.deleteNow(t.Context(), []string{agent.ID}))

	assert.Equal(t, []string{task.Checkout.Cwd}, lifecycle.ranTaskDeprovisions())
	_, err = st.Tasks.Get(t.Context(), task.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
	_, err = st.Workflows.GetIncludingArchived(t.Context(), created.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestDeleteAgentMarksFailedToCleanupWhenTeardownFails covers the other side of a teardown: a
// delete whose agent.pre-delete hook fails leaves the row behind, marked so.
func TestDeleteAgentMarksFailedToCleanupWhenTeardownFails(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	lifecycle := &fakeLifecycle{hookErrs: map[string]error{refusingHook: errors.New("worktree is dirty")}}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

	created, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), refusingConfig(t, "backend"))
	require.NoError(t, err)

	agent, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "agent",
		Dir:        t.TempDir(),
		WorkflowID: created.ID,
	})
	require.NoError(t, err)

	require.Error(t, manager.lifecycle.deleteNow(t.Context(), []string{agent.ID}))

	agents, listErr := st.Agents.List(t.Context())
	require.NoError(t, listErr)
	require.Len(t, agents, 1, "a failed teardown leaves the row rather than dropping it")
	assert.Equal(t, agentstore.StateFailedToCleanup, agents[0].State)
	assert.Contains(t, agents[0].FailureReason, "worktree is dirty")
}

// TestDeleteClaimsTheRowWhileTheTeardownRuns covers the row leaving the page listing the
// moment the delete is asked, and coming back only when the teardown fails.
func TestDeleteClaimsTheRowWhileTheTeardownRuns(t *testing.T) {
	tests := []struct {
		name        string
		teardownErr error
		wantListed  int
		wantState   agentstore.State
	}{
		{
			name:        "a teardown that fails gives the row back marked so",
			teardownErr: errors.New("worktree is dirty"),
			wantListed:  1,
			wantState:   agentstore.StateFailedToCleanup,
		},
		{
			name:       "a teardown that works takes the row",
			wantListed: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			st := newStore(t)
			lifecycle := &fakeLifecycle{hookErrs: map[string]error{refusingHook: tt.teardownErr}}
			manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

			created, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), refusingConfig(t, "backend"))
			require.NoError(t, err)

			agent, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
				Title:      "agent",
				Dir:        t.TempDir(),
				WorkflowID: created.ID,
			})
			require.NoError(t, err)
			require.NoError(t, archiveWriters(t.Context(), manager.lifecycle, []string{agent.ID}))

			claims, err := st.Agents.MarkDeleting(t.Context(), []string{agent.ID})
			require.NoError(t, err)
			require.Len(t, claims, 1)

			page, err := st.Rows.ListPage(t.Context(), rowstore.Page{Limit: 10, Archived: true})
			require.NoError(t, err)
			assert.Empty(t, page, "the row is out of the archive while the teardown is owed")
			all, err := st.Agents.ListAll(t.Context())
			require.NoError(t, err)
			require.Len(t, all, 1, "and still there")
			assert.NotNil(t, all[0].DeletingAt)

			require.ErrorIs(t, manager.lifecycle.DeleteAgent(t.Context(), agent.ID, claims[0].DeletingAt), tt.teardownErr)

			page, err = st.Rows.ListPage(t.Context(), rowstore.Page{Limit: 10, Archived: true})
			require.NoError(t, err)
			require.Len(t, page, tt.wantListed)
			if tt.wantListed > 0 {
				assert.Equal(t, tt.wantState, page[0].State)
				assert.Nil(t, page[0].DeletingAt)
			}
		})
	}
}

// TestDeleteStaysArchivedOnAFailedTeardown pins a failed cleanup leaving the row in the archive.
func TestDeleteStaysArchivedOnAFailedTeardown(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	lifecycle := &fakeLifecycle{hookErrs: map[string]error{refusingHook: errors.New("worktree is dirty")}}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

	created, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), refusingConfig(t, "backend"))
	require.NoError(t, err)

	agent, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "agent",
		Dir:        t.TempDir(),
		WorkflowID: created.ID,
	})
	require.NoError(t, err)

	require.NoError(t, archiveWriters(t.Context(), manager.lifecycle, []string{agent.ID}))
	require.Error(t, manager.lifecycle.deleteNow(t.Context(), []string{agent.ID}))

	live, listErr := st.Agents.List(t.Context())
	require.NoError(t, listErr)
	assert.Empty(t, live, "a teardown that failed does not put the agent back on the live list")

	all, listErr := st.Agents.ListAll(t.Context())
	require.NoError(t, listErr)
	require.Len(t, all, 1, "a failed teardown leaves the row rather than dropping it")
	assert.Equal(t, agentstore.StateFailedToCleanup, all[0].State)
	assert.NotNil(t, all[0].ArchivedAt)
}

// TestDeleteAgentRetryRecordsTheLatestReasonOnly pins a second failure overwriting the first.
func TestDeleteAgentRetryRecordsTheLatestReasonOnly(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	lifecycle := &fakeLifecycle{hookErrs: map[string]error{refusingHook: errors.New("worktree is dirty")}}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

	created, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), refusingConfig(t, "backend"))
	require.NoError(t, err)

	agent, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "agent",
		Dir:        t.TempDir(),
		WorkflowID: created.ID,
	})
	require.NoError(t, err)

	require.Error(t, manager.lifecycle.deleteNow(t.Context(), []string{agent.ID}))

	agents, err := st.Agents.List(t.Context())
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Contains(t, agents[0].FailureReason, "worktree is dirty")

	lifecycle.hookErrs[refusingHook] = errors.New("network unreachable")
	require.Error(t, manager.lifecycle.deleteNow(t.Context(), []string{agent.ID}))

	agents, err = st.Agents.List(t.Context())
	require.NoError(t, err)
	require.Len(t, agents, 1, "still failing, so still here")
	assert.Equal(t, agentstore.StateFailedToCleanup, agents[0].State)
	assert.Contains(t, agents[0].FailureReason, "network unreachable")
	assert.NotContains(t, agents[0].FailureReason, "worktree is dirty", "the stale reason does not linger alongside the new one")
}

// TestDeleteAgentsHandlesMixedCleanupOutcomes pins each agent's cleanup outcome landing on
// that agent alone rather than on the batch.
func TestDeleteAgentsHandlesMixedCleanupOutcomes(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	lifecycle := &fakeLifecycle{}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)
	workflows := newWorkflowService(st, tmux)

	backend, err := workflows.NewWorkflow(t.Context(), worktreeConfig(t, "backend"))
	require.NoError(t, err)
	frontend, err := workflows.NewWorkflow(t.Context(), refusingConfig(t, "frontend"))
	require.NoError(t, err)

	ok, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "ok",
		Dir:        t.TempDir(),
		WorkflowID: backend.ID,
	})
	require.NoError(t, err)
	bad, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "bad",
		Dir:        t.TempDir(),
		WorkflowID: frontend.ID,
	})
	require.NoError(t, err)

	lifecycle.hookErrs = map[string]error{refusingHook: errors.New("container is still running")}

	require.ErrorContains(t, manager.lifecycle.deleteNow(t.Context(), []string{ok.ID, bad.ID}), "container is still running")

	agents, err := st.Agents.List(t.Context())
	require.NoError(t, err)
	require.Len(t, agents, 1, "the one that succeeded is gone, the one that failed is left")
	assert.Equal(t, bad.ID, agents[0].ID)
	assert.Equal(t, agentstore.StateFailedToCleanup, agents[0].State)
	assert.Contains(t, agents[0].FailureReason, "container is still running")
}

// TestDeleteAgentRemovesItsRunLogs pins a delete taking the agent's logs with it, and
// leaving them in place where the cleanup failed and they are all there is to read.
func TestDeleteAgentRemovesItsRunLogs(t *testing.T) {
	tests := []struct {
		name        string
		cleanupErr  error
		wantRemoved bool
	}{
		{name: "no cleanup configured", wantRemoved: true},
		{name: "cleanup succeeds", wantRemoved: true},
		{name: "cleanup fails", cleanupErr: errors.New("worktree is dirty")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			st := newStore(t)
			lifecycle := &fakeLifecycle{hookErrs: map[string]error{refusingHook: tt.cleanupErr}}
			logsDir := t.TempDir()
			manager := st.agentService(agentServiceOptions{
				tmux:      tmux,
				harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}},
				addr:      "127.0.0.1:9999",
				lifecycle: lifecycle,
				logsDir:   logsDir,
			})

			cfg := workflowstore.Config{Name: "backend"}
			if tt.name != "no cleanup configured" {
				cfg = refusingConfig(t, "backend")
			}
			created, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), cfg)
			require.NoError(t, err)

			agent, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
				Title:      "agent",
				Dir:        t.TempDir(),
				WorkflowID: created.ID,
			})
			require.NoError(t, err)

			logDir := config.RunLogDir(logsDir, agent.ID)
			require.NoError(t, os.MkdirAll(logDir, 0o755))
			provisionPath := filepath.Join(logDir, config.RunLogName(1, config.TriggerAgentProvision, "make-worktree"))
			deprovisionPath := filepath.Join(logDir, config.RunLogName(2, config.TriggerAgentDeprovision, "drop-worktree"))
			require.NoError(t, os.WriteFile(provisionPath, []byte("cloning\n"), 0o644))
			require.NoError(t, os.WriteFile(deprovisionPath, []byte("dropping\n"), 0o644))

			require.ErrorIs(t, manager.lifecycle.deleteNow(t.Context(), []string{agent.ID}), tt.cleanupErr)

			_, dirErr := os.Stat(logDir)
			if tt.wantRemoved {
				assert.True(t, os.IsNotExist(dirErr), "every run goes with a row that is really gone")
			} else {
				require.NoError(t, dirErr, "the row is still here to read the failure from, so its runs stay too")
			}
		})
	}
}

// TestDeleteAgentRemovesItsRecordedFrames pins a delete reclaiming what a recording kept for
// that agent.
func TestDeleteAgentRemovesItsRecordedFrames(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	framesDir := t.TempDir()
	manager := st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}},
		addr:      "127.0.0.1:9999",
		lifecycle: &fakeLifecycle{},
		framesDir: framesDir,
	})

	created, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{Name: "backend"})
	require.NoError(t, err)

	agent, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "agent",
		Dir:        t.TempDir(),
		WorkflowID: created.ID,
	})
	require.NoError(t, err)

	agentFrames := frames.AgentDir(framesDir, agent.ID)
	require.NoError(t, os.MkdirAll(agentFrames, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(agentFrames, "20260904T120000.000-0001-idle.txt"), []byte("frame"), 0o600))

	require.NoError(t, manager.lifecycle.deleteNow(t.Context(), []string{agent.ID}))

	_, dirErr := os.Stat(agentFrames)
	assert.True(t, os.IsNotExist(dirErr), "the frames go with a row that is really gone")
}

// TestArchiveAgent pins the agent being stopped while its record and transcript stay, so
// search still reaches it.
func TestArchiveAgent(t *testing.T) {
	tests := []struct {
		name      string
		target    func(writer, reviewer agentstore.Agent) string
		wantErrIs error
	}{
		{
			name:      "an id nobody holds is not a quiet success",
			target:    func(agentstore.Agent, agentstore.Agent) string { return "00000000-0000-0000-0000-000000000000" },
			wantErrIs: agenterr.ErrAgentNotFound,
		},
		{
			name:      "a task's writer goes only with its task",
			target:    func(writer, _ agentstore.Agent) string { return writer.ID },
			wantErrIs: agenterr.ErrWriterGoesWithTask,
		},
		{
			name:   "a live reviewer is stopped and hidden",
			target: func(_, reviewer agentstore.Agent) string { return reviewer.ID },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := tmuxtest.NewFake()
			manager := newManager(tmux, st, "")

			writer := seedRunning(t, st, tmux, "writer", "%1", agentstore.StateIdle)
			reviewer := seedReviewer(t, st, tmux, writer)
			require.NoError(t, st.Agents.SetStateByID(t.Context(), reviewer.ID, agentstore.StateThinking))
			target := tt.target(writer, reviewer)

			err := manager.lifecycle.ArchiveAgents(t.Context(), ArchiveAgentsParams{IDs: []string{target}})
			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
				assert.Nil(t, agentByID(t, st, writer.ID).ArchivedAt, "nothing is archived")

				return
			}

			require.NoError(t, err)
			assert.Contains(t, tmux.KilledPanes, reviewer.Pane, "the agent's pane is killed")
			assert.NotContains(t, tmux.KilledPanes, writer.Pane, "its writer works on")

			archived := agentByID(t, st, target)
			assert.NotNil(t, archived.ArchivedAt, "the agent is hidden from listings")
			assert.Equal(t, agentstore.StateIdle, archived.State, "the state settles with the pane")
			assert.NotNil(t, archived.InterruptedAt, "the turn the kill cut is written down for the resume")
		})
	}
}

// TestArchiveKillsAPaneWrittenBeforeItsOwnWrite pins the archive killing the pane its own write
// took.
func TestArchiveKillsAPaneWrittenBeforeItsOwnWrite(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()
	ctx := t.Context()

	agent := seed(t, st, "dormant", "", agentstore.StateIdle)
	tmux.AfterGetPanes = buildResumeWrite(t, buildResumeWriteParams{
		Store: st,
		Tmux:  tmux,
		Agent: agent,
		Pane:  "%9",
	})

	manager := newManager(tmux, st, "")

	require.NoError(t, archiveWriters(ctx, manager.lifecycle, []string{agent.ID}))

	after := agentByID(t, st, agent.ID)
	require.NotNil(t, after.ArchivedAt, "the row really is archived")
	assert.Empty(t, after.Pane, "and holds no pane for anything else to find it by")
	assert.Contains(t, tmux.KilledPanes, "%9", "so the archive kills the pane its write took")
}

// TestArchiveFinishesAfterKillingTheCallersOwnPane covers an agent archiving itself, whose request dies with its pane.
func TestArchiveFinishesAfterKillingTheCallersOwnPane(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()
	manager := newManager(tmux, st, "")

	agent := seedRunning(t, st, tmux, "self", "%1", agentstore.StateThinking)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	tmux.AfterKillPane = func(string) { cancel() }

	require.NoError(t, archiveWriters(ctx, manager.lifecycle, []string{agent.ID}))

	after := agentByID(t, st, agent.ID)
	assert.Contains(t, tmux.KilledPanes, "%1")
	assert.NotNil(t, after.ArchivedAt, "the write outlives the request the kill cancelled")
}

// TestDeleteKillsAPaneWrittenBeforeItsOwnWrite is the same gap on the delete, whose row goes
// entirely rather than being hidden, so a pane written into the gap is even further out of
// reach afterwards.
func TestDeleteKillsAPaneWrittenBeforeItsOwnWrite(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()
	ctx := t.Context()

	agent := seed(t, st, "dormant", "", agentstore.StateIdle)

	manager := st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}},
		addr:      "127.0.0.1:9999",
		tx: txWritingPaneFirst{txRunner: st.Pool, inTheGap: buildResumeWrite(t, buildResumeWriteParams{
			Store: st,
			Tmux:  tmux,
			Agent: agent,
			Pane:  "%9",
		})},
	})

	require.NoError(t, manager.lifecycle.deleteNow(ctx, []string{agent.ID}))

	all, err := st.Agents.ListAll(ctx)
	require.NoError(t, err)
	assert.Empty(t, all, "the row really is gone")
	assert.Contains(t, tmux.KilledPanes, "%9", "so the delete kills the pane its write took")
}

// TestDeleteAgent covers destroying an agent for good, keyed by id since the unique name
// index holds among live rows only and these agents are archived.
func TestDeleteAgent(t *testing.T) {
	tests := []struct {
		name           string
		archive        bool
		target         func(seeded agentstore.Agent) string
		wantPaneKilled bool
	}{
		{
			name:           "a live agent is stopped and destroyed",
			target:         func(seeded agentstore.Agent) string { return seeded.ID },
			wantPaneKilled: true,
		},
		{
			name:    "an archived agent is destroyed too",
			archive: true,
			target:  func(seeded agentstore.Agent) string { return seeded.ID },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := tmuxtest.NewFake()
			manager := newManager(tmux, st, "")
			ctx := t.Context()

			seeded := seedRunning(t, st, tmux, "target", "%1", agentstore.StateThinking)
			st.SeedTranscript(t, seeded.ID, "something it said")
			if tt.archive {
				require.NoError(t, st.SoftDelete(ctx, seeded.Name))
			}

			err := manager.lifecycle.deleteNow(ctx, []string{tt.target(seeded)})

			require.NoError(t, err)
			if tt.wantPaneKilled {
				assert.Contains(t, tmux.KilledPanes, "%1", "the agent's pane is killed")
			}

			all, err := st.Agents.ListAll(ctx)
			require.NoError(t, err)
			assert.Empty(t, all, "the row is gone from every listing, not hidden from one")

			tr, err := st.GetTranscript(ctx, seeded.ID)
			require.NoError(t, err)
			assert.Empty(t, tr, "the transcript goes with the agent")
		})
	}
}

// TestDeleteAgentsTakesAReviewerAndNotItsWriter pins that a delete naming a task's writer is
// refused while one naming a reviewer goes ahead.
func TestDeleteAgentsTakesAReviewerAndNotItsWriter(t *testing.T) {
	tests := []struct {
		name      string
		target    func(writer, reviewer agentstore.Agent) string
		wantErrIs error
		wantKept  []string
	}{
		{
			name:      "a task's writer goes only with its task",
			target:    func(writer, _ agentstore.Agent) string { return writer.ID },
			wantErrIs: agenterr.ErrWriterGoesWithTask,
			wantKept:  []string{"writer", "reviewer"},
		},
		{
			name:     "a reviewer goes alone",
			target:   func(_, reviewer agentstore.Agent) string { return reviewer.ID },
			wantKept: []string{"writer"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := tmuxtest.NewFake()
			manager := newManager(tmux, st, "")

			writer := seedRunning(t, st, tmux, "writer", "%1", agentstore.StateIdle)
			reviewer := seedReviewer(t, st, tmux, writer)

			err := manager.lifecycle.DeleteAgents(t.Context(), []string{tt.target(writer, reviewer)})
			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
			} else {
				require.NoError(t, err)
			}

			all, err := st.Agents.ListAll(t.Context())
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.wantKept, xslice.Map(all, func(a agentstore.Agent) string { return a.Name }))
		})
	}
}

// TestAReissuedPaneIsNotTheAgents pins every read of a stored pane id surviving reissue.
func TestAReissuedPaneIsNotTheAgents(t *testing.T) {
	const shared = "%4"

	setup := func(t *testing.T) (*testAgents, *testStore, *tmuxtest.Fake, agentstore.Agent) {
		t.Helper()
		st := newStore(t)
		tmux := tmuxtest.NewFake()
		dormant := seed(t, st, "dormant", shared, agentstore.StateIdle)
		seedRunning(t, st, tmux, "running", shared, agentstore.StateThinking)

		return newManager(tmux, st, ""), st, tmux, dormant
	}

	t.Run("the dormant agent does not read as running", func(t *testing.T) {
		manager, _, _, dormant := setup(t)
		assert.False(t, manager.panes.IsAgentRunning(dormant))
	})

	t.Run("archiving it leaves the pane running", func(t *testing.T) {
		manager, _, tmux, dormant := setup(t)
		require.NoError(t, archiveWriters(t.Context(), manager.lifecycle, []string{dormant.ID}))
		assert.Empty(t, tmux.KilledPanes, "killing by a stale id would stop the agent using that pane now")
	})

	t.Run("deleting it leaves the pane running", func(t *testing.T) {
		manager, _, tmux, dormant := setup(t)
		require.NoError(t, manager.lifecycle.deleteNow(t.Context(), []string{dormant.ID}))
		assert.Empty(t, tmux.KilledPanes, "killing by a stale id would stop the agent using that pane now")
	})

	t.Run("resuming it opens a window rather than adopting the pane", func(t *testing.T) {
		manager, st, tmux, dormant := setup(t)
		resumed, err := manager.resume.ResumeAgent(t.Context(), ResumeAgentParams{ID: dormant.ID})
		require.NoError(t, err)
		assert.NotEqual(t, shared, resumed.Pane, "a resume that read the pane as its own would relaunch nothing")
		assert.Equal(t, dormant.ID, tmux.Owners[resumed.Pane], "and the new pane is its own")

		still := agentByID(t, st, dormant.ID)
		assert.Equal(t, resumed.Pane, still.Pane)
	})

	t.Run("a reconcile pass forgets the claim without reading the pane", func(t *testing.T) {
		_, st, tmux, dormant := setup(t)
		tmux.Captures[shared] = harnesstest.FrameWorking

		rec := NewReconciler(NewReconcilerParams{
			Panes:    tmux,
			Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
			States:   st.Agents,
		})
		require.NoError(t, rec.Reconcile(t.Context()))

		after := agentByID(t, st, dormant.ID)
		assert.Empty(t, after.Pane, "the stale claim is dropped, so it cannot collide again")
		assert.Equal(t, agentstore.StateIdle, after.State, "the frame belongs to another agent and says nothing about this one")
		assert.Equal(t, agentstore.StateThinking, stateOf(t, st, "running"), "the agent actually in that pane is read normally")
	})
}

// TestRestore pins an agent returning as a row alone, with a name taken while it sat hidden
// refused rather than worked around.
func TestRestore(t *testing.T) {
	tests := []struct {
		name      string
		reuseName bool
		wantErrIs error
	}{
		{
			name:      "a name taken since is refused",
			reuseName: true,
			wantErrIs: store.ErrNameTaken,
		},
		{
			name: "a free name is kept",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := tmuxtest.NewFake()
			manager := newManager(tmux, st, "")
			ctx := t.Context()

			workflowID := seedWorkflow(t, st)
			require.NoError(t, st.createAgent(ctx, agentstore.Agent{
				Name:       "foo",
				State:      agentstore.StateIdle,
				SessionID:  "sess-old",
				WorkflowID: workflowID,
			}))
			original, err := st.Agents.GetInWorkflow(ctx, workflowID, "foo")
			require.NoError(t, err)
			require.NoError(t, st.SoftDelete(ctx, "foo"))

			if tt.reuseName {
				require.NoError(t, st.createAgent(ctx, agentstore.Agent{
					Name:       "foo",
					State:      agentstore.StateIdle,
					WorkflowID: workflowID,
				}))
			}

			_, err = manager.lifecycle.RestoreAgents(ctx, []string{original.ID})

			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
				require.ErrorContains(t, err, "rename whatever holds", "the refusal says what to do about it")

				refused := agentByID(t, st, original.ID)
				assert.NotNil(t, refused.ArchivedAt, "the agent stays in the archive")
				assert.Equal(t, "foo", refused.Name, "under the name it went in with")

				return
			}

			require.NoError(t, err)

			restored := agentByID(t, st, original.ID)
			assert.Nil(t, restored.ArchivedAt, "the agent is back in the listing")
			assert.Equal(t, "foo", restored.Name)
			assert.Equal(t, agentstore.StateIdle, restored.State)
			assert.Equal(t, "sess-old", restored.SessionID, "the conversation is preserved")
			assert.Empty(t, tmux.Owners, "a restore starts nothing, so no pane was opened")
			assert.False(t, manager.panes.IsAgentRunning(restored), "it comes back dormant, waiting to be resumed")

			resumed, err := manager.resume.ResumeAgent(ctx, ResumeAgentParams{ID: original.ID})
			require.NoError(t, err)
			assert.Equal(t, original.ID, tmux.Owners[resumed.Pane], "resuming stamps the pane it came back in")
			assert.True(t, manager.panes.IsAgentRunning(*resumed), "so it reads as running again")
		})
	}
}

// TestRestoreCeiling pins a restore being refused where a create would be, or the archive is
// a way around the limit.
func TestRestoreCeiling(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := st.agentServiceCapped(tmux, &fakeLifecycle{}, 2)
	ctx := t.Context()

	archived := seed(t, st, "archived", "%0", agentstore.StateIdle)
	require.NoError(t, st.SoftDelete(ctx, "archived"))

	seed(t, st, "live-0", "%1", agentstore.StateThinking)
	liveTwo := seed(t, st, "live-1", "%2", agentstore.StateThinking)

	_, err := manager.lifecycle.RestoreAgents(ctx, []string{archived.ID})

	require.ErrorIs(t, err, agenterr.ErrTooManyAgents)
	still := agentByID(t, st, archived.ID)
	assert.NotNil(t, still.ArchivedAt, "the refused restore leaves it in the archive")

	require.NoError(t, archiveWriters(ctx, manager.lifecycle, []string{liveTwo.ID}))
	_, err = manager.lifecycle.RestoreAgents(ctx, []string{archived.ID})
	require.NoError(t, err, "archiving one makes room again")
}

// TestDeletingATasksLastAgentKeepsItsRounds pins that a task outliving its last agent keeps the
// snapshots its review diffs are drawn against.
func TestDeletingATasksLastAgentKeepsItsRounds(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, &fakeLifecycle{})
	reviews := reviewstore.NewReviewStore(st.Pool.GetDB())

	going := seedRoundedAgent(t, st, reviews, "going", t.TempDir(), "sha")
	task := *agentByID(t, st, going).TaskID

	require.NoError(t, manager.lifecycle.deleteNow(t.Context(), []string{going}))

	_, err := st.Tasks.Get(t.Context(), task)
	require.NoError(t, err, "the task outlives its last agent")
	assert.Empty(t, manager.snapshots.Released)
}

// TestDeletingAnAgentTakesItsDraftsAndKeepsItsReviews pins the two halves apart: a batch nobody
// has released has nothing in it worth keeping, and a released one is what the agent it was given
// to is working from.
func TestDeletingAnAgentTakesItsDraftsAndKeepsItsReviews(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, &fakeLifecycle{})
	reviews := reviewstore.NewReviewStore(st.Pool.GetDB())

	root := t.TempDir()
	worker := seedRoundedAgent(t, st, reviews, "worker", root, "worker-sha")
	first := seedRoundedAgent(t, st, reviews, "kid-a", root, "kid-a-sha")
	second := seedRoundedAgent(t, st, reviews, "kid-b", root, "kid-b-sha")

	round, err := reviews.GetLatestRound(t.Context(), *agentByID(t, st, worker).TaskID)
	require.NoError(t, err)

	// The first reviewer has had its say and started another; the second is still writing its
	// first.
	released := openBatch(t, reviews, round, first)
	_, err = reviews.ReleaseReview(t.Context(), reviewstore.ReleaseReviewParams{
		ID:      released.ID,
		Verdict: reviewstore.VerdictChangesRequested,
		Path:    "/tmp/round-1.md",
	})
	require.NoError(t, err)

	openBatch(t, reviews, round, first)
	openBatch(t, reviews, round, second)

	require.NoError(t, manager.lifecycle.deleteNow(t.Context(), []string{first, second}),
		"two reviewers holding open batches on one agent both delete")

	left, err := reviews.ListReviews(t.Context(), round.TaskID)
	require.NoError(t, err)
	require.Len(t, left, 1, "the drafts went and the released review stayed")
	assert.Nil(t, left[0].AuthorAgentID, "with its author nulled rather than the row taken")
	assert.NotNil(t, left[0].ReleasedAt)
}

// openBatch starts a review by one agent of a round.
func openBatch(t *testing.T, reviews *reviewstore.ReviewStore, round *reviewstore.Round, author string) *reviewstore.Review {
	t.Helper()

	review, err := reviews.OpenReview(t.Context(), reviewstore.OpenReviewParams{
		TaskID:        round.TaskID,
		RoundID:       round.ID,
		AuthorKind:    reviewstore.AuthorAgent,
		AuthorAgentID: &author,
	})
	require.NoError(t, err)

	return review
}

// seedRoundedAgent records an agent in a shared checkout with one round taken of it.
func seedRoundedAgent(t *testing.T, st *testStore, reviews *reviewstore.ReviewStore, name, root, sha string) string {
	t.Helper()

	require.NoError(t, st.createAgent(t.Context(), agentstore.Agent{
		Name:  name,
		Cwd:   root,
		State: agentstore.StateIdle,
	}))
	agent, err := st.Get(t.Context(), name)
	require.NoError(t, err)

	_, err = reviews.CreateRound(t.Context(), reviewstore.CreateRoundParams{
		AgentID:     agent.ID,
		TaskID:      *agent.TaskID,
		BaseSHA:     "base",
		SnapshotSHA: sha,
	})
	require.NoError(t, err)

	return agent.ID
}

// TestArchiveAgentsTakesTheWholeBatch covers archiving several agents at once, which need not
// share a workflow and whose panes go with them.
func TestArchiveAgentsTakesTheWholeBatch(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()
	manager := newManager(tmux, st, "")
	ctx := t.Context()

	backend, frontend := seedWorkflow(t, st), seedWorkflow(t, st)
	auth := seedRunningIn(t, st, tmux, backend, "auth", "%1")
	api := seedRunningIn(t, st, tmux, backend, "api", "%2")

	ui := seedInWorkflow(t, st, frontend, "ui", "", agentstore.StateIdle)

	require.NoError(t, archiveWriters(ctx, manager.lifecycle, []string{auth.ID, api.ID, ui.ID}))

	for _, agent := range []agentstore.Agent{auth, api, ui} {
		assert.NotNil(t, agentByID(t, st, agent.ID).ArchivedAt, "%s is archived", agent.Name)
	}

	assert.ElementsMatch(t, []string{"%1", "%2"}, tmux.KilledPanes, "only the running agents had a pane to kill")

	live, err := st.Agents.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, live, "the batch spanned both workflows")
}

// TestArchiveAgentsRefusesTheWholeBatch pins one bad id refusing the batch.
func TestArchiveAgentsRefusesTheWholeBatch(t *testing.T) {
	tests := []struct {
		name  string
		stray string
	}{
		{
			name:  "an id nobody holds",
			stray: uuid.NewString(),
		},
		{
			name:  "a value that is not an id at all",
			stray: "not-an-id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := tmuxtest.NewFake()
			manager := newManager(tmux, st, "")
			ctx := t.Context()

			auth := seedReviewer(t, st, tmux, seedRunning(t, st, tmux, "auth", "%1", agentstore.StateThinking))
			api := seedReviewer(t, st, tmux, seedRunning(t, st, tmux, "api", "%2", agentstore.StateThinking))

			err := manager.lifecycle.ArchiveAgents(ctx, ArchiveAgentsParams{IDs: []string{auth.ID, tt.stray, api.ID}})

			require.ErrorIs(t, err, agenterr.ErrAgentNotFound)
			require.ErrorContains(t, err, tt.stray, "the refusal names what it could not find")
			assert.Empty(t, tmux.KilledPanes, "nothing was stopped")

			live, err := st.Agents.List(ctx)
			require.NoError(t, err)
			assert.Len(t, live, 4, "and nothing was archived")
		})
	}
}

// TestArchiveAgentsHoldsAnAgentToItselfAndItsTasksReviewers leaves an orchestrator and the panel
// unrestricted.
func TestArchiveAgentsHoldsAnAgentToItselfAndItsTasksReviewers(t *testing.T) {
	tests := []struct {
		name      string
		from      string
		archive   []string
		wantErrIs error
	}{
		{
			name:      "a writer archiving another task's reviewer",
			from:      "writer",
			archive:   []string{"reviewer on another task"},
			wantErrIs: errArchiveNotYours,
		},
		{
			name:      "another task's reviewer refuses the whole batch",
			from:      "writer",
			archive:   []string{"reviewer", "reviewer on another task"},
			wantErrIs: errArchiveNotYours,
		},
		{
			name:      "a reviewer archiving its writer",
			from:      "reviewer",
			archive:   []string{"writer"},
			wantErrIs: errArchiveNotYours,
		},
		{
			name:      "another task's reviewer is refused ahead of an id nobody holds",
			from:      "writer",
			archive:   []string{"reviewer on another task", "missing"},
			wantErrIs: errArchiveNotYours,
		},
		{
			name:      "an id nobody holds is skipped by the ownership check and then not found",
			from:      "writer",
			archive:   []string{"reviewer", "missing"},
			wantErrIs: agenterr.ErrAgentNotFound,
		},
		{
			name:      "a writer archiving itself",
			from:      "writer",
			archive:   []string{"writer"},
			wantErrIs: agenterr.ErrWriterGoesWithTask,
		},
		{
			name:      "the panel archiving a writer",
			archive:   []string{"writer"},
			wantErrIs: agenterr.ErrWriterGoesWithTask,
		},
		{
			name:    "a writer archiving its task's reviewer",
			from:    "writer",
			archive: []string{"reviewer"},
		},
		{
			name:    "a reviewer archiving itself",
			from:    "reviewer",
			archive: []string{"reviewer"},
		},
		{
			name:    "an orchestrator archiving anybody",
			from:    "orchestrator",
			archive: []string{"reviewer on another task"},
		},
		{
			name:    "the panel archiving a reviewer",
			archive: []string{"reviewer"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := tmuxtest.NewFake()
			manager := newManager(tmux, st, "")
			ctx := t.Context()

			writer := seedRunning(t, st, tmux, "writer", "%1", agentstore.StateIdle)
			seeded := map[string]string{
				"writer":                   writer.ID,
				"reviewer":                 seedReviewer(t, st, tmux, writer).ID,
				"reviewer on another task": seedReviewer(t, st, tmux, seedRunning(t, st, tmux, "billing", "%2", agentstore.StateIdle)).ID,
				"orchestrator":             seedOrchestrator(t, st, tmux).ID,
				"missing":                  uuid.NewString(),
			}

			err := manager.lifecycle.ArchiveAgents(ctx, ArchiveAgentsParams{
				IDs:       xslice.Map(tt.archive, func(name string) string { return seeded[name] }),
				ByAgentID: seeded[tt.from],
			})
			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
				assert.Empty(t, tmux.KilledPanes, "nothing was stopped")

				return
			}

			require.NoError(t, err)
			for _, name := range tt.archive {
				assert.NotNil(t, agentByID(t, st, seeded[name]).ArchivedAt, "%s is archived", name)
			}
		})
	}
}

// TestDeleteAgentsTakesTheirTranscripts pins the conversations going with the rows.
func TestDeleteAgentsTakesTheirTranscripts(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()
	manager := newManager(tmux, st, "")
	ctx := t.Context()

	auth := seedRunning(t, st, tmux, "auth", "%1", agentstore.StateThinking)
	api := seed(t, st, "api", "%2", agentstore.StateIdle)

	for _, agent := range []agentstore.Agent{auth, api} {
		st.SeedTranscript(t, agent.ID, "something "+agent.Name+" said")
	}

	require.NoError(t, manager.lifecycle.deleteNow(ctx, []string{auth.ID, api.ID}))

	for _, agent := range []agentstore.Agent{auth, api} {
		stored, err := st.GetTranscript(ctx, agent.ID)
		require.NoError(t, err)
		assert.Empty(t, stored, "%s left no conversation behind", agent.Name)
	}
}

// TestRestoreAgentsBringsBackEveryWorkflow pins each workflow coming back with its agents.
func TestRestoreAgentsBringsBackEveryWorkflow(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()
	manager := newManager(tmux, st, "")
	workflows := newWorkflowService(st, tmux)
	ctx := t.Context()

	backend, err := workflows.NewWorkflow(ctx, workflowstore.Config{Name: "backend"})
	require.NoError(t, err)
	frontend, err := workflows.NewWorkflow(ctx, workflowstore.Config{Name: "frontend"})
	require.NoError(t, err)

	auth := seedInWorkflow(t, st, backend.ID, "auth", "", agentstore.StateIdle)
	ui := seedInWorkflow(t, st, frontend.ID, "ui", "", agentstore.StateIdle)

	require.NoError(t, workflows.ArchiveWorkflow(ctx, backend.ID))
	require.NoError(t, workflows.ArchiveWorkflow(ctx, frontend.ID))

	restored, err := manager.lifecycle.RestoreAgents(ctx, []string{auth.ID, ui.ID})
	require.NoError(t, err)
	require.Len(t, restored, 2)

	listed, err := st.Workflows.List(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"backend", "frontend"}, workflowNames(listed), "both folders came back")

	for _, agent := range restored {
		assert.Nil(t, agent.ArchivedAt, "%s is back in the listing", agent.Name)
	}
}

// TestRestoreAgentsRefusesTheWholeBatch pins one bad id refusing the batch, so a stale archive
// listing does not half-restore a folder.
func TestRestoreAgentsRefusesTheWholeBatch(t *testing.T) {
	st := newStore(t)
	manager := newManager(tmuxtest.NewFake(), st, "")
	ctx := t.Context()

	archived := seed(t, st, "auth", "", agentstore.StateIdle)
	require.NoError(t, st.SoftDelete(ctx, "auth"))

	_, err := manager.lifecycle.RestoreAgents(ctx, []string{archived.ID, uuid.NewString()})

	require.ErrorIs(t, err, agenterr.ErrAgentNotFound)
	assert.NotNil(t, agentByID(t, st, archived.ID).ArchivedAt, "the agent stays in the archive")
}

// TestRestoreAgentsCountsTheWholeBatchAgainstTheCeiling pins the whole batch being counted at
// once.
func TestRestoreAgentsCountsTheWholeBatchAgainstTheCeiling(t *testing.T) {
	st := newStore(t)
	manager := st.agentServiceCapped(tmuxtest.NewFake(), &fakeLifecycle{}, 3)
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	archived := make([]string, 0, 3)
	for _, name := range []string{"auth", "api", "db"} {
		agent := seedInWorkflow(t, st, workflowID, name, "", agentstore.StateIdle)
		archived = append(archived, agent.ID)
	}
	for _, name := range []string{"auth", "api", "db"} {
		require.NoError(t, st.SoftDelete(ctx, name))
	}

	seedInWorkflow(t, st, workflowID, "live", "%1", agentstore.StateThinking)

	_, err := manager.lifecycle.RestoreAgents(ctx, archived)
	require.ErrorIs(t, err, agenterr.ErrTooManyAgents, "three back into a panel of three holding one is refused")

	for _, id := range archived {
		assert.NotNil(t, agentByID(t, st, id).ArchivedAt, "the refused batch stays in the archive whole")
	}

	restored, err := manager.lifecycle.RestoreAgents(ctx, archived[:2])
	require.NoError(t, err, "what does fit comes back")
	assert.Len(t, restored, 2)
}

// TestRestoreAgentsRefusesOnANameTaken covers a name being taken either by a live agent or by
// two of the returning agents between them.
func TestRestoreAgentsRefusesOnANameTaken(t *testing.T) {
	tests := []struct {
		name           string
		liveNamesake   bool
		secondNamesake bool
	}{
		{
			name:         "a live agent has taken the name",
			liveNamesake: true,
		},
		{
			name:           "two returning agents want one name",
			secondNamesake: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			manager := newManager(tmuxtest.NewFake(), st, "")
			ctx := t.Context()

			workflowID := seedWorkflow(t, st)
			archived := seedInWorkflow(t, st, workflowID, "auth", "", agentstore.StateIdle)
			other := seedInWorkflow(t, st, workflowID, "api", "", agentstore.StateIdle)
			require.NoError(t, st.SoftDelete(ctx, "auth"))
			require.NoError(t, st.SoftDelete(ctx, "api"))

			batch := []string{archived.ID, other.ID}
			if tt.secondNamesake {
				second := seedInWorkflow(t, st, workflowID, "auth", "", agentstore.StateIdle)
				_, archiveErr := st.Agents.Archive(ctx, []string{second.ID})
				require.NoError(t, archiveErr)
				batch = append(batch, second.ID)
			}
			if tt.liveNamesake {
				seedInWorkflow(t, st, workflowID, "auth", "%1", agentstore.StateThinking)
			}

			_, err := manager.lifecycle.RestoreAgents(ctx, batch)

			require.ErrorIs(t, err, store.ErrNameTaken)
			require.ErrorContains(t, err, "auth", "the refusal names what is in the way")
			assert.NotNil(t, agentByID(t, st, other.ID).ArchivedAt, "the agent with no clash stays back too")
		})
	}
}

// TestRestoreAgentsAllowsANameSharedAcrossWorkflows pins that two agents called auth in two
// workflows are not a clash, a name being unique only inside its own.
func TestRestoreAgentsAllowsANameSharedAcrossWorkflows(t *testing.T) {
	st := newStore(t)
	manager := newManager(tmuxtest.NewFake(), st, "")
	ctx := t.Context()

	backend, frontend := seedWorkflow(t, st), seedWorkflow(t, st)
	first := seedInWorkflow(t, st, backend, "auth", "", agentstore.StateIdle)
	second := seedInWorkflow(t, st, frontend, "auth", "", agentstore.StateIdle)

	_, archiveErr := st.Agents.Archive(ctx, []string{first.ID, second.ID})

	require.NoError(t, archiveErr)

	restored, err := manager.lifecycle.RestoreAgents(ctx, []string{first.ID, second.ID})
	require.NoError(t, err)
	assert.Len(t, restored, 2)

	for _, id := range []string{first.ID, second.ID} {
		assert.Nil(t, agentByID(t, st, id).ArchivedAt)
	}
}

// TestRestoreAgentsPassesOverTheLiveOnes pins live agents being skipped.
func TestRestoreAgentsPassesOverTheLiveOnes(t *testing.T) {
	st := newStore(t)
	manager := st.agentServiceCapped(tmuxtest.NewFake(), &fakeLifecycle{}, 2)
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	live := seedInWorkflow(t, st, workflowID, "live", "%1", agentstore.StateThinking)
	archived := seedInWorkflow(t, st, workflowID, "archived", "", agentstore.StateIdle)
	require.NoError(t, st.SoftDelete(ctx, "archived"))

	restored, err := manager.lifecycle.RestoreAgents(ctx, []string{live.ID, archived.ID})
	require.NoError(t, err)
	require.Len(t, restored, 2)

	assert.Nil(t, agentByID(t, st, archived.ID).ArchivedAt)
	assert.Nil(t, agentByID(t, st, live.ID).ArchivedAt, "the live agent is returned as it stands")
}

// TestRestoreAgentsWithNothingArchived pins that a batch of already-live ids writes nothing
// rather than being refused for room it does not need.
func TestRestoreAgentsWithNothingArchived(t *testing.T) {
	st := newStore(t)
	manager := st.agentServiceCapped(tmuxtest.NewFake(), &fakeLifecycle{}, 2)
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	first := seedInWorkflow(t, st, workflowID, "auth", "%1", agentstore.StateThinking)
	second := seedInWorkflow(t, st, workflowID, "api", "%2", agentstore.StateThinking)

	restored, err := manager.lifecycle.RestoreAgents(ctx, []string{first.ID, second.ID})
	require.NoError(t, err)
	assert.Len(t, restored, 2)
}

func seedRunningIn(t *testing.T, st *testStore, tmux paneStamper, workflowID, name, pane string) agentstore.Agent {
	t.Helper()

	agent := seedInWorkflow(t, st, workflowID, name, pane, agentstore.StateThinking)
	require.NoError(t, tmux.SetPaneOwner(pane, agent.ID))

	return agent
}

// TestArchiveAgentsNamesOnlyTheFirstMissing pins the refusal naming one id.
func TestArchiveAgentsNamesOnlyTheFirstMissing(t *testing.T) {
	st := newStore(t)
	manager := newManager(tmuxtest.NewFake(), st, "")

	ids := make([]string, 0, 20)
	for range 20 {
		ids = append(ids, uuid.NewString())
	}

	err := manager.lifecycle.ArchiveAgents(t.Context(), ArchiveAgentsParams{IDs: ids})

	require.ErrorIs(t, err, agenterr.ErrAgentNotFound)
	require.ErrorContains(t, err, ids[0], "the first few are named")
	require.ErrorContains(t, err, "and 15 more", "and the rest are counted")
	assert.NotContains(t, err.Error(), ids[19], "rather than spelled out")
}

// TestRestoreOfAReviewerUnderTheReviewerCeiling pins that a reviewer comes back only while its
// task still has room.
func TestRestoreOfAReviewerUnderTheReviewerCeiling(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}},
		addr:      "127.0.0.1:9999",
		usage:     config.Usage{Task: config.TaskUsage{MaxReviewers: 1}},
	})
	ctx := t.Context()

	writer := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
	require.NoError(t, st.Agents.Update(ctx, withCwd(writer, t.TempDir())))

	first, err := manager.launch.NewAgent(ctx, reviewerOf(writer, "reviewer"))
	require.NoError(t, err)
	require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, ArchiveAgentsParams{IDs: []string{first.ID}}))

	_, err = manager.launch.NewAgent(ctx, reviewerOf(writer, "second-reviewer"))
	require.NoError(t, err, "the ceiling frees up once one reviewer is archived")

	_, err = manager.lifecycle.RestoreAgents(ctx, []string{first.ID})
	require.ErrorIs(t, err, agenterr.ErrTooManyReviewers)
	assert.NotNil(t, agentByID(t, st, first.ID).ArchivedAt, "the refusal restores nothing")

	held, err := st.Agents.CountLiveReviewers(ctx, *writer.TaskID)
	require.NoError(t, err)
	assert.Equal(t, 1, held)
}

// TestATasksWriterMovesWithItsTask pins that a task's archive and restore carry its writer while
// a reviewer comes and goes on its own.
func TestATasksWriterMovesWithItsTask(t *testing.T) {
	archiveTask := func(ctx context.Context, lifecycle *LifecycleService, writer, _ agentstore.Agent) error {
		return lifecycle.ArchiveTask(ctx, *writer.TaskID)
	}

	tests := []struct {
		name     string
		act      func(ctx context.Context, lifecycle *LifecycleService, writer, reviewer agentstore.Agent) error
		wantLive []string
		wantKept []string
	}{
		{
			name:     "archiving the task takes the reviewer",
			act:      archiveTask,
			wantKept: []string{"writer", "reviewer"},
		},
		{
			name: "archiving the reviewer leaves the writer",
			act: func(ctx context.Context, lifecycle *LifecycleService, writer, reviewer agentstore.Agent) error {
				return lifecycle.ArchiveAgents(ctx, ArchiveAgentsParams{IDs: []string{reviewer.ID}})
			},
			wantLive: []string{"writer"},
			wantKept: []string{"writer", "reviewer"},
		},
		{
			name: "deleting the writer takes the reviewer",
			act: func(ctx context.Context, lifecycle *LifecycleService, writer, reviewer agentstore.Agent) error {
				return lifecycle.deleteNow(ctx, []string{writer.ID})
			},
		},
		{
			name: "deleting the reviewer leaves the writer",
			act: func(ctx context.Context, lifecycle *LifecycleService, writer, reviewer agentstore.Agent) error {
				return lifecycle.deleteNow(ctx, []string{reviewer.ID})
			},
			wantLive: []string{"writer"},
			wantKept: []string{"writer"},
		},
		{
			name: "restoring the writer brings the task back alone",
			act: func(ctx context.Context, lifecycle *LifecycleService, writer, reviewer agentstore.Agent) error {
				if err := archiveTask(ctx, lifecycle, writer, reviewer); err != nil {
					return err
				}

				_, err := lifecycle.RestoreAgents(ctx, []string{writer.ID})

				return err
			},
			wantLive: []string{"writer"},
			wantKept: []string{"writer", "reviewer"},
		},
		{
			name: "restoring the writer and the reviewer brings both back",
			act: func(ctx context.Context, lifecycle *LifecycleService, writer, reviewer agentstore.Agent) error {
				if err := archiveTask(ctx, lifecycle, writer, reviewer); err != nil {
					return err
				}

				_, err := lifecycle.RestoreAgents(ctx, []string{writer.ID, reviewer.ID})

				return err
			},
			wantLive: []string{"writer", "reviewer"},
			wantKept: []string{"writer", "reviewer"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			st := newStore(t)
			manager := newManager(tmux, st, "")
			ctx := t.Context()

			writer := seedRunning(t, st, tmux, "writer", "%writer", agentstore.StateIdle)
			reviewer := seedReviewer(t, st, tmux, writer)

			require.NoError(t, tt.act(ctx, manager.lifecycle, writer, reviewer))

			live, err := st.Agents.List(ctx)
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.wantLive, collectAgentNames(live))

			kept, err := st.Agents.ListAll(ctx)
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.wantKept, collectAgentNames(kept))
			assert.Contains(t, tmux.KilledPanes, reviewer.Pane, "the reviewer's pane goes with its row")
		})
	}
}

// TestRestoreOfAReviewerNeedsItsTaskBack pins that a reviewer comes back only onto a live task.
func TestRestoreOfAReviewerNeedsItsTaskBack(t *testing.T) {
	tests := []struct {
		name        string
		restoreTask bool
		wantErrIs   error
	}{
		{
			name:        "the task is live again",
			restoreTask: true,
		},
		{
			name:      "the task is still archived",
			wantErrIs: agenterr.ErrTaskArchived,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			st := newStore(t)
			manager := newManager(tmux, st, "")
			ctx := t.Context()

			writer := seedRunning(t, st, tmux, "writer", "%writer", agentstore.StateIdle)
			reviewer := seedReviewer(t, st, tmux, writer)

			require.NoError(t, manager.lifecycle.ArchiveTask(ctx, *writer.TaskID))
			if tt.restoreTask {
				_, err := manager.lifecycle.RestoreAgents(ctx, []string{writer.ID})
				require.NoError(t, err)
			}

			restored, err := manager.lifecycle.RestoreAgents(ctx, []string{reviewer.ID})
			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
				assert.NotNil(t, agentByID(t, st, reviewer.ID).ArchivedAt, "the refusal restores nothing")

				return
			}

			require.NoError(t, err)
			assert.Len(t, restored, 1)
			assert.Nil(t, agentByID(t, st, reviewer.ID).ArchivedAt)
		})
	}
}

func collectAgentNames(agents []agentstore.Agent) []string {
	return xslice.Map(agents, func(agent agentstore.Agent) string {
		return agent.Name
	})
}

// TestDeleteAgentTakesOnlyAClaimedAgent pins a delete job leaving alone an agent its claim no
// longer holds.
func TestDeleteAgentTakesOnlyAClaimedAgent(t *testing.T) {
	tests := []struct {
		name     string
		claims   int
		wantLeft int
	}{
		{
			name:   "an agent the claim holds is torn down and removed",
			claims: 1,
		},
		{
			name:     "an agent nobody claimed is left alone",
			wantLeft: 1,
		},
		{
			name:     "an agent claimed again since is left to the newer claim's job",
			claims:   2,
			wantLeft: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			st := newStore(t)
			lifecycle := &fakeLifecycle{}
			manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)
			ctx := t.Context()

			created, err := newWorkflowService(st, tmux).NewWorkflow(ctx, worktreeConfig(t, "backend"))
			require.NoError(t, err)
			agent, err := manager.launch.NewAgent(ctx, NewAgentParams{
				Title:      "agent",
				Dir:        t.TempDir(),
				WorkflowID: created.ID,
			})
			require.NoError(t, err)

			var first time.Time
			for i := range tt.claims {
				if i > 0 {
					require.NoError(t, st.Agents.ClearDeleting(ctx, []string{agent.ID}))
				}
				claims, err := st.Agents.MarkDeleting(ctx, []string{agent.ID})
				require.NoError(t, err)
				require.Len(t, claims, 1)
				if i == 0 {
					first = claims[0].DeletingAt
				}
			}

			require.NoError(t, manager.lifecycle.DeleteAgent(ctx, agent.ID, first))

			all, err := st.Agents.ListAll(ctx)
			require.NoError(t, err)
			assert.Len(t, all, tt.wantLeft)
		})
	}
}

// TestDeleteWaitsOnAChildAnotherDeleteHolds pins a parent's delete tearing nothing down while
// a child claimed by an earlier delete is still going.
func TestDeleteWaitsOnAChildAnotherDeleteHolds(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	lifecycle := &fakeLifecycle{}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)
	ctx := t.Context()

	created, err := newWorkflowService(st, tmux).NewWorkflow(ctx, worktreeConfig(t, "backend"))
	require.NoError(t, err)
	parent, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:      "author",
		Dir:        t.TempDir(),
		WorkflowID: created.ID,
	})
	require.NoError(t, err)
	child, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:      "reviewer",
		WorkflowID: created.ID,
		Task:       *parent.TaskID,
	})
	require.NoError(t, err)

	childClaims, err := st.Agents.MarkDeleting(ctx, []string{child.ID})
	require.NoError(t, err)
	require.Len(t, childClaims, 1)
	parentClaims, err := st.Agents.MarkDeleting(ctx, []string{parent.ID})
	require.NoError(t, err)
	require.Len(t, parentClaims, 1, "the child is not taken twice")

	err = manager.lifecycle.DeleteAgent(ctx, parent.ID, parentClaims[0].DeletingAt)
	require.ErrorIs(t, err, agenterr.ErrReviewerDeleting)
	all, err := st.Agents.ListAll(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 2)

	require.NoError(t, manager.lifecycle.DeleteAgent(ctx, child.ID, childClaims[0].DeletingAt))
	require.NoError(t, manager.lifecycle.DeleteAgent(ctx, parent.ID, parentClaims[0].DeletingAt))

	all, err = st.Agents.ListAll(ctx)
	require.NoError(t, err)
	assert.Empty(t, all)
}

// TestFailDeleteGivesTheRowBack pins a delete given up on leaving the agent in the archive
// marked failed to clean up.
func TestFailDeleteGivesTheRowBack(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, &fakeLifecycle{})
	ctx := t.Context()

	created, err := newWorkflowService(st, tmux).NewWorkflow(ctx, worktreeConfig(t, "backend"))
	require.NoError(t, err)
	agent, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:      "agent",
		Dir:        t.TempDir(),
		WorkflowID: created.ID,
	})
	require.NoError(t, err)
	require.NoError(t, archiveWriters(ctx, manager.lifecycle, []string{agent.ID}))
	claims, err := st.Agents.MarkDeleting(ctx, []string{agent.ID})
	require.NoError(t, err)
	require.Len(t, claims, 1)

	require.NoError(t, manager.lifecycle.FailDelete(ctx, agent.ID, claims[0].DeletingAt.Add(-time.Second), errors.New("an older job")))
	all, err := st.Agents.ListAll(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.NotNil(t, all[0].DeletingAt, "a job the claim no longer names fails nothing")

	require.NoError(t, manager.lifecycle.FailDelete(ctx, agent.ID, claims[0].DeletingAt, errors.New("the store is away")))

	page, err := st.Rows.ListPage(ctx, rowstore.Page{Limit: 10, Archived: true})
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, agentstore.StateFailedToCleanup, page[0].State)
	assert.Contains(t, page[0].FailureReason, "the store is away")
	assert.Nil(t, page[0].DeletingAt)
}

// TestArchiveTaskTakesItsLiveAgents pins that archiving a task archives every live agent on it and nothing on another task.
func TestArchiveTaskTakesItsLiveAgents(t *testing.T) {
	st := newStore(t)
	manager := newManager(tmuxtest.NewFake(), st, "")
	ctx := t.Context()
	workflowID := seedWorkflow(t, st)
	work := st.SeedTask(t, taskstore.CreateParams{WorkflowID: workflowID, Slug: "work"})
	other := st.SeedTask(t, taskstore.CreateParams{WorkflowID: workflowID, Slug: "other"})
	author := seedOnTask(t, st, workflowID, "author", work.ID)
	followUp := seedOnTask(t, st, workflowID, "follow-up", work.ID)
	bystander := seedOnTask(t, st, workflowID, "bystander", other.ID)

	require.NoError(t, manager.lifecycle.ArchiveTask(ctx, work.Slug))

	assert.NotNil(t, agentByID(t, st, author.ID).ArchivedAt)
	assert.NotNil(t, agentByID(t, st, followUp.ID).ArchivedAt)
	assert.True(t, isTaskArchived(t, st, work.ID))
	assert.Nil(t, agentByID(t, st, bystander.ID).ArchivedAt, "an agent on another task stays live")
	assert.False(t, isTaskArchived(t, st, other.ID))
}

// TestArchivingAReviewerLeavesItsTaskLive pins that a reviewer leaving does not take its task with it.
func TestArchivingAReviewerLeavesItsTaskLive(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()
	manager := newManager(tmux, st, "")
	writer := seed(t, st, "writer", "", agentstore.StateIdle)
	reviewer := seedReviewer(t, st, tmux, writer)

	require.NoError(t, manager.lifecycle.ArchiveAgents(t.Context(), ArchiveAgentsParams{IDs: []string{reviewer.ID}}))

	assert.False(t, isTaskArchived(t, st, *writer.TaskID))
}

// TestRestoringAgentsRestoresTheirTasks pins that an agent never comes back under an archived task.
func TestRestoringAgentsRestoresTheirTasks(t *testing.T) {
	st := newStore(t)
	manager := newManager(tmuxtest.NewFake(), st, "")
	ctx := t.Context()
	workflowID := seedWorkflow(t, st)
	work := st.SeedTask(t, taskstore.CreateParams{WorkflowID: workflowID, Slug: "work"})
	author := seedOnTask(t, st, workflowID, "author", work.ID)
	require.NoError(t, archiveWriters(ctx, manager.lifecycle, []string{author.ID}))
	require.True(t, isTaskArchived(t, st, work.ID))

	_, err := manager.lifecycle.RestoreAgents(ctx, []string{author.ID})
	require.NoError(t, err)

	assert.False(t, isTaskArchived(t, st, work.ID))
}

func seedOnTask(t *testing.T, st *testStore, workflowID, name, taskID string) agentstore.Agent {
	t.Helper()

	require.NoError(t, st.createAgent(t.Context(), agentstore.Agent{
		Name:       name,
		State:      agentstore.StateIdle,
		WorkflowID: workflowID,
		TaskID:     &taskID,
	}))

	got, err := st.Agents.GetInWorkflow(t.Context(), workflowID, name)
	require.NoError(t, err)

	return *got
}

func isTaskArchived(t *testing.T, st *testStore, id string) bool {
	t.Helper()

	got, err := st.Tasks.Get(t.Context(), id)
	require.NoError(t, err)

	return got.ArchivedAt != nil
}

const refusingHook = "refuse-delete"

// refusingConfig is a worktree workflow whose agent.pre-delete hook can refuse a delete.
func refusingConfig(t *testing.T, name string) workflowstore.Config {
	t.Helper()

	config := worktreeConfig(t, name)
	config.Hooks = []workflowstore.Hook{{Event: workflowstore.HookAgentPreDelete, Script: refusingHook}}

	return config
}

type taskDeleteFixture struct {
	st       *testStore
	agents   *testAgents
	workflow *workflowstore.Workflow
}

func newTaskDeleteFixture(t *testing.T, lifecycle *fakeLifecycle, config workflowstore.Config) *taskDeleteFixture {
	t.Helper()

	tmux := tmuxtest.NewFake()
	st := newStore(t)
	built := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)
	created, err := st.workflowService(workflowServiceOptions{
		tmux:      tmux,
		lifecycle: lifecycle,
		hooks:     everyHook{},
	}).NewWorkflow(t.Context(), config)
	require.NoError(t, err)

	return &taskDeleteFixture{
		st:       st,
		agents:   built,
		workflow: created,
	}
}

func (f *taskDeleteFixture) launch(t *testing.T, title string) (*agentstore.Agent, *taskstore.Task) {
	t.Helper()

	agent, err := f.agents.launch.NewAgent(t.Context(), NewAgentParams{
		Title:      title,
		Dir:        t.TempDir(),
		WorkflowID: f.workflow.ID,
	})
	require.NoError(t, err)
	task, err := f.st.Tasks.Get(t.Context(), *agent.TaskID)
	require.NoError(t, err)
	require.NotEmpty(t, task.Checkout.Cwd)

	return agent, task
}

// TestDeleteTasksRefusesATaskNothingHolds pins that one ref nothing holds deletes none of the others.
func TestDeleteTasksRefusesATaskNothingHolds(t *testing.T) {
	fx := newTaskDeleteFixture(t, &fakeLifecycle{}, worktreeConfig(t, "backend"))
	agent, task := fx.launch(t, "auth")

	err := fx.agents.lifecycle.DeleteTasks(t.Context(), []string{task.Slug, "no-such-task"})
	require.ErrorIs(t, err, store.ErrNotFound)

	kept, err := fx.st.Tasks.Get(t.Context(), task.ID)
	require.NoError(t, err)
	assert.Nil(t, kept.ArchivedAt)
	assert.Nil(t, kept.DeletingAt)
	assert.Nil(t, agentByID(t, fx.st, agent.ID).ArchivedAt)
}

// TestDeleteTasksGivesBackATaskAnAgentWillNotLeave pins a task whose agent's cleanup fails staying
// archived and no longer claimed, with its checkout in place.
func TestDeleteTasksGivesBackATaskAnAgentWillNotLeave(t *testing.T) {
	lifecycle := &fakeLifecycle{hookErrs: map[string]error{refusingHook: errors.New("worktree is dirty")}}
	fx := newTaskDeleteFixture(t, lifecycle, refusingConfig(t, "backend"))
	agent, task := fx.launch(t, "auth")

	require.NoError(t, fx.agents.lifecycle.DeleteTasks(t.Context(), []string{task.ID}))

	kept, err := fx.st.Tasks.Get(t.Context(), task.ID)
	require.NoError(t, err)
	assert.NotNil(t, kept.ArchivedAt)
	assert.Nil(t, kept.DeletingAt, "the failed agent gives the task back")
	assert.Empty(t, lifecycle.ranTaskDeprovisions())
	assert.Equal(t, agentstore.StateFailedToCleanup, agentByID(t, fx.st, agent.ID).State)
}

// TestDeleteTasksKeepsATaskWhoseCheckoutWillNotGo pins a failed deprovision leaving the task archived
// with the reason, and deletable again.
func TestDeleteTasksKeepsATaskWhoseCheckoutWillNotGo(t *testing.T) {
	lifecycle := &fakeLifecycle{taskDeprovisionErr: errors.New("worktree is dirty")}
	fx := newTaskDeleteFixture(t, lifecycle, worktreeConfig(t, "backend"))
	_, task := fx.launch(t, "auth")

	require.NoError(t, fx.agents.lifecycle.DeleteTasks(t.Context(), []string{task.ID}), "the agent delete reports the failure itself")

	kept, err := fx.st.Tasks.Get(t.Context(), task.ID)
	require.NoError(t, err)
	assert.NotNil(t, kept.ArchivedAt)
	assert.Nil(t, kept.DeletingAt)
	assert.Contains(t, kept.FailureReason, "worktree is dirty")
	archived, err := fx.st.Tasks.List(t.Context(), fx.workflow.ID, true)
	require.NoError(t, err)
	assert.Len(t, archived, 1, "it is back in the archive")

	lifecycle.taskDeprovisionErr = nil
	require.NoError(t, fx.agents.lifecycle.DeleteTasks(t.Context(), []string{task.ID}))

	_, err = fx.st.Tasks.Get(t.Context(), task.ID)
	require.ErrorIs(t, err, store.ErrNotFound, "a second delete takes it")
	assert.Equal(t, []string{task.Checkout.Cwd, task.Checkout.Cwd}, lifecycle.ranTaskDeprovisions())
}

// TestDeleteTasksTakesItsAgentsCheckoutAndReviews pins a task going only once its agents have, with
// its worktree given back and its review rounds gone.
func TestDeleteTasksTakesItsAgentsCheckoutAndReviews(t *testing.T) {
	lifecycle := &fakeLifecycle{}
	fx := newTaskDeleteFixture(t, lifecycle, worktreeConfig(t, "backend"))
	writer, task := fx.launch(t, "auth")
	_, err := fx.agents.launch.NewAgent(t.Context(), reviewerOf(*writer, "reviewer"))
	require.NoError(t, err)
	_, other := fx.launch(t, "ui")
	reviews := reviewstore.NewReviewStore(fx.st.Pool.GetDB())
	_, err = reviews.CreateRound(t.Context(), reviewstore.CreateRoundParams{
		AgentID:     writer.ID,
		TaskID:      task.ID,
		BaseSHA:     "base",
		SnapshotSHA: "sha",
	})
	require.NoError(t, err)

	require.NoError(t, fx.agents.lifecycle.DeleteTasks(t.Context(), []string{task.Slug}))

	_, err = fx.st.Tasks.Get(t.Context(), task.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
	rounds, err := reviews.ListRounds(t.Context(), task.ID)
	require.NoError(t, err)
	assert.Empty(t, rounds)
	assert.Equal(t, []string{task.Checkout.Cwd}, lifecycle.ranTaskDeprovisions())
	assert.Empty(t, lifecycle.ranDeprovisions(), "no agent gives back a checkout itself")

	left, err := fx.st.Agents.ListAll(t.Context())
	require.NoError(t, err)
	require.Len(t, left, 1, "only the agent on the other task is left")
	assert.Equal(t, other.ID, *left[0].TaskID)
}
