package agents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"komrad/config"
	"komrad/internal/frames"
	"komrad/internal/harness/pane"
	"komrad/internal/harnesstest"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	reviewstore "komrad/internal/store/review"
	rowstore "komrad/internal/store/row"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/tmuxtest"
	"komrad/lib/xslice"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeleteAgentRunsWorkflowCleanup covers cleanup firing on the irreversible half only.
func TestDeleteAgentRunsWorkflowCleanup(t *testing.T) {
	tests := []struct {
		name        string
		archive     bool
		wantCleanup []string
	}{
		{
			name:        "deleting an agent releases what setup built",
			wantCleanup: []string{worktreeDeprovisioner},
		},
		{
			name:    "archiving one keeps it",
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

			if tt.archive {
				require.NoError(t, manager.lifecycle.ArchiveAgents(t.Context(), []string{agent.ID}))
			} else {
				require.NoError(t, manager.lifecycle.deleteNow(t.Context(), []string{agent.ID}))
			}

			assert.Equal(t, tt.wantCleanup, lifecycle.deprovision)
		})
	}
}

// TestDeleteAgentRunsCleanupFromAnArchivedWorkflow covers deleting an agent out of the
// archive, where the worktree setup built is still on disk and the cleanup is the only
// thing that takes it back.
func TestDeleteAgentRunsCleanupFromAnArchivedWorkflow(t *testing.T) {
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

	require.NoError(t, workflows.ArchiveWorkflow(t.Context(), created.ID))
	require.NoError(t, manager.lifecycle.deleteNow(t.Context(), []string{agent.ID}))

	assert.Equal(t, []string{worktreeDeprovisioner}, lifecycle.deprovision)
}

