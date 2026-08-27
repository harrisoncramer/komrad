package agents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResumeDir covers the directory a restore relaunches in, and whether the
// harness is asked to carry the conversation there. A restore has to produce a
// usable directory whatever became of the recorded one, since a harness may match
// a resume against the path the agent ran in and the panel lists agents whose
// worktree is long gone.
func TestResumeDir(t *testing.T) {
	const sessionID = "6f1b0c34-0b6b-4a17-9d1f-1f4b6c0e9a11"

	tests := []struct {
		name string

		cwd func(t *testing.T, root string) string

		wantDir func(root, fallbackResumeRoot string) string

		wantMoved bool
	}{
		{
			name: "a directory that is still there is used as it stands",
			cwd: func(t *testing.T, root string) string {
				dir := filepath.Join(root, "live")
				require.NoError(t, os.MkdirAll(dir, 0o755))

				return dir
			},
			wantDir: func(root, _ string) string { return filepath.Join(root, "live") },
		},
		{
			name: "a deleted worktree is recreated so the conversation stays reachable",
			cwd: func(_ *testing.T, root string) string {
				return filepath.Join(root, "repo", ".worktrees", "gone")
			},
			wantDir: func(root, _ string) string {
				return filepath.Join(root, "repo", ".worktrees", "gone")
			},
		},
		{
			name: "a path that cannot be remade sends the agent and its log to a stand-in",
			cwd: func(t *testing.T, root string) string {
				blocked := filepath.Join(root, "blocked")
				require.NoError(t, os.WriteFile(blocked, []byte("not a directory"), 0o600))

				return filepath.Join(blocked, "worktree")
			},
			wantDir:   func(_, fallbackResumeRoot string) string { return filepath.Join(fallbackResumeRoot, "agent-id") },
			wantMoved: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			fallbackResumeRoot := filepath.Join(t.TempDir(), "restored")

			cwd := tt.cwd(t, root)

			h := &fakeHarness{}
			service := &AgentService{fallbackResumeRoot: fallbackResumeRoot}
			got := service.getResumeDir(t.Context(), agentstore.Agent{ID: "agent-id", Cwd: cwd, SessionID: sessionID}, h)

			assert.Equal(t, tt.wantDir(root, fallbackResumeRoot), got)
			require.NoError(t, requireUsableDir(got), "a restore always gets a directory it can start in")

			if !tt.wantMoved {
				assert.Empty(t, h.relocated, "a directory the agent can be started in leaves the conversation alone")

				return
			}

			assert.Equal(t, [][2]string{{sessionID, got}}, h.relocated,
				"the conversation is carried into the directory the agent resumes in")
		})
	}
}

