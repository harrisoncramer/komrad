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

// TestResumeDir pins that a restore produces a usable directory whatever became of the
// recorded one, since a pane can only be opened somewhere that exists.
func TestResumeDir(t *testing.T) {
	const sessionID = "6f1b0c34-0b6b-4a17-9d1f-1f4b6c0e9a11"

	tests := []struct {
		name    string
		cwd     func(t *testing.T, root string) string
		wantDir func(root, fallbackResumeRoot string) string
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
			name: "a path that cannot be remade sends the agent to a stand-in",
			cwd: func(t *testing.T, root string) string {
				blocked := filepath.Join(root, "blocked")
				require.NoError(t, os.WriteFile(blocked, []byte("not a directory"), 0o600))

				return filepath.Join(blocked, "worktree")
			},
			wantDir: func(_, fallbackResumeRoot string) string { return filepath.Join(fallbackResumeRoot, "agent-id") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			fallbackResumeRoot := filepath.Join(t.TempDir(), "restored")

			cwd := tt.cwd(t, root)

			service := NewResumeService(ResumeServiceParams{FallbackResumeRoot: fallbackResumeRoot})
			got := service.getResumeDir(agentstore.Agent{
				ID:        "agent-id",
				Cwd:       cwd,
				SessionID: sessionID,
			})

			assert.Equal(t, tt.wantDir(root, fallbackResumeRoot), got)
			require.NoError(t, requireUsableDir(got), "a restore always gets a directory it can start in")
		})
	}
}

// TestAnInterruptedTurnIsAskedForAgain pins the resume asking again as an ordinary message,
// since nothing about a cut-off turn survives the pane.
func TestAnInterruptedTurnIsAskedForAgain(t *testing.T) {
	const sessionID = "0f0b6f2c-6f8f-4a0e-9c2f-2a8f9d0a1b22"

	cut := func(t *testing.T, state agentstore.State, session string) (*testAgents, *testStore, *fakeHarness, agentstore.Agent) {
		t.Helper()

		st := newStore(t)
		tmux := newFakeTmux()
		agent := seed(t, st, "cut", "%1", state)
		if session != "" {
			require.NoError(t, st.Agents.SetSessionID(t.Context(), agent.ID, session))
			agent.SessionID = session
		}
		service, h := newManagerWithHarness(tmux, st, "")

		rec := NewReconciler(tmux, fakeHarnesses{harness: h}, st.Agents)
		for range listingsBeforePaneGone {
			require.NoError(t, rec.Reconcile(t.Context()))
		}

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

		_, err := service.resume.ResumeAgent(t.Context(), agent.ID)
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
	unwatched := func(t *testing.T, state agentstore.State) (*testAgents, *testStore, *fakeHarness, agentstore.Agent) {
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

		_, err := service.resume.ResumeAgent(t.Context(), agent.ID)
		require.NoError(t, err)
		require.Len(t, h.launches, 1)
		assert.Equal(t, interruptedPrompt, h.launches[0].Prompt)
	})

	t.Run("a turn left waiting on a person is asked for again even if nothing recorded it as cut off", func(t *testing.T) {
		service, _, h, agent := unwatched(t, agentstore.StateWaiting)

		_, err := service.resume.ResumeAgent(t.Context(), agent.ID)
		require.NoError(t, err)
		require.Len(t, h.launches, 1)
		assert.Equal(t, interruptedPrompt, h.launches[0].Prompt)
	})

	t.Run("a relaunch that fails leaves the turn for the resume after it", func(t *testing.T) {
		service, st, h, agent := unwatched(t, agentstore.StateThinking)
		h.commandErr = errors.New("no")

		_, err := service.resume.ResumeAgent(t.Context(), agent.ID)
		require.Error(t, err)
		assert.NotNil(t, agentByID(t, st, agent.ID).InterruptedAt,
			"the state it was read from is gone, so the turn only survives as the stamp")
	})

	t.Run("an agent that finished its turn resumes without being told anything", func(t *testing.T) {
		service, _, h, agent := cut(t, agentstore.StateIdle, sessionID)

		_, err := service.resume.ResumeAgent(t.Context(), agent.ID)
		require.NoError(t, err)
		require.Len(t, h.launches, 1)
		assert.Empty(t, h.launches[0].Prompt)
	})

	t.Run("an agent with no conversation to reopen is not asked about a turn it cannot read", func(t *testing.T) {
		service, st, h, agent := cut(t, agentstore.StateThinking, "")

		_, err := service.resume.ResumeAgent(t.Context(), agent.ID)
		require.NoError(t, err)
		require.Len(t, h.launches, 1)
		assert.Empty(t, h.launches[0].Prompt, "a fresh conversation cannot be asked to read back over itself")
		assert.False(t, h.launches[0].Resume, "there is no conversation to reopen")
		assert.Nil(t, agentByID(t, st, agent.ID).InterruptedAt,
			"the conversation that held the turn is gone, so nothing is left to ask about it")
	})

	t.Run("a turn cut off before its conversation was named is not asked about after a fresh one is", func(t *testing.T) {
		service, st, h, agent := cut(t, agentstore.StateThinking, "")

		_, err := service.resume.ResumeAgent(t.Context(), agent.ID)
		require.NoError(t, err)
		require.Len(t, h.launches, 1)
		require.Empty(t, h.launches[0].Prompt)

		// What a Codex agent does on the way back in: the fresh conversation it started is
		// matched to its rollout file, and the panel adopts that id as the agent's own.
		const learned = "9a1c7f10-2b3d-4e5f-8a9b-0c1d2e3f4a5b"
		require.NoError(t, st.Agents.SetSessionID(t.Context(), agent.ID, learned))
		require.NoError(t, st.Agents.SetStateByID(t.Context(), agent.ID, agentstore.StateIdle))

		tmux := newFakeTmux()
		rec := NewReconciler(tmux, fakeHarnesses{harness: h}, st.Agents)
		for range listingsBeforePaneGone {
			require.NoError(t, rec.Reconcile(t.Context()))
		}

		_, err = service.resume.ResumeAgent(t.Context(), agent.ID)
		require.NoError(t, err)
		require.Len(t, h.launches, 2)
		assert.True(t, h.launches[1].Resume, "the conversation it learned is what this reopens")
		assert.Empty(t, h.launches[1].Prompt, "the conversation this reopens ran its turn to the end")
	})
}