// TestDeleteAgentMarksFailedToCleanupWhenTeardownFails covers the other side of a teardown: a
// delete whose deprovisioner fails leaves the row behind, marked so.
func TestDeleteAgentMarksFailedToCleanupWhenTeardownFails(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	lifecycle := &fakeLifecycle{deprovisionRunErr: errors.New("worktree is dirty")}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

	created, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), worktreeConfig(t, "backend"))
	require.NoError(t, err)

	agent, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "agent",
		Dir:        t.TempDir(),
		WorkflowID: created.ID,
	})
	require.NoError(t, err)

	require.NoError(t, manager.lifecycle.deleteNow(t.Context(), []string{agent.ID}))

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
			lifecycle := &fakeLifecycle{deprovisionRunErr: tt.teardownErr}
			manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

			created, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), worktreeConfig(t, "backend"))
			require.NoError(t, err)

			agent, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
				Title:      "agent",
				Dir:        t.TempDir(),
				WorkflowID: created.ID,
			})
			require.NoError(t, err)
			require.NoError(t, manager.lifecycle.ArchiveAgents(t.Context(), []string{agent.ID}))

			_, err = st.Agents.MarkDeleting(t.Context(), []string{agent.ID})
			require.NoError(t, err)

			page, err := st.Rows.ListPage(t.Context(), rowstore.Page{Limit: 10, Archived: true})
			require.NoError(t, err)
			assert.Empty(t, page, "the row is out of the archive while the teardown is owed")
			all, err := st.Agents.ListAll(t.Context())
			require.NoError(t, err)
			require.Len(t, all, 1, "and still there")
			assert.NotNil(t, all[0].DeletingAt)

			require.NoError(t, manager.lifecycle.DeleteAgent(t.Context(), agent.ID))

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
	lifecycle := &fakeLifecycle{deprovisionRunErr: errors.New("worktree is dirty")}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

	created, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), worktreeConfig(t, "backend"))
	require.NoError(t, err)

	agent, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "agent",
		Dir:        t.TempDir(),
		WorkflowID: created.ID,
	})
	require.NoError(t, err)

	require.NoError(t, manager.lifecycle.ArchiveAgents(t.Context(), []string{agent.ID}))
	require.NoError(t, manager.lifecycle.deleteNow(t.Context(), []string{agent.ID}))

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
	lifecycle := &fakeLifecycle{deprovisionRunErr: errors.New("worktree is dirty")}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

	created, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), worktreeConfig(t, "backend"))
	require.NoError(t, err)

	agent, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "agent",
		Dir:        t.TempDir(),
		WorkflowID: created.ID,
	})
	require.NoError(t, err)

	require.NoError(t, manager.lifecycle.deleteNow(t.Context(), []string{agent.ID}))

	agents, err := st.Agents.List(t.Context())
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Contains(t, agents[0].FailureReason, "worktree is dirty")

	lifecycle.deprovisionRunErr = errors.New("network unreachable")
	require.NoError(t, manager.lifecycle.deleteNow(t.Context(), []string{agent.ID}))

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
	frontend, err := workflows.NewWorkflow(t.Context(), worktreeConfig(t, "frontend"))
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

	lifecycle.deprovisionRunErrByAgent = map[string]error{bad.ID: errors.New("container is still running")}

	require.NoError(t, manager.lifecycle.deleteNow(t.Context(), []string{ok.ID, bad.ID}))

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
			lifecycle := &fakeLifecycle{deprovisionRunErr: tt.cleanupErr}
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
				cfg = worktreeConfig(t, "backend")
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
			provisionPath := filepath.Join(logDir, config.RunLogName(1, config.TriggerProvision, "make-worktree"))
			deprovisionPath := filepath.Join(logDir, config.RunLogName(2, config.TriggerDeprovision, "drop-worktree"))
			require.NoError(t, os.WriteFile(provisionPath, []byte("cloning\n"), 0o644))
			require.NoError(t, os.WriteFile(deprovisionPath, []byte("dropping\n"), 0o644))

			require.NoError(t, manager.lifecycle.deleteNow(t.Context(), []string{agent.ID}))

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
		seed      string
		missing   bool
		wantErrIs error
	}{
		{
			name:      "an id nobody holds is not a quiet success",
			missing:   true,
			wantErrIs: ErrAgentNotFound,
		},
		{
			name: "a live agent is stopped and hidden",
			seed: "target",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := tmuxtest.NewFake()
			manager := newManager(tmux, st, "")

			target := "00000000-0000-0000-0000-000000000000"
			if tt.seed != "" {
				target = seedRunning(t, st, tmux, tt.seed, "%1", agentstore.StateThinking).ID
			}
			if tt.missing {
				target = "00000000-0000-0000-0000-000000000000"
			}

			err := manager.lifecycle.ArchiveAgents(t.Context(), []string{target})
			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
				return
			}

			require.NoError(t, err)
			assert.Contains(t, tmux.KilledPanes, "%1", "the agent's pane is killed")

			_, err = st.Get(t.Context(), tt.seed)
			require.ErrorIs(t, err, store.ErrNotFound, "the agent is hidden from listings")

			archived := agentByID(t, st, target)
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

	require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, []string{agent.ID}))

	after := agentByID(t, st, agent.ID)
	require.NotNil(t, after.ArchivedAt, "the row really is archived")
	assert.Empty(t, after.Pane, "and holds no pane for anything else to find it by")
	assert.Contains(t, tmux.KilledPanes, "%9", "so the archive kills the pane its write took")
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
		require.NoError(t, manager.lifecycle.ArchiveAgents(t.Context(), []string{dormant.ID}))
		assert.Empty(t, tmux.KilledPanes, "killing by a stale id would stop the agent using that pane now")
	})

	t.Run("deleting it leaves the pane running", func(t *testing.T) {
		manager, _, tmux, dormant := setup(t)
		require.NoError(t, manager.lifecycle.deleteNow(t.Context(), []string{dormant.ID}))
		assert.Empty(t, tmux.KilledPanes, "killing by a stale id would stop the agent using that pane now")
	})

	t.Run("resuming it opens a window rather than adopting the pane", func(t *testing.T) {
		manager, st, tmux, dormant := setup(t)
		resumed, err := manager.resume.ResumeAgent(t.Context(), dormant.ID)
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

			resumed, err := manager.resume.ResumeAgent(ctx, original.ID)
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

	require.ErrorIs(t, err, ErrTooManyAgents)
	still := agentByID(t, st, archived.ID)
	assert.NotNil(t, still.ArchivedAt, "the refused restore leaves it in the archive")

	require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, []string{liveTwo.ID}))
	_, err = manager.lifecycle.RestoreAgents(ctx, []string{archived.ID})
	require.NoError(t, err, "archiving one makes room again")
}

