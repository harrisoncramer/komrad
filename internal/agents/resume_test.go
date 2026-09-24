package agents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"weave/internal/harness/pane"
	"weave/internal/harnesstest"
	"weave/internal/scripts"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/tmuxtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResumeDir pins that a restore produces a usable directory whatever became of the recorded
// one.
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

			service := NewResumeService(NewResumeServiceParams{FallbackResumeRoot: fallbackResumeRoot})
			got := service.getResumeDir(agentstore.Agent{
				ID:        "agent-id",
				Cwd:       cwd,
				SessionID: sessionID,
			})

			assert.Equal(t, tt.wantDir(root, fallbackResumeRoot), got)
			require.NoError(t, scripts.RequireUsableDir(got), "a restore always gets a directory it can start in")
		})
	}
}

// TestAnInterruptedTurnIsAskedForAgain pins the resume asking again as an ordinary message.
func TestAnInterruptedTurnIsAskedForAgain(t *testing.T) {
	const sessionID = "0f0b6f2c-6f8f-4a0e-9c2f-2a8f9d0a1b22"

	cut := func(t *testing.T, state agentstore.State, session string) (*testAgents, *testStore, *harnesstest.Fake, agentstore.Agent) {
		t.Helper()

		st := newStore(t)
		tmux := tmuxtest.NewFake()
		agent := seed(t, st, "cut", "%1", state)
		if session != "" {
			require.NoError(t, st.Agents.SetSessionID(t.Context(), agent.ID, session))
			agent.SessionID = session
		}
		service, h := newManagerWithHarness(tmux, st, "")

		rec := NewReconciler(NewReconcilerParams{
			Panes:    tmux,
			Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: h}}),
			States:   st.Agents,
		})
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
		require.Len(t, h.Launches, 1)
		assert.Equal(t, interruptedPrompt, h.Launches[0].Prompt)
		assert.Nil(t, agentByID(t, st, agent.ID).InterruptedAt, "the ask is made once, not on every resume after it")
	})

	t.Run("a pane that goes while its agent waits on a person leaves the turn recorded as cut off", func(t *testing.T) {
		_, st, _, agent := cut(t, agentstore.StateWaiting, sessionID)
		assert.NotNil(t, agentByID(t, st, agent.ID).InterruptedAt,
			"an unanswered tool call is no more finished than a turn still being thought about")
	})

	t.Run("an agent archived mid-turn is settled by the archive and asked again once restored", func(t *testing.T) {
		st := newStore(t)
		tmux := tmuxtest.NewFake()
		agent := seedRunning(t, st, tmux, "cut", "%1", agentstore.StateThinking)
		require.NoError(t, st.Agents.SetSessionID(t.Context(), agent.ID, sessionID))
		service, h := newManagerWithHarness(tmux, st, "")

		require.NoError(t, service.lifecycle.ArchiveAgents(t.Context(), []string{agent.ID}))
		archived := agentByID(t, st, agent.ID)
		assert.Equal(t, agentstore.StateIdle, archived.State, "no reconcile pass visits an archived row, so the archive settles it")
		require.NotNil(t, archived.InterruptedAt)

		_, err := service.lifecycle.RestoreAgents(t.Context(), []string{agent.ID})
		require.NoError(t, err)
		assert.Equal(t, agentstore.StateIdle, agentByID(t, st, agent.ID).State, "the restore brings back a settled row")

		_, err = service.resume.ResumeAgent(t.Context(), agent.ID)
		require.NoError(t, err)
		require.Len(t, h.Launches, 1)
		assert.Equal(t, interruptedPrompt, h.Launches[0].Prompt)
	})

	// What `mise run restart` does: the tmux server goes whole and weave's own server goes
	// with it, so no reconcile pass ever watches the panes go and the store is left holding
	// a turn that is already over.
	unwatched := func(t *testing.T, state agentstore.State) (*testAgents, *testStore, *harnesstest.Fake, agentstore.Agent) {
		t.Helper()

		st := newStore(t)
		agent := seed(t, st, "cut", "%1", state)
		require.NoError(t, st.Agents.SetSessionID(t.Context(), agent.ID, sessionID))
		service, h := newManagerWithHarness(tmuxtest.NewFake(), st, "")
		require.Nil(t, agentByID(t, st, agent.ID).InterruptedAt, "no pass looked, so nothing recorded the turn")

		return service, st, h, agent
	}

	t.Run("a turn the store still has running is asked for again even if nothing recorded it as cut off", func(t *testing.T) {
		service, _, h, agent := unwatched(t, agentstore.StateThinking)

		_, err := service.resume.ResumeAgent(t.Context(), agent.ID)
		require.NoError(t, err)
		require.Len(t, h.Launches, 1)
		assert.Equal(t, interruptedPrompt, h.Launches[0].Prompt)
	})

	t.Run("a turn left waiting on a person is asked for again even if nothing recorded it as cut off", func(t *testing.T) {
		service, _, h, agent := unwatched(t, agentstore.StateWaiting)

		_, err := service.resume.ResumeAgent(t.Context(), agent.ID)
		require.NoError(t, err)
		require.Len(t, h.Launches, 1)
		assert.Equal(t, interruptedPrompt, h.Launches[0].Prompt)
	})

	t.Run("a relaunch that fails leaves the turn for the resume after it", func(t *testing.T) {
		service, st, h, agent := unwatched(t, agentstore.StateThinking)
		h.CommandErr = errors.New("no")

		_, err := service.resume.ResumeAgent(t.Context(), agent.ID)
		require.Error(t, err)
		assert.NotNil(t, agentByID(t, st, agent.ID).InterruptedAt,
			"the state it was read from is gone, so the turn only survives as the stamp")
	})

	t.Run("an agent that finished its turn resumes without being told anything", func(t *testing.T) {
		service, _, h, agent := cut(t, agentstore.StateIdle, sessionID)

		_, err := service.resume.ResumeAgent(t.Context(), agent.ID)
		require.NoError(t, err)
		require.Len(t, h.Launches, 1)
		assert.Empty(t, h.Launches[0].Prompt)
	})

	t.Run("an agent with no conversation to reopen is not asked about a turn it cannot read", func(t *testing.T) {
		service, st, h, agent := cut(t, agentstore.StateThinking, "")

		_, err := service.resume.ResumeAgent(t.Context(), agent.ID)
		require.NoError(t, err)
		require.Len(t, h.Launches, 1)
		assert.Empty(t, h.Launches[0].Prompt, "a fresh conversation cannot be asked to read back over itself")
		assert.False(t, h.Launches[0].Resume, "there is no conversation to reopen")
		assert.Nil(t, agentByID(t, st, agent.ID).InterruptedAt,
			"the conversation that held the turn is gone, so nothing is left to ask about it")
	})

	t.Run("a turn cut off before its conversation was named is not asked about after a fresh one is", func(t *testing.T) {
		service, st, h, agent := cut(t, agentstore.StateThinking, "")

		_, err := service.resume.ResumeAgent(t.Context(), agent.ID)
		require.NoError(t, err)
		require.Len(t, h.Launches, 1)
		require.Empty(t, h.Launches[0].Prompt)

		// What a Codex agent does on the way back in: the fresh conversation it started is
		// matched to its rollout file, and the panel adopts that id as the agent's own.
		const learned = "9a1c7f10-2b3d-4e5f-8a9b-0c1d2e3f4a5b"
		require.NoError(t, st.Agents.SetSessionID(t.Context(), agent.ID, learned))
		require.NoError(t, st.Agents.SetStateByID(t.Context(), agent.ID, agentstore.StateIdle))

		tmux := tmuxtest.NewFake()
		rec := NewReconciler(NewReconcilerParams{
			Panes:    tmux,
			Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: h}}),
			States:   st.Agents,
		})
		for range listingsBeforePaneGone {
			require.NoError(t, rec.Reconcile(t.Context()))
		}

		_, err = service.resume.ResumeAgent(t.Context(), agent.ID)
		require.NoError(t, err)
		require.Len(t, h.Launches, 2)
		assert.True(t, h.Launches[1].Resume, "the conversation it learned is what this reopens")
		assert.Empty(t, h.Launches[1].Prompt, "the conversation this reopens ran its turn to the end")
	})
}

