//go:build integration

package agents

import (
	"fmt"
	"testing"
	"time"

	"komrad/internal/harness/claude"
	"komrad/internal/harness/pane"
	"komrad/internal/harnesstest"
	"komrad/internal/mail"
	agentstore "komrad/internal/store/agent"
	mailstore "komrad/internal/store/mail"
	"komrad/internal/tmuxtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationReconcileAgainstRealTmux pins that the states the reconciler infers from a real pane
// match what the fakes stand in for.
func TestIntegrationReconcileAgainstRealTmux(t *testing.T) {
	it := tmuxtest.New(t)
	cli := it.Client()
	st := newStore(t)
	ctx := t.Context()
	rec := NewReconciler(NewReconcilerParams{
		Panes:    cli,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: cli, Harnesses: mustRegistry(t, claude.NewHarness(claude.NewHarnessParams{ConfigDir: t.TempDir()}))}),
		States:   st.Agents,
	})

	busyPane := it.Spawn("busy", `sh -c 'printf "\033[H· Cooking… (3s · down 1k tokens)\n"; exec cat'`)
	seedRunning(t, st, cli, "busy", busyPane, agentstore.StateIdle)
	it.WaitForFrame(busyPane, "Cooking…")

	idlePane := it.Spawn("idle", `sh -c 'printf "\033[H───────\n❯ \n───────\n-- INSERT --\n"; exec cat'`)
	seedRunning(t, st, cli, "idle", idlePane, agentstore.StateThinking)
	it.WaitForFrame(idlePane, "INSERT")

	unreadablePane := it.Spawn("unreadable", `sh -c 'printf "\033[Hsomething else has the screen\n"; exec cat'`)
	seedRunning(t, st, cli, "unreadable", unreadablePane, agentstore.StateThinking)
	it.WaitForFrame(unreadablePane, "something else")

	waitPane := it.Spawn("wait", `sh -c 'printf "\033[HPick one\n❯ 1. A\n  2. B\nEnter to select · Esc to cancel\n"; exec cat'`)
	seedRunning(t, st, cli, "wait", waitPane, agentstore.StateThinking)
	it.WaitForFrame(waitPane, "Enter to select")

	deadPane := it.Spawn("dead", "sleep 300")
	seedRunning(t, st, cli, "dead", deadPane, agentstore.StateThinking)
	it.Kill("dead")

	exitedPane := it.Spawn("exited", `sh -c 'printf "\033[H❯ ready\n"; exec sh'`)
	exited := seedRunning(t, st, cli, "exited", exitedPane, agentstore.StateThinking)
	it.WaitForFrame(exitedPane, "ready")

	squatPane := it.Spawn("squat", `sh -c 'printf "\033[H· Cooking… (3s · down 1k tokens)\n"; exec cat'`)
	squatter := seed(t, st, "squat", squatPane, agentstore.StateThinking)
	it.WaitForFrame(squatPane, "Cooking…")

	for range reconcileMisses {
		require.NoError(t, rec.Reconcile(ctx))
	}

	waitOutTheShellGrace(rec)
	require.NoError(t, rec.Reconcile(ctx))

	assert.Equal(t, agentstore.StateThinking, stateOf(t, st, "busy"), "the busy agent")
	assert.Equal(t, agentstore.StateIdle, stateOf(t, st, "idle"), "the idle agent")
	assert.Equal(t, agentstore.StateWaiting, stateOf(t, st, "wait"), "the waiting agent")
	assert.Equal(t, agentstore.StateUncertain, stateOf(t, st, "unreadable"), "the agent whose pane nothing recognises")
	assert.Equal(t, agentstore.StateIdle, stateOf(t, st, "dead"), "the dead agent")
	assert.Equal(t, agentstore.StateIdle, stateOf(t, st, "squat"), "the agent holding another pane's id")
	assert.Empty(t, agentByID(t, st, squatter.ID).Pane, "the agent holding another pane's id drops the claim")

	assert.Equal(t, agentstore.StateIdle, stateOf(t, st, "exited"), "the agent whose CLI exited")
	assert.Empty(t, agentByID(t, st, exited.ID).Pane, "the agent whose CLI exited forgets its pane")
	assert.False(t, cli.IsPaneAlive(exitedPane), "the shell's pane is closed")
}

// TestIntegrationExitedCLIStopsBeingANudgeTarget pins that mail is not typed at an agent whose CLI has
// exited, whose pane looks just like one waiting to be told.
func TestIntegrationExitedCLIStopsBeingANudgeTarget(t *testing.T) {
	it := tmuxtest.New(t)
	cli := it.Client()
	st := newStore(t)
	ctx := t.Context()
	rec := NewReconciler(NewReconcilerParams{
		Panes:    cli,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: cli, Harnesses: mustRegistry(t, claude.NewHarness(claude.NewHarnessParams{ConfigDir: t.TempDir()}))}),
		States:   st.Agents,
	})

	exitedPane := it.Spawn("exited", `sh -c 'printf "\033[H❯ ready\n"; exec sh'`)
	agent := seedRunning(t, st, cli, "exited", exitedPane, agentstore.StateThinking)
	it.WaitForFrame(exitedPane, "ready")

	for range reconcileMisses {
		require.NoError(t, rec.Reconcile(ctx))
	}

	waitOutTheShellGrace(rec)
	require.NoError(t, rec.Reconcile(ctx))

	_, err := st.Mail.Send(ctx, mailstore.SendParams{
		FromName:  "somebody",
		ToAgentID: agent.ID,
		Body:      "please read this",
		Deadline:  time.Hour,
	})
	require.NoError(t, err)

	panes := tmuxtest.NewFake()
	svc := mail.NewDeliveryService(mail.NewDeliveryServiceParams{
		MailboxReader: st.Mail,
		NudgeClaimer:  st.Agents,
		PaneReader:    NewPaneLiveness(cli),
		Channel:       pane.NewPane(pane.NewPaneParams{Terminal: panes, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
	})

	got, err := svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, mail.DeliveryLater, got, "delivery to an agent whose CLI exited")
	assert.Empty(t, panes.Sent, "nothing is sent to an agent whose CLI exited")
}