// TestDeletingAnAgentLeavesAnotherAgentsRoundHeld is what the pinned snapshot dates cost: two
// agents on one base whose worktrees hash the same produce the same commit, so the ref holding a
// round has to be named per agent or either of them deleting pulls the other's out from under it.
func TestDeletingAnAgentLeavesAnotherAgentsRoundHeld(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, &fakeLifecycle{})
	reviews := reviewstore.NewReviewStore(st.Pool.GetDB())

	root := t.TempDir()
	going := seedRoundedAgent(t, st, reviews, "going", root, "same-sha")
	seedRoundedAgent(t, st, reviews, "staying", root, "same-sha")

	require.NoError(t, manager.lifecycle.deleteNow(t.Context(), []string{going}))

	assert.Equal(t, []string{reviewstore.RoundRef(going, "same-sha")}, manager.snapshots.Released,
		"only the going agent's round is let go of, since the other's ref names the other agent")
}

// TestDeletingAnAgentReleasesItsRoundsFromACheckoutThatStands pins that a removed worktree hands
// the release to its workflow's checkout, which shares the refs.
func TestDeletingAnAgentReleasesItsRoundsFromACheckoutThatStands(t *testing.T) {
	tests := []struct {
		name            string
		worktreeRemoved bool
		wantWorkflowDir bool
	}{
		{
			name:            "a worktree already removed",
			worktreeRemoved: true,
			wantWorkflowDir: true,
		},
		{
			name:            "a worktree that stands",
			worktreeRemoved: false,
			wantWorkflowDir: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			st := newStore(t)
			manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, &fakeLifecycle{})
			reviews := reviewstore.NewReviewStore(st.Pool.GetDB())

			flow, err := st.Workflows.Create(t.Context(), worktreeConfig(t, "workflow-"+uuid.NewString()))
			require.NoError(t, err)

			agentDir := t.TempDir()
			require.NoError(t, st.createAgent(t.Context(), agentstore.Agent{
				Name:       "going",
				Cwd:        agentDir,
				State:      agentstore.StateIdle,
				WorkflowID: flow.ID,
			}))
			going, err := st.Get(t.Context(), "going")
			require.NoError(t, err)

			_, err = reviews.CreateRound(t.Context(), reviewstore.CreateRoundParams{
				AgentID:     going.ID,
				BaseSHA:     "base",
				SnapshotSHA: "sha",
			})
			require.NoError(t, err)
			require.NoError(t, st.Agents.SetCheckout(t.Context(), going.ID, agentstore.SetCheckoutParams{WorktreeRemoved: tt.worktreeRemoved}))

			require.NoError(t, manager.lifecycle.deleteNow(t.Context(), []string{going.ID}))

			want := agentDir
			if tt.wantWorkflowDir {
				want = flow.Cwd
			}
			assert.Equal(t, []string{want}, manager.snapshots.ReleasedFrom)
		})
	}
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

	round, err := reviews.GetLatestRound(t.Context(), worker)
	require.NoError(t, err)

	// The first reviewer has had its say and started another; the second is still writing its
	// first.
	released := openBatch(t, reviews, worker, round.ID, first)
	_, err = reviews.ReleaseReview(t.Context(), reviewstore.ReleaseReviewParams{
		ID:      released.ID,
		Verdict: reviewstore.VerdictChangesRequested,
		Path:    "/tmp/round-1.md",
	})
	require.NoError(t, err)

	openBatch(t, reviews, worker, round.ID, first)
	openBatch(t, reviews, worker, round.ID, second)

	require.NoError(t, manager.lifecycle.deleteNow(t.Context(), []string{first, second}),
		"two reviewers holding open batches on one agent both delete")

	left, err := reviews.ListReviews(t.Context(), worker)
	require.NoError(t, err)
	require.Len(t, left, 1, "the drafts went and the released review stayed")
	assert.Nil(t, left[0].AuthorAgentID, "with its author nulled rather than the row taken")
	assert.NotNil(t, left[0].ReleasedAt)
}