// TestAnInterruptedTurnIsAskedForAgain covers an agent stopped part-way through a turn.
// Nothing about that turn survives the pane and no CLI resumes into one, so the work only
// carries on if the resume asks for it as an ordinary message.
func TestAnInterruptedTurnIsAskedForAgain(t *testing.T) {
	const sessionID = "0f0b6f2c-6f8f-4a0e-9c2f-2a8f9d0a1b22"

	cut := func(t *testing.T, state, session string) (*AgentService, *testStore, *fakeHarness, agentstore.Agent) {
		t.Helper()

		st := newStore(t)
		tmux := newFakeTmux()
		agent := seed(t, st, "cut", "%1", state)
		if session != "" {
			require.NoError(t, st.Agents.SetSessionID(t.Context(), agent.ID, session))
			agent.SessionID = session
		}
		service, h := newManagerWithHarness(tmux, st, "")

		require.NoError(t, NewReconciler(tmux, fakeHarnesses{harness: h}, st.Agents).Reconcile(t.Context()))

		return service, st, h, agent
	}

	t.Run("a pane that goes while its agent is thinking leaves the turn recorded as cut off", func(t *testing.T) {
		_, st, _, agent := cut(t, agentstore.StateThinking, sessionID)
		assert.NotNil(t, agentByID(t, st, agent.ID).InterruptedAt)
	})

	t.Run("a pane that goes while its agent is idle leaves nothing to pick back up", func(t *testing.T) {
		_, st, _, agent := cut(t, agentstore.StateIdle, sessionID)
		assert.Nil(t, agentByID(t, st, agent.ID).InterruptedAt)
	})

	t.Run("the resume opens with the ask, and does not carry it into a later one", func(t *testing.T) {
		service, st, h, agent := cut(t, agentstore.StateThinking, sessionID)

		_, err := service.ResumeAgent(t.Context(), agent.ID)
		require.NoError(t, err)
		require.Len(t, h.launches, 1)
		assert.Equal(t, interruptedPrompt, h.launches[0].Prompt)
		assert.Nil(t, agentByID(t, st, agent.ID).InterruptedAt, "the ask is made once, not on every resume after it")
	})

	t.Run("a pane that goes while its agent waits on a person leaves the turn recorded as cut off", func(t *testing.T) {
		_, st, _, agent := cut(t, agentstore.StateWaiting, sessionID)
		assert.NotNil(t, agentByID(t, st, agent.ID).InterruptedAt,
			"an unanswered tool call is no more finished than a turn still being thought about")
	})

	// What `mise run restart` does: the tmux server goes whole and weave's own server goes
	// with it, so no reconcile pass ever watches the panes go and the store is left holding
	// a turn that is already over.
	unwatched := func(t *testing.T, state string) (*AgentService, *testStore, *fakeHarness, agentstore.Agent) {
		t.Helper()

		st := newStore(t)
		agent := seed(t, st, "cut", "%1", state)
		require.NoError(t, st.Agents.SetSessionID(t.Context(), agent.ID, sessionID))
		service, h := newManagerWithHarness(newFakeTmux(), st, "")
		require.Nil(t, agentByID(t, st, agent.ID).InterruptedAt, "no pass looked, so nothing recorded the turn")

		return service, st, h, agent
	}

	t.Run("a turn the store still has running is asked for again even if nothing recorded it as cut off", func(t *testing.T) {
		service, _, h, agent := unwatched(t, agentstore.StateThinking)

		_, err := service.ResumeAgent(t.Context(), agent.ID)
		require.NoError(t, err)
		require.Len(t, h.launches, 1)
		assert.Equal(t, interruptedPrompt, h.launches[0].Prompt)
	})

	t.Run("a turn left waiting on a person is asked for again even if nothing recorded it as cut off", func(t *testing.T) {
		service, _, h, agent := unwatched(t, agentstore.StateWaiting)

		_, err := service.ResumeAgent(t.Context(), agent.ID)
		require.NoError(t, err)
		require.Len(t, h.launches, 1)
		assert.Equal(t, interruptedPrompt, h.launches[0].Prompt)
	})

	t.Run("a relaunch that fails leaves the turn for the resume after it", func(t *testing.T) {
		service, st, h, agent := unwatched(t, agentstore.StateThinking)
		h.commandErr = errors.New("no")

		_, err := service.ResumeAgent(t.Context(), agent.ID)
		require.Error(t, err)
		assert.NotNil(t, agentByID(t, st, agent.ID).InterruptedAt,
			"the state it was read from is gone, so the turn only survives as the stamp")
	})

	t.Run("an agent that finished its turn resumes without being told anything", func(t *testing.T) {
		service, _, h, agent := cut(t, agentstore.StateIdle, sessionID)

		_, err := service.ResumeAgent(t.Context(), agent.ID)
		require.NoError(t, err)
		require.Len(t, h.launches, 1)
		assert.Empty(t, h.launches[0].Prompt)
	})

	t.Run("an agent with no conversation to reopen is not asked about a turn it cannot read", func(t *testing.T) {
		service, st, h, agent := cut(t, agentstore.StateThinking, "")

		_, err := service.ResumeAgent(t.Context(), agent.ID)
		require.NoError(t, err)
		require.Len(t, h.launches, 1)
		assert.Empty(t, h.launches[0].Prompt, "a fresh conversation cannot be asked to read back over itself")
		assert.False(t, h.launches[0].Resume, "there is no conversation to reopen")
		assert.Nil(t, agentByID(t, st, agent.ID).InterruptedAt,
			"the conversation that held the turn is gone, so nothing is left to ask about it")
	})

	t.Run("a turn cut off before its conversation was named is not asked about after a fresh one is", func(t *testing.T) {
		service, st, h, agent := cut(t, agentstore.StateThinking, "")

		_, err := service.ResumeAgent(t.Context(), agent.ID)
		require.NoError(t, err)
		require.Len(t, h.launches, 1)
		require.Empty(t, h.launches[0].Prompt)

		// What a Codex agent does on the way back in: the fresh conversation it started is
		// matched to its rollout file, and the panel adopts that id as the agent's own.
		const learned = "9a1c7f10-2b3d-4e5f-8a9b-0c1d2e3f4a5b"
		require.NoError(t, st.Agents.SetSessionID(t.Context(), agent.ID, learned))
		require.NoError(t, st.Agents.SetStateByID(t.Context(), agent.ID, agentstore.StateIdle))

		tmux := newFakeTmux()
		require.NoError(t, NewReconciler(tmux, fakeHarnesses{harness: h}, st.Agents).Reconcile(t.Context()))

		_, err = service.ResumeAgent(t.Context(), agent.ID)
		require.NoError(t, err)
		require.Len(t, h.launches, 2)
		assert.True(t, h.launches[1].Resume, "the conversation it learned is what this reopens")
		assert.Empty(t, h.launches[1].Prompt, "the conversation this reopens ran its turn to the end")
	})
}

