package agents

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"weave/config"
	"weave/internal/frames"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	transcriptstore "weave/internal/store/transcript"
	workflowstore "weave/internal/store/workflow"

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
			tmux := newFakeTmux()
			st := newStore(t)
			lifecycle := &fakeLifecycle{}
			manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)

			created, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), worktreeConfig(t, "backend"))
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
				require.NoError(t, manager.lifecycle.DeleteAgents(t.Context(), []string{agent.ID}))
			}

			assert.Equal(t, tt.wantCleanup, lifecycle.deprovision)
		})
	}
}

// TestDeleteAgentRunsCleanupFromAnArchivedWorkflow covers deleting an agent out of the
// archive, where the worktree setup built is still on disk and the cleanup is the only
// thing that takes it back.
func TestDeleteAgentRunsCleanupFromAnArchivedWorkflow(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	lifecycle := &fakeLifecycle{}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)
	workflows := workflowsFor(st, tmux)

	created, err := workflows.NewWorkflow(t.Context(), worktreeConfig(t, "backend"))
	require.NoError(t, err)

	agent, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "agent",
		Dir:        t.TempDir(),
		WorkflowID: created.ID,
	})
	require.NoError(t, err)

	require.NoError(t, workflows.ArchiveWorkflow(t.Context(), created.ID))
	require.NoError(t, manager.lifecycle.DeleteAgents(t.Context(), []string{agent.ID}))

	assert.Equal(t, []string{worktreeDeprovisioner}, lifecycle.deprovision)
}

// TestDeleteAgentMarksFailedToCleanupWhenTeardownFails covers the other side of a teardown:
// a delete whose deprovisioner fails leaves the row behind, marked so, rather than
// dropping it the moment the delete was asked for and losing the only place left to see why.
func TestDeleteAgentMarksFailedToCleanupWhenTeardownFails(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	lifecycle := &fakeLifecycle{deprovisionRunErr: errors.New("worktree is dirty")}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)

	created, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), worktreeConfig(t, "backend"))
	require.NoError(t, err)

	agent, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "agent",
		Dir:        t.TempDir(),
		WorkflowID: created.ID,
	})
	require.NoError(t, err)

	require.NoError(t, manager.lifecycle.DeleteAgents(t.Context(), []string{agent.ID}))

	agents, listErr := st.List(t.Context())
	require.NoError(t, listErr)
	require.Len(t, agents, 1, "a failed teardown leaves the row rather than dropping it")
	assert.Equal(t, agentstore.StateFailedToCleanup, agents[0].State)
	assert.Contains(t, metadataValue(agents[0].Metadata), "worktree is dirty")
}

// TestDeleteStaysArchivedOnAFailedTeardown pins a failed cleanup leaving the row in the
// archive, which is where the user was working and so where the failure shows.
func TestDeleteStaysArchivedOnAFailedTeardown(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	lifecycle := &fakeLifecycle{deprovisionRunErr: errors.New("worktree is dirty")}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)

	created, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), worktreeConfig(t, "backend"))
	require.NoError(t, err)

	agent, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "agent",
		Dir:        t.TempDir(),
		WorkflowID: created.ID,
	})
	require.NoError(t, err)

	require.NoError(t, manager.lifecycle.ArchiveAgents(t.Context(), []string{agent.ID}))
	require.NoError(t, manager.lifecycle.DeleteAgents(t.Context(), []string{agent.ID}))

	live, listErr := st.List(t.Context())
	require.NoError(t, listErr)
	assert.Empty(t, live, "a teardown that failed does not put the agent back on the live list")

	all, listErr := st.ListAll(t.Context())
	require.NoError(t, listErr)
	require.Len(t, all, 1, "a failed teardown leaves the row rather than dropping it")
	assert.Equal(t, agentstore.StateFailedToCleanup, all[0].State)
	assert.NotNil(t, all[0].ArchivedAt)
}

// TestDeleteAgentRetryRecordsTheLatestReasonOnly pins a second failure overwriting the first,
// since there is one error slot per agent rather than a history of them.
func TestDeleteAgentRetryRecordsTheLatestReasonOnly(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	lifecycle := &fakeLifecycle{deprovisionRunErr: errors.New("worktree is dirty")}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)

	created, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), worktreeConfig(t, "backend"))
	require.NoError(t, err)

	agent, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "agent",
		Dir:        t.TempDir(),
		WorkflowID: created.ID,
	})
	require.NoError(t, err)

	require.NoError(t, manager.lifecycle.DeleteAgents(t.Context(), []string{agent.ID}))

	agents, err := st.List(t.Context())
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Contains(t, metadataValue(agents[0].Metadata), "worktree is dirty")

	lifecycle.deprovisionRunErr = errors.New("network unreachable")
	require.NoError(t, manager.lifecycle.DeleteAgents(t.Context(), []string{agent.ID}))

	agents, err = st.List(t.Context())
	require.NoError(t, err)
	require.Len(t, agents, 1, "still failing, so still here")
	assert.Equal(t, agentstore.StateFailedToCleanup, agents[0].State)
	assert.Contains(t, metadataValue(agents[0].Metadata), "network unreachable")
	assert.NotContains(t, metadataValue(agents[0].Metadata), "worktree is dirty", "the stale reason does not linger alongside the new one")
}