// openBatch starts a review by one agent on another.
func openBatch(t *testing.T, reviews *reviewstore.ReviewStore, agentID, roundID, author string) *reviewstore.Review {
	t.Helper()

	review, err := reviews.OpenReview(t.Context(), reviewstore.OpenReviewParams{
		AgentID:       agentID,
		RoundID:       roundID,
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

	require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, []string{auth.ID, api.ID, ui.ID}))

	for _, agent := range []agentstore.Agent{auth, api, ui} {
		assert.NotNil(t, agentByID(t, st, agent.ID).ArchivedAt, "%s is archived", agent.Name)
	}

	assert.ElementsMatch(t, []string{"%1", "%2"}, tmux.KilledPanes, "only the running agents had a pane to kill")

	live, err := st.Agents.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, live, "the batch spanned both workflows")
}

// TestArchiveUnchangedAgents covers the guard a background pass needs.
func TestArchiveUnchangedAgents(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()
	manager := newManager(tmux, st, "")
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	settled := seedRunningIn(t, st, tmux, workflowID, "settled", "%1")
	moved := seedInWorkflow(t, st, workflowID, "moved", "", agentstore.StateIdle)

	observed := []agentstore.Agent{settled, moved}

	require.NoError(t, st.Agents.SetStateByID(ctx, moved.ID, agentstore.StateThinking))

	archived, err := manager.lifecycle.ArchiveUnchangedAgents(ctx, observed)
	require.NoError(t, err)

	require.Len(t, archived, 1)
	assert.Equal(t, settled.ID, archived[0].ID, "only the row that still read as it was observed is archived")
	assert.NotNil(t, agentByID(t, st, settled.ID).ArchivedAt)
	assert.Nil(t, agentByID(t, st, moved.ID).ArchivedAt, "the row that moved since the snapshot is left for a later pass")
	assert.Equal(t, []string{"%1"}, tmux.KilledPanes, "the pane goes with the archive it belongs to")
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

			auth := seedRunning(t, st, tmux, "auth", "%1", agentstore.StateThinking)
			api := seedRunning(t, st, tmux, "api", "%2", agentstore.StateThinking)

			err := manager.lifecycle.ArchiveAgents(ctx, []string{auth.ID, tt.stray, api.ID})

			require.ErrorIs(t, err, ErrAgentNotFound)
			require.ErrorContains(t, err, tt.stray, "the refusal names what it could not find")
			assert.Empty(t, tmux.KilledPanes, "nothing was stopped")

			live, err := st.Agents.List(ctx)
			require.NoError(t, err)
			assert.Len(t, live, 2, "and nothing was archived")
		})
	}
}

// TestDeleteAgentsFiresEveryWorkflowsCleanup pins each agent's own worktree being removed.
func TestDeleteAgentsFiresEveryWorkflowsCleanup(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()
	lifecycle := &fakeLifecycle{}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)
	workflows := newWorkflowService(st, tmux)
	ctx := t.Context()

	backend, err := workflows.NewWorkflow(ctx, worktreeConfig(t, "backend"))
	require.NoError(t, err)
	frontend, err := workflows.NewWorkflow(ctx, worktreeConfig(t, "frontend"))
	require.NoError(t, err)

	auth, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:      "auth",
		Dir:        t.TempDir(),
		WorkflowID: backend.ID,
	})
	require.NoError(t, err)
	ui, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:      "ui",
		Dir:        t.TempDir(),
		WorkflowID: frontend.ID,
	})
	require.NoError(t, err)

	require.NoError(t, manager.lifecycle.deleteNow(ctx, []string{auth.ID, ui.ID}))

	assert.Equal(t, []string{worktreeDeprovisioner, worktreeDeprovisioner}, lifecycle.deprovision,
		"one run per agent, whichever order the listing put them in")

	all, err := st.Agents.ListAll(ctx)
	require.NoError(t, err)
	assert.Empty(t, all)
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

	require.ErrorIs(t, err, ErrAgentNotFound)
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
	require.ErrorIs(t, err, ErrTooManyAgents, "three back into a panel of three holding one is refused")

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

	err := manager.lifecycle.ArchiveAgents(t.Context(), ids)

	require.ErrorIs(t, err, ErrAgentNotFound)
	require.ErrorContains(t, err, ids[0], "the first few are named")
	require.ErrorContains(t, err, "and 15 more", "and the rest are counted")
	assert.NotContains(t, err.Error(), ids[19], "rather than spelled out")
}