// TestATurnCutOffWithNoTmuxServerIsAskedForAgain pins the resume making the ask itself when no
// pass ever ran.
func TestATurnCutOffWithNoTmuxServerIsAskedForAgain(t *testing.T) {
	const sessionID = "1c7a3e8b-4d5f-4a6b-9c8d-7e6f5a4b3c21"

	st := newStore(t)
	tmux := tmuxtest.NewFake()
	agent := seed(t, st, "cut", "%1", agentstore.StateThinking)
	require.NoError(t, st.Agents.SetSessionID(t.Context(), agent.ID, sessionID))

	service, h := newManagerWithHarness(tmux, st, "")

	tmux.PaneOwnersErr = errors.New("error connecting to /private/tmp/tmux-501/weave")
	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: h}}),
		States:   st.Agents,
	})
	require.Error(t, rec.Reconcile(t.Context()))
	tmux.PaneOwnersErr = nil

	stored := agentByID(t, st, agent.ID)
	require.Nil(t, stored.InterruptedAt, "no pass could look at a pane, so nothing recorded the turn")
	require.Equal(t, agentstore.StateThinking, stored.State)

	_, err := service.resume.ResumeAgent(t.Context(), agent.ID)
	require.NoError(t, err)
	require.Len(t, h.Launches, 1)
	assert.Equal(t, interruptedPrompt, h.Launches[0].Prompt)

	stored = agentByID(t, st, agent.ID)
	tmux.KillPane(stored.Pane)

	_, err = service.resume.ResumeAgent(t.Context(), agent.ID)
	require.NoError(t, err)
	require.Len(t, h.Launches, 2)
	assert.Empty(t, h.Launches[1].Prompt, "the ask is made once, and the row it was read from now says idle")
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
	tmux := tmuxtest.NewFake()
	ctx := t.Context()

	created, err := st.Workflows.Create(ctx, workflowstore.Config{
		Name:     "worktrees",
		Cwd:      fakeCheckout(t),
		Worktree: workflowstore.WorktreeConfig{Enabled: true, BaseBranch: "main"},
	})
	require.NoError(t, err)

	agent := seedInWorkflow(t, st, created.ID, "agent", "", agentstore.StateIdle)

	lifecycle := &blockingLifecycle{entered: make(chan struct{}), release: make(chan struct{})}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

	done := make(chan error, 1)
	go func() {
		_, resumeErr := manager.resume.ResumeAgent(context.WithoutCancel(ctx), agent.ID)
		done <- resumeErr
	}()

	<-lifecycle.entered

	assert.Equal(t, agentstore.StateSettingUp, agentByID(t, st, agent.ID).State,
		"the row says a resume is in flight before the resume does any work")

	_, err = manager.lifecycle.ArchiveUnchangedAgents(ctx, []agentstore.Agent{agent})
	require.NoError(t, err)

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
// resume finished first and the row a pass read is still the row it archives.
func TestAPassArchivesAResumeThatFinishedFirst(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()
	ctx := t.Context()

	created, err := st.Workflows.Create(ctx, workflowstore.Config{
		Name:     "worktrees",
		Cwd:      t.TempDir(),
		Worktree: workflowstore.WorktreeConfig{Enabled: true, BaseBranch: "main"},
	})
	require.NoError(t, err)

	agent := seedInWorkflow(t, st, created.ID, "agent", "", agentstore.StateIdle)

	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, &fakeLifecycle{})
	resumed, err := manager.resume.ResumeAgent(ctx, agent.ID)
	require.NoError(t, err)
	require.NotEmpty(t, resumed.Pane)

	_, err = manager.lifecycle.ArchiveUnchangedAgents(ctx, []agentstore.Agent{agentByID(t, st, agent.ID)})
	require.NoError(t, err)

	assert.NotNil(t, agentByID(t, st, agent.ID).ArchivedAt)
	assert.Equal(t, []string{resumed.Pane}, tmux.KilledPanes, "the pane goes with the archive it belongs to")
}