// TestATurnCutOffWithNoTmuxServerIsAskedForAgain covers the restart path, where the tmux
// server is killed before any agent is put back. Every reconcile pass fails on the first
// thing it does, so nothing looks at a pane and nothing records the turn that was cut off.
// The resume holds the same two facts a reconcile pass would have gone on, the pane being
// gone and the row still saying thinking, so it makes the ask itself.
func TestATurnCutOffWithNoTmuxServerIsAskedForAgain(t *testing.T) {
	const sessionID = "1c7a3e8b-4d5f-4a6b-9c8d-7e6f5a4b3c21"

	st := newStore(t)
	tmux := newFakeTmux()
	agent := seed(t, st, "cut", "%1", agentstore.StateThinking)
	require.NoError(t, st.Agents.SetSessionID(t.Context(), agent.ID, sessionID))

	service, h := newManagerWithHarness(tmux, st, "")

	tmux.paneOwnersErr = errors.New("error connecting to /private/tmp/tmux-501/weave")
	rec := NewReconciler(tmux, fakeHarnesses{harness: h}, st.Agents)
	require.Error(t, rec.Reconcile(t.Context()))
	tmux.paneOwnersErr = nil

	stored := agentByID(t, st, agent.ID)
	require.Nil(t, stored.InterruptedAt, "no pass could look at a pane, so nothing recorded the turn")
	require.Equal(t, agentstore.StateThinking, stored.State)

	_, err := service.ResumeAgent(t.Context(), agent.ID)
	require.NoError(t, err)
	require.Len(t, h.launches, 1)
	assert.Equal(t, interruptedPrompt, h.launches[0].Prompt)

	stored = agentByID(t, st, agent.ID)
	require.NoError(t, tmux.ClearPaneOwner(stored.Pane))

	_, err = service.ResumeAgent(t.Context(), agent.ID)
	require.NoError(t, err)
	require.Len(t, h.launches, 2)
	assert.Empty(t, h.launches[1].Prompt, "the ask is made once, and the row it was read from now says idle")
}

// blockingLifecycle holds a resume inside its setup action until the test lets it go, which
// is the window a background pass has to land in to reach a row a resume is already working
// on.
type blockingLifecycle struct {
	fakeLifecycle

	entered chan struct{}
	release chan struct{}
}

func (b *blockingLifecycle) RunResumeAction(context.Context, agentstore.Agent, string) (string, error) {
	close(b.entered)
	<-b.release

	return "", nil
}