// TestArchiveUnchangedAgentsStampsAfterTheKill pins the ordering the transcript sync rests on:
// an agent's archived stamp is written again once the CLI holding its log is dead.
func TestArchiveUnchangedAgentsStampsAfterTheKill(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()
	agents := &restampWatcher{lifecycleAgentReadWriter: st.Agents, tmux: tmux}
	manager := newManager(tmux, st, "")
	manager.lifecycle.agents = agents
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	settled := seedRunningIn(t, st, tmux, workflowID, "settled", "%1")

	archived, err := manager.lifecycle.ArchiveUnchangedAgents(ctx, []agentstore.Agent{settled})
	require.NoError(t, err)
	require.Len(t, archived, 1)

	assert.Equal(t, []string{"%1"}, agents.killedWhenRestamped,
		"the pane is already gone when the stamp is written again")
}

// restampWatcher records which panes had been killed by the time the archived stamp was
// written again.
type restampWatcher struct {
	lifecycleAgentReadWriter
	tmux                *tmuxtest.Fake
	killedWhenRestamped []string
}

func (r *restampWatcher) RestampArchived(ctx context.Context, ids []string) error {
	r.killedWhenRestamped = append([]string(nil), r.tmux.KilledPanes...)

	return r.lifecycleAgentReadWriter.RestampArchived(ctx, ids)
}

// TestRestoreOfAChildUnderTheChildCeiling pins that a child comes back only while its parent
// still has room.
func TestRestoreOfAChildUnderTheChildCeiling(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}},
		addr:      "127.0.0.1:9999",
		usage:     config.Usage{Implementer: config.ImplementerUsage{MaxChildren: 1}},
	})
	ctx := t.Context()

	parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
	require.NoError(t, st.Agents.Update(ctx, withCwd(parent, t.TempDir())))

	first, err := manager.launch.NewAgent(ctx, childOf(parent, "reviewer"))
	require.NoError(t, err)
	require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, []string{first.ID}))

	_, err = manager.launch.NewAgent(ctx, childOf(parent, "second-reviewer"))
	require.NoError(t, err, "the ceiling frees up once one child is archived")

	_, err = manager.lifecycle.RestoreAgents(ctx, []string{first.ID})
	require.ErrorIs(t, err, ErrTooManyChildren)
	assert.NotNil(t, agentByID(t, st, first.ID).ArchivedAt, "the refusal restores nothing")

	held, err := st.Agents.CountLiveChildren(ctx, parent.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, held)
}

// family is a parent and the child attached to it.
type family struct {
	parent agentstore.Agent
	child  agentstore.Agent
}

// TestTheFamilyMovesTogether pins that a parent's archive, delete and restore carry its children
// and a child's own leave the parent.
func TestTheFamilyMovesTogether(t *testing.T) {
	tests := []struct {
		name     string
		act      func(ctx context.Context, lifecycle *LifecycleService, members family) error
		wantLive []string
		wantKept []string
	}{
		{
			name: "archiving the parent takes the child",
			act: func(ctx context.Context, lifecycle *LifecycleService, members family) error {
				return lifecycle.ArchiveAgents(ctx, []string{members.parent.ID})
			},
			wantKept: []string{"author", "reviewer"},
		},
		{
			name: "archiving both is the same archive",
			act: func(ctx context.Context, lifecycle *LifecycleService, members family) error {
				return lifecycle.ArchiveAgents(ctx, []string{members.parent.ID, members.child.ID})
			},
			wantKept: []string{"author", "reviewer"},
		},
		{
			name: "archiving the child leaves the parent",
			act: func(ctx context.Context, lifecycle *LifecycleService, members family) error {
				return lifecycle.ArchiveAgents(ctx, []string{members.child.ID})
			},
			wantLive: []string{"author"},
			wantKept: []string{"author", "reviewer"},
		},
		{
			name: "deleting the parent takes the child",
			act: func(ctx context.Context, lifecycle *LifecycleService, members family) error {
				return lifecycle.deleteNow(ctx, []string{members.parent.ID})
			},
		},
		{
			name: "deleting the child leaves the parent",
			act: func(ctx context.Context, lifecycle *LifecycleService, members family) error {
				return lifecycle.deleteNow(ctx, []string{members.child.ID})
			},
			wantLive: []string{"author"},
			wantKept: []string{"author"},
		},
		{
			name: "restoring the parent brings the child back",
			act: func(ctx context.Context, lifecycle *LifecycleService, members family) error {
				if err := lifecycle.ArchiveAgents(ctx, []string{members.parent.ID}); err != nil {
					return err
				}

				_, err := lifecycle.RestoreAgents(ctx, []string{members.parent.ID})

				return err
			},
			wantLive: []string{"author", "reviewer"},
			wantKept: []string{"author", "reviewer"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			st := newStore(t)
			manager := newManager(tmux, st, "")
			ctx := t.Context()

			parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
			child := seedChild(t, st, tmux, parent)

			require.NoError(t, tt.act(ctx, manager.lifecycle, family{
				parent: parent,
				child:  child,
			}))

			live, err := st.Agents.List(ctx)
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.wantLive, collectAgentNames(live))

			kept, err := st.Agents.ListAll(ctx)
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.wantKept, collectAgentNames(kept))
			assert.Contains(t, tmux.KilledPanes, child.Pane, "the child's pane goes with its row")
		})
	}
}