// TestAResumeThatGivesUpPutsTheStateBack pins the state going back.
func TestAResumeThatGivesUpPutsTheStateBack(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	agent := seedInWorkflow(t, st, workflowID, "cut", "", agentstore.StateThinking)

	lifecycle := &fakeLifecycle{resumeErr: errors.New("git worktree add: already checked out")}
	require.NoError(t, st.Workflows.Update(ctx, workflowID, workflowstore.Config{
		Name:     "workflow",
		Cwd:      fakeCheckout(t),
		Worktree: workflowstore.WorktreeConfig{Enabled: true, BaseBranch: "main"},
	}))

	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

	_, err := manager.resume.ResumeAgent(ctx, agent.ID)
	require.Error(t, err)

	assert.Equal(t, agentstore.StateThinking, agentByID(t, st, agent.ID).State,
		"a resume that gave up leaves the row reading as it found it")
}

// TestASecondResumeOpensNoSecondWindow pins the claim inside the resume, which several resumes
// of one agent can be in flight against.
func TestASecondResumeOpensNoSecondWindow(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	agent := seedInWorkflow(t, st, workflowID, "dormant", "", agentstore.StateIdle)

	// The second resume arrives while the first is part-way through.
	arrived := make(chan struct{})
	release := make(chan struct{})
	tmux.BeforeNewWindow = func() {
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

	assert.Equal(t, 1, tmux.WindowsOpened(), "the second resume opened no window of its own")
}

// TestAResumeIsClaimableAgainAfterOneFinishes pins that the claim is held for the length of a
// resume rather than for the life of the agent.
func TestAResumeIsClaimableAgainAfterOneFinishes(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	agent := seedInWorkflow(t, st, workflowID, "dormant", "", agentstore.StateIdle)

	manager := newManager(tmux, st, "")

	_, err := manager.resume.ResumeAgent(ctx, agent.ID)
	require.NoError(t, err)

	tmux.KillPane(agentByID(t, st, agent.ID).Pane)

	_, err = manager.resume.ResumeAgent(ctx, agent.ID)
	require.NoError(t, err, "the claim goes with the resume that took it")
	assert.Equal(t, 2, tmux.WindowsOpened())
}

// archivingTmux archives the agent the first time a resume asks whether it is already running,
// which is the only call between the caller reading the row and the resume marking it.
type archivingTmux struct {
	*tmuxtest.Fake
	sync func()
	once sync.Once
}

func (a *archivingTmux) GetPaneOwner(pane string) string {
	a.once.Do(a.sync)

	return a.Fake.GetPaneOwner(pane)
}

// TestAPassThatArchivesBeforeTheMarkStopsTheResume pins the mark refusing an archived row,
// which is where a resume finds out about an archive that landed while it was reading.
func TestAPassThatArchivesBeforeTheMarkStopsTheResume(t *testing.T) {
	st := newStore(t)
	base := tmuxtest.NewFake()
	ctx := t.Context()

	created, err := st.Workflows.Create(ctx, workflowstore.Config{
		Name:     "worktrees",
		Cwd:      t.TempDir(),
		Worktree: workflowstore.WorktreeConfig{Enabled: true, BaseBranch: "main"},
	})
	require.NoError(t, err)

	agent := seedInWorkflow(t, st, created.ID, "agent", "", agentstore.StateIdle)

	tmux := &archivingTmux{Fake: base}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, &fakeLifecycle{})
	tmux.sync = func() {
		_, syncErr := manager.lifecycle.ArchiveUnchangedAgents(ctx, []agentstore.Agent{agent})
		require.NoError(t, syncErr)
	}

	_, err = manager.resume.ResumeAgent(ctx, agent.ID)
	require.ErrorIs(t, err, ErrAgentArchived)

	after := agentByID(t, st, agent.ID)
	require.NotNil(t, after.ArchivedAt, "the pass really did archive it")
	assert.Empty(t, after.Pane, "and the resume wrote no pane onto the archived row")
	assert.Empty(t, base.NewWindows, "the resume gave up before it opened a window")

	live, err := st.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, live, "nothing is left running that no listing shows")
}