// TestDeleteAgentsHandlesMixedCleanupOutcomes pins each agent's cleanup outcome landing on
// that agent alone rather than on the batch.
func TestDeleteAgentsHandlesMixedCleanupOutcomes(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	lifecycle := &fakeLifecycle{}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)
	workflows := workflowsFor(st, tmux)

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

	require.NoError(t, manager.lifecycle.DeleteAgents(t.Context(), []string{ok.ID, bad.ID}))

	agents, err := st.List(t.Context())
	require.NoError(t, err)
	require.Len(t, agents, 1, "the one that succeeded is gone, the one that failed is left")
	assert.Equal(t, bad.ID, agents[0].ID)
	assert.Equal(t, agentstore.StateFailedToCleanup, agents[0].State)
	assert.Contains(t, metadataValue(agents[0].Metadata), "container is still running")
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
			tmux := newFakeTmux()
			st := newStore(t)
			lifecycle := &fakeLifecycle{deprovisionRunErr: tt.cleanupErr}
			logsDir := t.TempDir()
			manager := st.agentService(agentServiceOptions{
				tmux:      tmux,
				harnesses: fakeHarnesses{harness: &fakeHarness{}},
				addr:      "127.0.0.1:9999",
				lifecycle: lifecycle,
				logsDir:   logsDir,
			})

			cfg := workflowstore.Config{Name: "backend"}
			if tt.name != "no cleanup configured" {
				cfg = worktreeConfig(t, "backend")
			}
			created, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), cfg)
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

			require.NoError(t, manager.lifecycle.DeleteAgents(t.Context(), []string{agent.ID}))

			_, dirErr := os.Stat(logDir)
			if tt.wantRemoved {
				assert.True(t, os.IsNotExist(dirErr), "every run goes with a row that is really gone")
			} else {
				assert.NoError(t, dirErr, "the row is still here to read the failure from, so its runs stay too")
			}
		})
	}
}

// TestDeleteAgentRemovesItsRecordedFrames pins a delete reclaiming what a recording kept for
// that agent, since a recording is bounded per agent and nothing else ever clears it.
func TestDeleteAgentRemovesItsRecordedFrames(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	framesDir := t.TempDir()
	manager := st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: fakeHarnesses{harness: &fakeHarness{}},
		addr:      "127.0.0.1:9999",
		lifecycle: &fakeLifecycle{},
		framesDir: framesDir,
	})

	created, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{Name: "backend"})
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

	require.NoError(t, manager.lifecycle.DeleteAgents(t.Context(), []string{agent.ID}))

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
			tmux := newFakeTmux()
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
			assert.Contains(t, tmux.killedPanes, "%1", "the agent's pane is killed")

			_, err = st.Get(t.Context(), tt.seed)
			assert.ErrorIs(t, err, store.ErrNotFound, "the agent is hidden from listings")
		})
	}
}

// TestArchiveKillsAPaneWrittenBeforeItsOwnWrite pins the archive killing the pane its own
// write took, since the pane landed on the row after its read saw nothing there.
func TestArchiveKillsAPaneWrittenBeforeItsOwnWrite(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	ctx := t.Context()

	agent := seed(t, st, "dormant", "", agentstore.StateIdle)

	manager := st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: fakeHarnesses{harness: &fakeHarness{}},
		addr:      "127.0.0.1:9999",
		agents:    writingPaneFirst{Store: st.Agents, inTheGap: resumeInto(t, st, tmux, agent, "%9")},
	})

	require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, []string{agent.ID}))

	after := agentByID(t, st, agent.ID)
	require.NotNil(t, after.ArchivedAt, "the row really is archived")
	assert.Empty(t, after.Pane, "and holds no pane for anything else to find it by")
	assert.Contains(t, tmux.killedPanes, "%9", "so the archive kills the pane its write took")
}

// TestDeleteKillsAPaneWrittenBeforeItsOwnWrite is the same gap on the delete, whose row goes
// entirely rather than being hidden, so a pane written into the gap is even further out of
// reach afterwards.
func TestDeleteKillsAPaneWrittenBeforeItsOwnWrite(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	ctx := t.Context()

	agent := seed(t, st, "dormant", "", agentstore.StateIdle)

	manager := st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: fakeHarnesses{harness: &fakeHarness{}},
		addr:      "127.0.0.1:9999",
		tx:        txWritingPaneFirst{txRunner: st.Pool, inTheGap: resumeInto(t, st, tmux, agent, "%9")},
	})

	require.NoError(t, manager.lifecycle.DeleteAgents(ctx, []string{agent.ID}))

	all, err := st.ListAll(ctx)
	require.NoError(t, err)
	assert.Empty(t, all, "the row really is gone")
	assert.Contains(t, tmux.killedPanes, "%9", "so the delete kills the pane its write took")
}