// TestRestoreOfAChildNeedsItsParentBack pins the one direction the family rule still holds in:
// a child comes back only once there is a live row for it to be drawn under.
func TestRestoreOfAChildNeedsItsParentBack(t *testing.T) {
	tests := []struct {
		name        string
		restoreBoth bool
		wantErrIs   error
	}{
		{
			name:        "the parent is live again",
			restoreBoth: true,
		},
		{
			name:      "the parent is still archived",
			wantErrIs: ErrParentStillArchived,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			st := newStore(t)
			manager := newManager(tmux, st, "")
			ctx := t.Context()

			parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
			child := seedChild(t, st, tmux, parent)

			require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, []string{parent.ID}))
			if tt.restoreBoth {
				_, err := manager.lifecycle.RestoreAgents(ctx, []string{parent.ID})
				require.NoError(t, err)
				require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, []string{child.ID}))
			}

			restored, err := manager.lifecycle.RestoreAgents(ctx, []string{child.ID})
			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
				assert.NotNil(t, agentByID(t, st, child.ID).ArchivedAt, "the refusal restores nothing")

				return
			}

			require.NoError(t, err)
			assert.Len(t, restored, 1)
			assert.Nil(t, agentByID(t, st, child.ID).ArchivedAt)
		})
	}
}

func collectAgentNames(agents []agentstore.Agent) []string {
	return xslice.Map(agents, func(agent agentstore.Agent) string {
		return agent.Name
	})
}

// TestDeleteAgentTakesOnlyAClaimedAgent pins a delete job leaving alone an agent whose claim
// was given back before it ran.
func TestDeleteAgentTakesOnlyAClaimedAgent(t *testing.T) {
	tests := []struct {
		name            string
		claim           bool
		wantDeprovision []string
		wantLeft        int
	}{
		{
			name:            "a claimed agent is torn down and removed",
			claim:           true,
			wantDeprovision: []string{worktreeDeprovisioner},
		},
		{
			name:     "an agent nobody claimed is left alone",
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
			if tt.claim {
				_, err = st.Agents.MarkDeleting(ctx, []string{agent.ID})
				require.NoError(t, err)
			}

			require.NoError(t, manager.lifecycle.DeleteAgent(ctx, agent.ID))

			assert.Equal(t, tt.wantDeprovision, lifecycle.deprovision)
			all, err := st.Agents.ListAll(ctx)
			require.NoError(t, err)
			assert.Len(t, all, tt.wantLeft)
		})
	}
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
	require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, []string{agent.ID}))
	_, err = st.Agents.MarkDeleting(ctx, []string{agent.ID})
	require.NoError(t, err)

	require.NoError(t, manager.lifecycle.FailDelete(ctx, agent.ID, errors.New("the store is away")))

	page, err := st.Rows.ListPage(ctx, rowstore.Page{Limit: 10, Archived: true})
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, agentstore.StateFailedToCleanup, page[0].State)
	assert.Contains(t, page[0].FailureReason, "the store is away")
	assert.Nil(t, page[0].DeletingAt)
}