// TestIntegrationReconcileHoldsAPaneOneListingLeftOut pins that a pane missing from a single listing is not
// acted on.
func TestIntegrationReconcileHoldsAPaneOneListingLeftOut(t *testing.T) {
	it := tmuxtest.New(t)
	st := newStore(t)
	ctx := t.Context()
	rec := NewReconciler(NewReconcilerParams{
		Panes:    it.Client(),
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: it.Client(), Harnesses: mustRegistry(t, claude.NewHarness(claude.NewHarnessParams{ConfigDir: t.TempDir()}))}),
		States:   st.Agents,
	})

	it.Spawn("bystander", "sleep 300")
	board := seedBoard(t, it, st, 5)

	for _, agent := range board {
		it.Kill(agent.Name)
	}

	for range listingsBeforePaneGone - 1 {
		require.NoError(t, rec.Reconcile(ctx))
	}

	for _, agent := range board {
		held := agentByID(t, st, agent.ID)
		assert.Equal(t, agent.Pane, held.Pane, "%s keeps its pane", agent.Name)
		assert.Equal(t, agentstore.StateThinking, held.State, "%s stays thinking", agent.Name)
		assert.Nil(t, held.InterruptedAt, "%s records no interruption", agent.Name)
	}

	require.NoError(t, rec.Reconcile(ctx))

	for _, agent := range board {
		after := agentByID(t, st, agent.ID)
		assert.Empty(t, after.Pane, "%s forgets its pane", agent.Name)
		assert.Equal(t, agentstore.StateIdle, after.State, "%s falls idle", agent.Name)
		assert.NotNil(t, after.InterruptedAt, "%s records its unfinished turn as interrupted", agent.Name)
	}
}

// TestIntegrationReconcileHoldsAPaneWhateverElseTheListingKept pins that the hold is per pane rather
// than measured against the size of the loss, so a listing that dropped a minority of the
// board is held on the same terms as one that dropped all of it.
func TestIntegrationReconcileHoldsAPaneWhateverElseTheListingKept(t *testing.T) {
	it := tmuxtest.New(t)
	st := newStore(t)
	ctx := t.Context()
	rec := NewReconciler(NewReconcilerParams{
		Panes:    it.Client(),
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: it.Client(), Harnesses: mustRegistry(t, claude.NewHarness(claude.NewHarnessParams{ConfigDir: t.TempDir()}))}),
		States:   st.Agents,
	})

	board := seedBoard(t, it, st, 8)
	for _, agent := range board[:3] {
		it.Kill(agent.Name)
	}

	require.NoError(t, rec.Reconcile(ctx))

	for _, agent := range board {
		after := agentByID(t, st, agent.ID)
		assert.Equal(t, agent.Pane, after.Pane, "%s keeps its pane", agent.Name)
		assert.Nil(t, after.InterruptedAt, "%s records no interruption", agent.Name)
	}
}

// TestIntegrationReconcileForgetsAPaneThatStaysGone pins that an ordinary exit still settles, the hold
// costing it the passes it takes to tell an exit from a listing that came back short.
func TestIntegrationReconcileForgetsAPaneThatStaysGone(t *testing.T) {
	it := tmuxtest.New(t)
	st := newStore(t)
	ctx := t.Context()
	rec := NewReconciler(NewReconcilerParams{
		Panes:    it.Client(),
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: it.Client(), Harnesses: mustRegistry(t, claude.NewHarness(claude.NewHarnessParams{ConfigDir: t.TempDir()}))}),
		States:   st.Agents,
	})

	board := seedBoard(t, it, st, 5)
	gone := board[0]
	it.Kill(gone.Name)

	for range listingsBeforePaneGone {
		require.NoError(t, rec.Reconcile(ctx))
	}

	after := agentByID(t, st, gone.ID)
	assert.Empty(t, after.Pane, "the agent that exited forgets its pane")
	assert.Equal(t, agentstore.StateIdle, after.State, "the agent that exited falls idle")

	for _, agent := range board[1:] {
		still := agentByID(t, st, agent.ID)
		assert.Equal(t, agent.Pane, still.Pane, "%s keeps its pane", agent.Name)
		assert.Equal(t, agentstore.StateThinking, still.State, "%s stays thinking", agent.Name)
	}
}

// seedBoard spawns count agents that are all working.
func seedBoard(t *testing.T, it *tmuxtest.Server, st *testStore, count int) []agentstore.Agent {
	t.Helper()

	board := make([]agentstore.Agent, 0, count)
	for i := range count {
		name := fmt.Sprintf("agent-%d", i)
		spawned := it.Spawn(name, `sh -c 'printf "\033[H· Cooking… (3s · down 1k tokens)\n"; exec cat'`)
		board = append(board, seedRunning(t, st, it.Client(), name, spawned, agentstore.StateThinking))
		it.WaitForFrame(spawned, "Cooking…")
	}

	return board
}