// TestAResumeInItsSetupIsNotArchivedOutFromUnderItself covers the whole of a resume before its
// first write to the row. A worktree setup action is seconds of work, and for all of it the row
// used to read as an untouched dormant agent, which is exactly what the pull request pass
// archives. Its compare-and-set did not help, since nothing had changed for it to notice.
func TestAResumeInItsSetupIsNotArchivedOutFromUnderItself(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	ctx := t.Context()

	created, err := st.Workflows.Create(ctx, workflowstore.Config{
		Name:        "worktrees",
		Cwd:         t.TempDir(),
		SetupAction: "worktree-setup",
		Worktree:    workflowstore.WorktreeConfig{Enabled: true, ArchiveOnPRClosed: true},
	})
	require.NoError(t, err)

	agent := seedPRAgent(t, st, created.ID, agentstore.StateIdle, "", prURL)

	lifecycle := &blockingLifecycle{entered: make(chan struct{}), release: make(chan struct{})}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)

	done := make(chan error, 1)
	go func() {
		_, resumeErr := manager.ResumeAgent(context.WithoutCancel(ctx), agent.ID)
		done <- resumeErr
	}()

	<-lifecycle.entered

	assert.Equal(t, agentstore.StateSettingUp, agentByID(t, st, agent.ID).State,
		"the row says a resume is in flight before the resume does any work")

	prs := &fakePRStates{state: prStateMerged, branch: prBranch, repo: prRepo, origin: prRepo}
	require.NoError(t, newPRArchiver(st, prs, manager).Sync(ctx))

	assert.Nil(t, agentByID(t, st, agent.ID).ArchivedAt, "a pass landing in the window leaves the resume alone")

	close(lifecycle.release)
	select {
	case resumeErr := <-done:
		require.NoError(t, resumeErr)
	case <-time.After(10 * time.Second):
		t.Fatal("the resume never finished")
	}

	after := agentByID(t, st, agent.ID)
	require.Nil(t, after.ArchivedAt)
	require.NotEmpty(t, after.Pane)
	assert.Equal(t, agentstore.StateIdle, after.State)

	live, err := st.List(ctx)
	require.NoError(t, err)
	assert.Len(t, live, 1, "the agent whose CLI is running is one a listing shows")
}

// TestAPassArchivesAResumeThatFinishedFirst covers the other side of the same window. A resume
// that got all the way through before the pass ran is an agent sitting idle in a pane, and a
// merged pull request is reason enough to archive it, pane and all.
func TestAPassArchivesAResumeThatFinishedFirst(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	ctx := t.Context()

	created, err := st.Workflows.Create(ctx, workflowstore.Config{
		Name:     "worktrees",
		Cwd:      t.TempDir(),
		Worktree: workflowstore.WorktreeConfig{Enabled: true, ArchiveOnPRClosed: true},
	})
	require.NoError(t, err)

	agent := seedPRAgent(t, st, created.ID, agentstore.StateIdle, "", prURL)

	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, &fakeLifecycle{})
	resumed, err := manager.ResumeAgent(ctx, agent.ID)
	require.NoError(t, err)
	require.NotEmpty(t, resumed.Pane)

	prs := &fakePRStates{state: prStateMerged, branch: prBranch, repo: prRepo, origin: prRepo}
	require.NoError(t, newPRArchiver(st, prs, manager).Sync(ctx))

	assert.NotNil(t, agentByID(t, st, agent.ID).ArchivedAt)
	assert.Equal(t, []string{resumed.Pane}, tmux.killedPanes, "the pane goes with the archive it belongs to")
}

// TestAResumeThatGivesUpPutsTheStateBack covers a resume that never reaches its first write to
// the row. Nothing else in weave moves a row out of setting_up, so a row left there is one no
// later resume and no pass will touch again.
func TestAResumeThatGivesUpPutsTheStateBack(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	agent := seedInWorkflow(t, st, workflowID, "cut", "", agentstore.StateThinking)

	lifecycle := &fakeLifecycle{resumeErr: errors.New("git worktree add: already checked out")}
	require.NoError(t, st.Workflows.Update(ctx, workflowID, workflowstore.Config{Name: "workflow", SetupAction: "worktree-setup"}))

	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)

	_, err := manager.ResumeAgent(ctx, agent.ID)
	require.Error(t, err)

	assert.Equal(t, agentstore.StateThinking, agentByID(t, st, agent.ID).State,
		"a resume that gave up leaves the row reading as it found it")
}