// TestATurnCutOffWithNoTmuxServerIsAskedForAgain pins the resume making the ask itself when
// no pass ever ran, since the tmux server was killed before any agent was put back.
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

	_, err := service.resume.ResumeAgent(t.Context(), agent.ID)
	require.NoError(t, err)
	require.Len(t, h.launches, 1)
	assert.Equal(t, interruptedPrompt, h.launches[0].Prompt)

	stored = agentByID(t, st, agent.ID)
	require.NoError(t, tmux.ClearPaneOwner(stored.Pane))

	_, err = service.resume.ResumeAgent(t.Context(), agent.ID)
	require.NoError(t, err)
	require.Len(t, h.launches, 2)
	assert.Empty(t, h.launches[1].Prompt, "the ask is made once, and the row it was read from now says idle")
}

// blockingLifecycle holds a resume inside its provisioner until the test lets it go, which
// is the window a background pass has to land in to reach a row a resume is already working
// on.
type blockingLifecycle struct {
	fakeLifecycle
	entered chan struct{}
	release chan struct{}
}

func (b *blockingLifecycle) RunResume(context.Context, agentstore.Agent, string) (string, error) {
	close(b.entered)
	<-b.release

	return "", nil
}

// TestAResumeInItsSetupIsNotArchivedOutFromUnderItself pins that a resume still in its setup
// is not archived, which its row used to read as an untouched dormant agent for seconds.
func TestAResumeInItsSetupIsNotArchivedOutFromUnderItself(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	ctx := t.Context()

	created, err := st.Workflows.Create(ctx, workflowstore.Config{
		Name:              "worktrees",
		Cwd:               initRepo(t),
		Worktree:          workflowstore.WorktreeConfig{Enabled: true},
		ArchiveOnPRClosed: true,
	})
	require.NoError(t, err)

	agent := seedPRAgent(t, st, created.ID, agentstore.StateIdle, "", prURL)

	lifecycle := &blockingLifecycle{entered: make(chan struct{}), release: make(chan struct{})}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)

	done := make(chan error, 1)
	go func() {
		_, resumeErr := manager.resume.ResumeAgent(context.WithoutCancel(ctx), agent.ID)
		done <- resumeErr
	}()

	<-lifecycle.entered

	assert.Equal(t, agentstore.StateSettingUp, agentByID(t, st, agent.ID).State,
		"the row says a resume is in flight before the resume does any work")

	prs := &fakePRStates{
		state:  prStateMerged,
		branch: prBranch,
		repo:   prRepo,
		origin: prRepo,
	}
	require.NoError(t, newPRArchiver(st, prs, manager.lifecycle).Sync(ctx))

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