// TestAnArchiveAfterTheMarkStopsTheResume covers the window past the mark, where the archive
// kills a pane the row does not name yet and only the resume can take away the one it opened.
func TestAnArchiveAfterTheMarkStopsTheResume(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()
	ctx := t.Context()

	created, err := st.Workflows.Create(ctx, workflowstore.Config{
		Name:     "worktrees",
		Cwd:      fakeCheckout(t),
		Worktree: workflowstore.WorktreeConfig{Enabled: true, BaseBranch: "main"},
	})
	require.NoError(t, err)

	agent := seedInWorkflow(t, st, created.ID, "cut", "", agentstore.StateIdle)

	h := &harnesstest.Fake{}
	lifecycle := &blockingLifecycle{entered: make(chan struct{}), release: make(chan struct{})}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: h}, lifecycle)

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
	assert.Len(t, tmux.KilledPanes, 1, "the pane the resume opened goes with the resume that lost")
	assert.Empty(t, h.Launches, "and no CLI was launched into a pane no listing shows")

	live, err := st.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, live, "nothing is left running that no listing shows")
}

// TestADeleteAfterTheMarkStopsTheResume covers the same window ended by a delete, which drops
// the row and would leave a launched CLI attached to an id nothing holds.
func TestADeleteAfterTheMarkStopsTheResume(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()
	ctx := t.Context()

	created, err := st.Workflows.Create(ctx, workflowstore.Config{
		Name:     "worktrees",
		Cwd:      fakeCheckout(t),
		Worktree: workflowstore.WorktreeConfig{Enabled: true, BaseBranch: "main"},
	})
	require.NoError(t, err)

	agent := seedInWorkflow(t, st, created.ID, "cut", "", agentstore.StateIdle)

	h := &harnesstest.Fake{}
	lifecycle := &blockingLifecycle{entered: make(chan struct{}), release: make(chan struct{})}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: h}, lifecycle)

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

	assert.Len(t, tmux.KilledPanes, 1, "the pane the resume opened goes with the resume that lost")
	assert.Empty(t, h.Launches, "no CLI was launched for an agent no row holds")

	rows, err := st.ListAll(ctx)
	require.NoError(t, err)
	assert.Empty(t, rows, "the delete really did take the row")
}