// archivingTmux runs one pull request archiving pass the first time a resume asks whether the
// agent is already running, which is the only call between the caller reading the row and the
// resume marking it.
type archivingTmux struct {
	*fakeTmux

	sync func()
	once sync.Once
}

func (a *archivingTmux) GetPaneOwner(pane string) string {
	a.once.Do(a.sync)

	return a.fakeTmux.GetPaneOwner(pane)
}

// TestAPassThatArchivesBeforeTheMarkStopsTheResume covers the ordering the mark cannot skip
// ahead of: a pass that gets the whole way through its archive between the caller reading the
// row and the resume writing to it. The reading is fresh, so nothing in the pass refuses. The
// mark is where the resume finds out, which is why it refuses an archived row rather than
// going on to open a pane no listing would show.
func TestAPassThatArchivesBeforeTheMarkStopsTheResume(t *testing.T) {
	st := newStore(t)
	base := newFakeTmux()
	ctx := t.Context()

	created, err := st.Workflows.Create(ctx, workflowstore.Config{
		Name:     "worktrees",
		Cwd:      t.TempDir(),
		Worktree: workflowstore.WorktreeConfig{Enabled: true, ArchiveOnPRClosed: true},
	})
	require.NoError(t, err)

	agent := seedPRAgent(t, st, created.ID, agentstore.StateIdle, "", prURL)

	tmux := &archivingTmux{fakeTmux: base}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, &fakeLifecycle{})
	tmux.sync = func() {
		prs := &fakePRStates{state: prStateMerged, branch: prBranch, repo: prRepo, origin: prRepo}
		require.NoError(t, newPRArchiver(st, prs, manager).Sync(ctx))
	}

	_, err = manager.ResumeAgent(ctx, agent.ID)
	require.ErrorIs(t, err, ErrAgentArchived)

	after := agentByID(t, st, agent.ID)
	require.NotNil(t, after.ArchivedAt, "the pass really did archive it")
	assert.Empty(t, after.Pane, "and the resume wrote no pane onto the archived row")
	assert.Empty(t, base.newWindows, "the resume gave up before it opened a window")

	live, err := st.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, live, "nothing is left running that no listing shows")
}

// TestAnArchiveAfterTheMarkStopsTheResume covers the window on the far side of the mark. From
// there to the resume's write is its setup action and a tmux window, seconds of real work for a
// worktree workflow, and an archive landing in it is a person hitting a key rather than a pass
// with a compare-and-set. It kills the pane the row names, which is none yet, so the resume is
// the only side left that can take away the pane it went on to open.
func TestAnArchiveAfterTheMarkStopsTheResume(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	ctx := t.Context()

	created, err := st.Workflows.Create(ctx, workflowstore.Config{
		Name:        "worktrees",
		Cwd:         t.TempDir(),
		SetupAction: "worktree-setup",
		Worktree:    workflowstore.WorktreeConfig{Enabled: true},
	})
	require.NoError(t, err)

	agent := seedInWorkflow(t, st, created.ID, "cut", "", agentstore.StateIdle)

	h := &fakeHarness{}
	lifecycle := &blockingLifecycle{entered: make(chan struct{}), release: make(chan struct{})}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: h}, lifecycle)

	done := make(chan error, 1)
	go func() {
		_, resumeErr := manager.ResumeAgent(context.WithoutCancel(ctx), agent.ID)
		done <- resumeErr
	}()

	<-lifecycle.entered

	require.Equal(t, agentstore.StateSettingUp, agentByID(t, st, agent.ID).State)
	require.NoError(t, manager.ArchiveAgents(ctx, []string{agent.ID}))

	close(lifecycle.release)
	select {
	case resumeErr := <-done:
		require.ErrorIs(t, resumeErr, ErrAgentArchived)
	case <-time.After(10 * time.Second):
		t.Fatal("the resume never finished")
	}

	after := agentByID(t, st, agent.ID)
	require.NotNil(t, after.ArchivedAt, "the archive really did land")
	assert.Empty(t, after.Pane, "the resume wrote no pane onto the archived row")
	assert.Equal(t, agentstore.StateIdle, after.State, "and the row reads as the resume found it rather than mid-setup")
	assert.Len(t, tmux.killedPanes, 1, "the pane the resume opened goes with the resume that lost")
	assert.Empty(t, h.launches, "and no CLI was launched into a pane no listing shows")

	live, err := st.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, live, "nothing is left running that no listing shows")
}