// TestAPassArchivesAResumeThatFinishedFirst covers the other side of that window, where a
// resume finished first and a merged pull request is reason enough to archive it.
func TestAPassArchivesAResumeThatFinishedFirst(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	ctx := t.Context()

	created, err := st.Workflows.Create(ctx, workflowstore.Config{
		Name:              "worktrees",
		Cwd:               t.TempDir(),
		Worktree:          workflowstore.WorktreeConfig{Enabled: true},
		ArchiveOnPRClosed: true,
	})
	require.NoError(t, err)

	agent := seedPRAgent(t, st, created.ID, agentstore.StateIdle, "", prURL)

	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, &fakeLifecycle{})
	resumed, err := manager.resume.ResumeAgent(ctx, agent.ID)
	require.NoError(t, err)
	require.NotEmpty(t, resumed.Pane)

	prs := &fakePRStates{
		state:  prStateMerged,
		branch: prBranch,
		repo:   prRepo,
		origin: prRepo,
	}
	require.NoError(t, newPRArchiver(st, prs, manager.lifecycle).Sync(ctx))

	assert.NotNil(t, agentByID(t, st, agent.ID).ArchivedAt)
	assert.Equal(t, []string{resumed.Pane}, tmux.killedPanes, "the pane goes with the archive it belongs to")
}

// TestAResumeThatGivesUpPutsTheStateBack pins the state going back, since nothing else in
// weave moves a row out of setting_up.
func TestAResumeThatGivesUpPutsTheStateBack(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	agent := seedInWorkflow(t, st, workflowID, "cut", "", agentstore.StateThinking)

	lifecycle := &fakeLifecycle{resumeErr: errors.New("git worktree add: already checked out")}
	require.NoError(t, st.Workflows.Update(ctx, workflowID, workflowstore.Config{
		Name:     "workflow",
		Cwd:      initRepo(t),
		Worktree: workflowstore.WorktreeConfig{Enabled: true},
	}))

	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)

	_, err := manager.resume.ResumeAgent(ctx, agent.ID)
	require.Error(t, err)

	assert.Equal(t, agentstore.StateThinking, agentByID(t, st, agent.ID).State,
		"a resume that gave up leaves the row reading as it found it")
}