// refusingUpdate is the store with its last write to the row taken away.
type refusingUpdate struct {
	*agentstore.AgentStore
}

func (refusingUpdate) UpdateIfLive(context.Context, agentstore.Agent) (bool, error) {
	return false, errors.New("the row would not take the write")
}

// TestAResumeThatFailsOnItsLastWritePutsBackWhatItFound pins that the state on the row when
// the resume started goes back, not the idle it was about to write.
func TestAResumeThatFailsOnItsLastWritePutsBackWhatItFound(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	agent := seedInWorkflow(t, st, workflowID, "cut", "", agentstore.StateThinking)

	manager := st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}},
		addr:      "127.0.0.1:9999",
		lifecycle: &fakeLifecycle{},
		agents:    refusingUpdate{AgentStore: st.Agents},
	})

	_, err := manager.resume.ResumeAgent(ctx, agent.ID)
	require.Error(t, err)

	after := agentByID(t, st, agent.ID)
	assert.Equal(t, agentstore.StateThinking, after.State, "the row reads as the resume found it")
	assert.Empty(t, after.Pane)
	assert.Len(t, tmux.KilledPanes, 1, "the window it opened goes with it")
}

// TestResumeRunsTheProvisioner pins the provisioner firing again on resume.
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
			tmux := tmuxtest.NewFake()
			ctx := t.Context()

			recorded := t.TempDir()
			named := tt.named
			if named == "named" {
				named = t.TempDir()
			}

			lifecycle := &fakeLifecycle{resumeDir: named, resumeErr: tt.resumeErr}
			manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

			created, err := st.Workflows.Create(ctx, workflowstore.Config{
				Name:     "resume-" + tt.name,
				Cwd:      fakeCheckout(t),
				Worktree: workflowstore.WorktreeConfig{Enabled: true, BaseBranch: "main"},
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
				assert.Empty(t, tmux.NewWindows, "a refused script opens no window")
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
			assert.Equal(t, want, tmux.NewWindows["auth"], "the window opens where the script said")
		})
	}
}