// TestADeleteAfterTheMarkStopsTheResume is the same window reached by the worse of the two
// ways out of it. A delete drops the row, so there is nothing left for the resume to write a
// pane onto and nothing anybody could later archive: a CLI launched here would be attached to
// an id no row holds.
func TestADeleteAfterTheMarkStopsTheResume(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	ctx := t.Context()

	created, err := st.Workflows.Create(ctx, workflowstore.Config{
		Name:        "worktrees",
		Cwd:         t.TempDir(),
		SetupAction: "worktree-setup",
		Worktree:    workflowstore.WorktreeConfig{Enabled: true},
	})
	require.NoError(t, err)

	agent := seedInWorkflow(t, st, created.ID, "cut", "", agentstore.StateIdle)

	h := &fakeHarness{}
	lifecycle := &blockingLifecycle{entered: make(chan struct{}), release: make(chan struct{})}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: h}, lifecycle)

	done := make(chan error, 1)
	go func() {
		_, resumeErr := manager.ResumeAgent(context.WithoutCancel(ctx), agent.ID)
		done <- resumeErr
	}()

	<-lifecycle.entered

	require.Equal(t, agentstore.StateSettingUp, agentByID(t, st, agent.ID).State)
	require.NoError(t, manager.DeleteAgents(ctx, []string{agent.ID}))

	close(lifecycle.release)
	select {
	case resumeErr := <-done:
		require.ErrorIs(t, resumeErr, ErrAgentArchived)
	case <-time.After(10 * time.Second):
		t.Fatal("the resume never finished")
	}

	assert.Len(t, tmux.killedPanes, 1, "the pane the resume opened goes with the resume that lost")
	assert.Empty(t, h.launches, "no CLI was launched for an agent no row holds")

	rows, err := st.ListAll(ctx)
	require.NoError(t, err)
	assert.Empty(t, rows, "the delete really did take the row")
}

// refusingUpdate is the store with its last write to the row taken away, which is the one step
// a resume can fail at after it has started overwriting the copy of the agent it was handed.
type refusingUpdate struct {
	*agentstore.Store
}

func (refusingUpdate) UpdateIfLive(context.Context, agentstore.Agent) (bool, error) {
	return false, errors.New("the row would not take the write")
}

// TestAResumeThatFailsOnItsLastWritePutsBackWhatItFound covers the state a resume restores
// when it gives up at the only step that runs after it has begun rewriting the agent in
// memory. What goes back is the state on the row when the resume started, not the idle the
// resume was about to write.
func TestAResumeThatFailsOnItsLastWritePutsBackWhatItFound(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	agent := seedInWorkflow(t, st, workflowID, "cut", "", agentstore.StateThinking)

	manager := st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: fakeHarnesses{harness: &fakeHarness{}},
		addr:      "127.0.0.1:9999",
		lifecycle: &fakeLifecycle{},
		agents:    refusingUpdate{Store: st.Agents},
	})

	_, err := manager.ResumeAgent(ctx, agent.ID)
	require.Error(t, err)

	after := agentByID(t, st, agent.ID)
	assert.Equal(t, agentstore.StateThinking, after.State, "the row reads as the resume found it")
	assert.Empty(t, after.Pane)
	assert.Len(t, tmux.killedPanes, 1, "the window it opened goes with it")
}