// TestASecondResumeOpensNoSecondWindow pins the claim inside the resume, since AddReviewMessage
// wakes a dormant reviewer and a harness that batches its tool calls answers several threads at
// once, which is several resumes of one agent in flight together.
func TestASecondResumeOpensNoSecondWindow(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	agent := seedInWorkflow(t, st, workflowID, "dormant", "", agentstore.StateIdle)

	// The second resume arrives while the first is part-way through, which is the window both of
	// them would otherwise go on to open a window in.
	arrived := make(chan struct{})
	release := make(chan struct{})
	tmux.beforeNewWindow = func() {
		select {
		case <-arrived:
		default:
			close(arrived)
			<-release
		}
	}

	manager := newManager(tmux, st, "")

	first := make(chan error, 1)
	go func() {
		_, err := manager.resume.ResumeAgent(context.WithoutCancel(ctx), agent.ID)
		first <- err
	}()

	<-arrived

	_, err := manager.resume.ResumeAgent(ctx, agent.ID)
	require.ErrorIs(t, err, ErrResumeInFlight)

	close(release)
	require.NoError(t, <-first)

	assert.Equal(t, 1, tmux.windowsOpened(), "the second resume opened no window of its own")
}

// TestAResumeIsClaimableAgainAfterOneFinishes pins that the claim is held for the length of a
// resume rather than for the life of the agent, since a reviewer woken once still has to come
// back the next time its pane goes.
func TestAResumeIsClaimableAgainAfterOneFinishes(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	agent := seedInWorkflow(t, st, workflowID, "dormant", "", agentstore.StateIdle)

	manager := newManager(tmux, st, "")

	_, err := manager.resume.ResumeAgent(ctx, agent.ID)
	require.NoError(t, err)

	require.NoError(t, tmux.ClearPaneOwner(agentByID(t, st, agent.ID).Pane))

	_, err = manager.resume.ResumeAgent(ctx, agent.ID)
	require.NoError(t, err, "the claim goes with the resume that took it")
	assert.Equal(t, 2, tmux.windowsOpened())
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

// TestAPassThatArchivesBeforeTheMarkStopsTheResume pins the mark refusing an archived row,
// which is where a resume finds out about an archive that landed while it was reading.
func TestAPassThatArchivesBeforeTheMarkStopsTheResume(t *testing.T) {
	st := newStore(t)
	base := newFakeTmux()
	ctx := t.Context()

	created, err := st.Workflows.Create(ctx, workflowstore.Config{
		Name:              "worktrees",
		Cwd:               t.TempDir(),
		Worktree:          workflowstore.WorktreeConfig{Enabled: true},
		ArchiveOnPRClosed: true,
	})
	require.NoError(t, err)

	agent := seedPRAgent(t, st, created.ID, agentstore.StateIdle, "", prURL)

	tmux := &archivingTmux{fakeTmux: base}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, &fakeLifecycle{})
	tmux.sync = func() {
		prs := &fakePRStates{
			state:  prStateMerged,
			branch: prBranch,
			repo:   prRepo,
			origin: prRepo,
		}
		require.NoError(t, newPRArchiver(st, prs, manager.lifecycle).Sync(ctx))
	}

	_, err = manager.resume.ResumeAgent(ctx, agent.ID)
	require.ErrorIs(t, err, ErrAgentArchived)

	after := agentByID(t, st, agent.ID)
	require.NotNil(t, after.ArchivedAt, "the pass really did archive it")
	assert.Empty(t, after.Pane, "and the resume wrote no pane onto the archived row")
	assert.Empty(t, base.newWindows, "the resume gave up before it opened a window")

	live, err := st.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, live, "nothing is left running that no listing shows")
}

// TestAnArchiveAfterTheMarkStopsTheResume covers the window past the mark, where the archive
// kills a pane the row does not name yet and only the resume can take away the one it opened.
func TestAnArchiveAfterTheMarkStopsTheResume(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	ctx := t.Context()

	created, err := st.Workflows.Create(ctx, workflowstore.Config{
		Name:     "worktrees",
		Cwd:      initRepo(t),
		Worktree: workflowstore.WorktreeConfig{Enabled: true},
	})
	require.NoError(t, err)

	agent := seedInWorkflow(t, st, created.ID, "cut", "", agentstore.StateIdle)

	h := &fakeHarness{}
	lifecycle := &blockingLifecycle{entered: make(chan struct{}), release: make(chan struct{})}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: h}, lifecycle)

	done := make(chan error, 1)
	go func() {
		_, resumeErr := manager.resume.ResumeAgent(context.WithoutCancel(ctx), agent.ID)
		done <- resumeErr
	}()

	<-lifecycle.entered

	require.Equal(t, agentstore.StateSettingUp, agentByID(t, st, agent.ID).State)
	require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, []string{agent.ID}))

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