// TestResumeWithoutAProvisionerFiresNothing covers the ordinary workflow, which names no
// provisioner and comes back up the way it always has.
func TestResumeWithoutAProvisionerFiresNothing(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()
	lifecycle := &fakeLifecycle{}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

	agent := seed(t, st, "auth", "%0", agentstore.StateIdle)

	_, err := manager.resume.ResumeAgent(t.Context(), agent.ID)

	require.NoError(t, err)
	assert.Empty(t, lifecycle.resumed)
}

// TestResumeGivesBackThePaneItOpened pins a resume that opens a window and then refuses giving
// it up.
func TestResumeGivesBackThePaneItOpened(t *testing.T) {
	tests := []struct {
		name        string
		setOwnerErr error
		respawnErr  error
	}{
		{
			name:        "a pane tmux refuses to stamp",
			setOwnerErr: errors.New("tmux refused the option"),
		},
		{
			name:       "a relaunch that never starts in the pane",
			respawnErr: errors.New("tmux refused the command"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			tmux.SetOwnerErr = tt.setOwnerErr
			tmux.RespawnErr = tt.respawnErr
			st := newStore(t)
			manager := newManager(tmux, st, "")
			ctx := t.Context()

			agent := seed(t, st, "auth", "", agentstore.StateThinking)

			_, err := manager.resume.ResumeAgent(ctx, agent.ID)

			require.Error(t, err)
			assert.Equal(t, []string{"%auth"}, tmux.KilledPanes, "the window it opened is taken away")
			assert.Equal(t, agentstore.StateThinking, agentByID(t, st, agent.ID).State,
				"and the row is left reading as the resume found it")
		})
	}
}

// TestResumeAfterARefusedRelaunchGetsThrough pins the recovery from a pane left behind stamped
// with the agent's id.
func TestResumeAfterARefusedRelaunchGetsThrough(t *testing.T) {
	tmux := tmuxtest.NewFake()
	tmux.RespawnErr = errors.New("tmux refused the command")
	st := newStore(t)
	harness := &harnesstest.Fake{}
	manager := st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: harnesstest.Registry{Harness: harness},
		lifecycle: &fakeLifecycle{},
	})
	ctx := t.Context()

	agent := seed(t, st, "auth", "", agentstore.StateThinking)

	_, err := manager.resume.ResumeAgent(ctx, agent.ID)
	require.Error(t, err)

	tmux.RespawnErr = nil

	resumed, err := manager.resume.ResumeAgent(ctx, agent.ID)

	require.NoError(t, err)
	assert.Len(t, harness.Launches, 2, "the second resume reaches the CLI rather than reporting a pane that is already running")
	assert.Equal(t, agentstore.StateIdle, resumed.State)
}

// TestResumeRefusesArchived pins the refusal naming the way out.
func TestResumeRefusesArchived(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := newManager(tmux, st, "")
	ctx := t.Context()

	archived := seed(t, st, "archived", "%0", agentstore.StateIdle)
	require.NoError(t, st.SoftDelete(ctx, "archived"))

	_, err := manager.resume.ResumeAgent(ctx, archived.ID)

	require.ErrorIs(t, err, ErrAgentArchived)
	require.ErrorContains(t, err, "restore it first", "the refusal says what to do about it")
	assert.Empty(t, tmux.NewWindows, "and opens no window")
}