// TestDeleteAgent covers destroying an agent for good, keyed by id since the unique name
// index holds among live rows only and these agents are archived.
func TestDeleteAgent(t *testing.T) {
	tests := []struct {
		name           string
		archive        bool
		target         func(seeded agentstore.Agent) string
		wantPaneKilled bool
		wantErrIs      error
	}{
		{
			name:      "an id nobody holds is not a quiet success",
			target:    func(agentstore.Agent) string { return "019fa434-e7a7-7ac9-bab5-718681da797d" },
			wantErrIs: ErrAgentNotFound,
		},
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
			tmux := newFakeTmux()
			manager := newManager(tmux, st, "")
			ctx := t.Context()

			seeded := seedRunning(t, st, tmux, "target", "%1", agentstore.StateThinking)
			require.NoError(t, st.Transcripts.Upsert(ctx, transcriptstore.UpsertParams{
				AgentID: seeded.ID, Content: "something it said", Offset: 1,
			}))
			if tt.archive {
				require.NoError(t, st.SoftDelete(ctx, seeded.Name))
			}

			err := manager.lifecycle.DeleteAgents(ctx, []string{tt.target(seeded)})
			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)

				all, listErr := st.ListAll(ctx)
				require.NoError(t, listErr)
				assert.Len(t, all, 1, "a delete that matched nothing removes nothing")

				return
			}

			require.NoError(t, err)
			if tt.wantPaneKilled {
				assert.Contains(t, tmux.killedPanes, "%1", "the agent's pane is killed")
			}

			all, err := st.ListAll(ctx)
			require.NoError(t, err)
			assert.Empty(t, all, "the row is gone from every listing, not hidden from one")

			tr, err := st.GetTranscript(ctx, seeded.ID)
			require.NoError(t, err)
			assert.Empty(t, tr.Content, "the transcript goes with the agent")
		})
	}
}

// TestAReissuedPaneIsNotTheAgents pins every read of a stored pane id surviving reissue,
// since tmux only promises the id is unique while the server that issued it lives.
func TestAReissuedPaneIsNotTheAgents(t *testing.T) {
	const shared = "%4"

	setup := func(t *testing.T) (*testAgents, *testStore, *fakeTmux, agentstore.Agent) {
		t.Helper()
		st := newStore(t)
		tmux := newFakeTmux()
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
		assert.Empty(t, tmux.killedPanes, "killing by a stale id would stop the agent using that pane now")
	})

	t.Run("deleting it leaves the pane running", func(t *testing.T) {
		manager, _, tmux, dormant := setup(t)
		require.NoError(t, manager.lifecycle.DeleteAgents(t.Context(), []string{dormant.ID}))
		assert.Empty(t, tmux.killedPanes, "killing by a stale id would stop the agent using that pane now")
	})

	t.Run("resuming it opens a window rather than adopting the pane", func(t *testing.T) {
		manager, st, tmux, dormant := setup(t)
		resumed, err := manager.resume.ResumeAgent(t.Context(), dormant.ID)
		require.NoError(t, err)
		assert.NotEqual(t, shared, resumed.Pane, "a resume that read the pane as its own would relaunch nothing")
		assert.Equal(t, dormant.ID, tmux.owners[resumed.Pane], "and the new pane is its own")

		still := agentByID(t, st, dormant.ID)
		assert.Equal(t, resumed.Pane, still.Pane)
	})

	t.Run("a reconcile pass forgets the claim without reading the pane", func(t *testing.T) {
		_, st, tmux, dormant := setup(t)
		tmux.captures[shared] = busyFrame

		rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents)
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
			tmux := newFakeTmux()
			manager := newManager(tmux, st, "")
			ctx := t.Context()

			workflowID := seedWorkflow(t, st)
			require.NoError(t, st.Create(ctx, agentstore.Agent{
				Name:       "foo",
				State:      agentstore.StateIdle,
				SessionID:  "sess-old",
				WorkflowID: workflowID,
			}))
			original, err := st.Agents.GetInWorkflow(ctx, workflowID, "foo")
			require.NoError(t, err)
			require.NoError(t, st.SoftDelete(ctx, "foo"))

			if tt.reuseName {
				require.NoError(t, st.Create(ctx, agentstore.Agent{
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
			assert.Empty(t, tmux.owners, "a restore starts nothing, so no pane was opened")
			assert.False(t, manager.panes.IsAgentRunning(restored), "it comes back dormant, waiting to be resumed")

			resumed, err := manager.resume.ResumeAgent(ctx, original.ID)
			require.NoError(t, err)
			assert.Equal(t, original.ID, tmux.owners[resumed.Pane], "resuming stamps the pane it came back in")
			assert.True(t, manager.panes.IsAgentRunning(*resumed), "so it reads as running again")
		})
	}
}

// TestRestoreCeiling pins a restore being refused where a create would be, or the archive is
// a way around the limit.
func TestRestoreCeiling(t *testing.T) {
	tmux := newFakeTmux()
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