// TestADeleteAfterTheMarkStopsTheResume covers the same window ended by a delete, which drops
// the row and would leave a launched CLI attached to an id nothing holds.
func TestADeleteAfterTheMarkStopsTheResume(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	ctx := t.Context()

	created, err := st.Workflows.Create(ctx, workflowstore.Config{
		Name:     "worktrees",
		Cwd:      initRepo(t),
		Worktree: workflowstore.WorktreeConfig{Enabled: true},
	})
	require.NoError(t, err)

	agent := seedInWorkflow(t, st, created.ID, "cut", "", agentstore.StateIdle)

	h := &fakeHarness{}
	lifecycle := &blockingLifecycle{entered: make(chan struct{}), release: make(chan struct{})}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: h}, lifecycle)

	done := make(chan error, 1)
	go func() {
		_, resumeErr := manager.resume.ResumeAgent(context.WithoutCancel(ctx), agent.ID)
		done <- resumeErr
	}()

	<-lifecycle.entered

	require.Equal(t, agentstore.StateSettingUp, agentByID(t, st, agent.ID).State)
	require.NoError(t, manager.lifecycle.DeleteAgents(ctx, []string{agent.ID}))

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

// TestAResumeThatFailsOnItsLastWritePutsBackWhatItFound pins that the state on the row when
// the resume started goes back, not the idle it was about to write.
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

	_, err := manager.resume.ResumeAgent(ctx, agent.ID)
	require.Error(t, err)

	after := agentByID(t, st, agent.ID)
	assert.Equal(t, agentstore.StateThinking, after.State, "the row reads as the resume found it")
	assert.Empty(t, after.Pane)
	assert.Len(t, tmux.killedPanes, 1, "the window it opened goes with it")
}

// TestResumeRunsTheProvisioner pins the provisioner firing again on resume, which is the
// moment a workspace has to exist since nothing before it rebuilds one.
func TestResumeRunsTheProvisioner(t *testing.T) {
	tests := []struct {
		name       string
		named      string
		resumeErr  error
		wantWindow bool
		wantErr    string
	}{
		{
			name:      "a script that refuses keeps the agent dormant",
			resumeErr: errors.New("fatal: 'auth' is already checked out"),
			wantErr:   "already checked out",
		},
		{
			name:    "a script naming somewhere that is not a directory is refused",
			named:   "/definitely/not/here",
			wantErr: "a provisioner named",
		},
		{
			name:       "a script naming nowhere leaves the recorded directory alone",
			wantWindow: true,
		},
		{
			name:       "a script naming a directory brings the agent back up in it",
			named:      "named",
			wantWindow: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := newFakeTmux()
			ctx := t.Context()

			recorded := t.TempDir()
			named := tt.named
			if named == "named" {
				named = t.TempDir()
			}

			lifecycle := &fakeLifecycle{resumeDir: named, resumeErr: tt.resumeErr}
			manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)

			created, err := st.Workflows.Create(ctx, workflowstore.Config{
				Name:     "resume-" + tt.name,
				Cwd:      initRepo(t),
				Worktree: workflowstore.WorktreeConfig{Enabled: true},
			})
			require.NoError(t, err)

			agent := seedInWorkflow(t, st, created.ID, "auth", "%0", agentstore.StateIdle)
			agent.Cwd = recorded
			require.NoError(t, st.Agents.Update(ctx, agent))

			resumed, err := manager.resume.ResumeAgent(ctx, agent.ID)

			assert.Equal(t, []string{worktreeProvisioner}, lifecycle.resumed, "the workflow's own script is what fired")

			if tt.wantErr != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tt.wantErr)
				assert.Empty(t, tmux.newWindows, "a refused script opens no window")
				assert.Equal(t, recorded, agentByID(t, st, agent.ID).Cwd, "and moves nobody")

				return
			}

			require.NoError(t, err)
			require.True(t, tt.wantWindow)

			want := recorded
			if named != "" {
				want = named
			}

			assert.Equal(t, want, resumed.Cwd)
			assert.Equal(t, want, tmux.newWindows["auth"], "the window opens where the script said")
		})
	}
}

// TestResumeWithoutAProvisionerFiresNothing covers the ordinary workflow, which names no
// provisioner and comes back up the way it always has.
func TestResumeWithoutAProvisionerFiresNothing(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	lifecycle := &fakeLifecycle{}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)

	agent := seed(t, st, "auth", "%0", agentstore.StateIdle)

	_, err := manager.resume.ResumeAgent(t.Context(), agent.ID)

	require.NoError(t, err)
	assert.Empty(t, lifecycle.resumed)
}

// TestResumeGivesBackThePaneItOpened pins a resume that opens a window and then refuses giving
// it up, since what it leaves behind is a bare shell with no CLI in it and a row saying the
// agent is idle in that shell.
func TestResumeGivesBackThePaneItOpened(t *testing.T) {
	tests := []struct {
		name        string
		setOwnerErr error
		sendKeysErr error
	}{
		{
			name:        "a pane tmux refuses to stamp",
			setOwnerErr: errors.New("tmux refused the option"),
		},
		{
			name:        "a relaunch that never reaches the pane's shell",
			sendKeysErr: errors.New("tmux dropped the keys"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := newFakeTmux()
			tmux.setOwnerErr = tt.setOwnerErr
			tmux.sendKeysErr = tt.sendKeysErr
			st := newStore(t)
			manager := newManager(tmux, st, "")
			ctx := t.Context()

			agent := seed(t, st, "auth", "", agentstore.StateThinking)

			_, err := manager.resume.ResumeAgent(ctx, agent.ID)

			require.Error(t, err)
			assert.Equal(t, []string{"%auth"}, tmux.killedPanes, "the window it opened is taken away")
			assert.Equal(t, agentstore.StateThinking, agentByID(t, st, agent.ID).State,
				"and the row is left reading as the resume found it")
		})
	}
}

// TestResumeAfterARefusedRelaunchGetsThrough pins the recovery, since a pane left behind is
// stamped with the agent's id and makes every resume after it report success having done nothing.
func TestResumeAfterARefusedRelaunchGetsThrough(t *testing.T) {
	tmux := newFakeTmux()
	tmux.sendKeysErr = errors.New("tmux dropped the keys")
	st := newStore(t)
	harness := &fakeHarness{}
	manager := st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: fakeHarnesses{harness: harness},
		lifecycle: &fakeLifecycle{},
	})
	ctx := t.Context()

	agent := seed(t, st, "auth", "", agentstore.StateThinking)

	_, err := manager.resume.ResumeAgent(ctx, agent.ID)
	require.Error(t, err)

	tmux.sendKeysErr = nil

	resumed, err := manager.resume.ResumeAgent(ctx, agent.ID)

	require.NoError(t, err)
	assert.Len(t, harness.launches, 2, "the second resume reaches the CLI rather than reporting a pane that is already running")
	assert.Equal(t, agentstore.StateIdle, resumed.State)
}

// TestResumeRefusesArchived pins the refusal naming the way out, since the pane would
// otherwise sit behind a row nothing lists.
func TestResumeRefusesArchived(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManager(tmux, st, "")
	ctx := t.Context()

	archived := seed(t, st, "archived", "%0", agentstore.StateIdle)
	require.NoError(t, st.SoftDelete(ctx, "archived"))

	_, err := manager.resume.ResumeAgent(ctx, archived.ID)

	require.ErrorIs(t, err, ErrAgentArchived)
	require.ErrorContains(t, err, "restore it first", "the refusal says what to do about it")
	assert.Empty(t, tmux.newWindows, "and opens no window")
}
