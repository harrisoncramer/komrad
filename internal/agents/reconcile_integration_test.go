package agents

import (
	"fmt"
	"testing"

	"weave/internal/harness"
	"weave/internal/harness/claude"
	agentstore "weave/internal/store/agent"
	"weave/internal/tmux"
	"weave/internal/tmuxtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReconcileAgainstRealTmux pins that the states the reconciler infers from a real pane
// match what the fakes stand in for.
func TestReconcileAgainstRealTmux(t *testing.T) {
	it := tmuxtest.New(t)
	cli := it.Client()
	st := newStore(t)
	ctx := t.Context()
	rec := NewReconciler(NewReconcilerParams{Panes: cli, Harnesses: mustRegistry(t, claude.NewHarness(claude.NewHarnessParams{ProjectsDir: t.TempDir()})), States: st.Agents})

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
	assert.Empty(t, cli.GetPaneOwner(exitedPane), "the abandoned pane's stamp is cleared")
	assert.True(t, cli.IsPaneAlive(exitedPane), "the shell's pane is left standing")
}

// TestExitedCLIStopsBeingANudgeTarget pins that mail is not typed at an agent whose CLI has
// exited, whose pane looks just like one waiting to be told.
func TestExitedCLIStopsBeingANudgeTarget(t *testing.T) {
	it := tmuxtest.New(t)
	cli := it.Client()
	st := newStore(t)
	ctx := t.Context()
	rec := NewReconciler(NewReconcilerParams{Panes: cli, Harnesses: mustRegistry(t, claude.NewHarness(claude.NewHarnessParams{ProjectsDir: t.TempDir()})), States: st.Agents})

	pane := it.Spawn("exited", `sh -c 'printf "\033[H❯ ready\n"; exec sh'`)
	agent := seedRunning(t, st, cli, "exited", pane, agentstore.StateThinking)
	it.WaitForFrame(pane, "ready")

	for range reconcileMisses {
		require.NoError(t, rec.Reconcile(ctx))
	}

	waitOutTheShellGrace(rec)
	require.NoError(t, rec.Reconcile(ctx))

	after := agentByID(t, st, agent.ID)
	panes := &fakePanes{}
	svc := NewDeliveryService(NewDeliveryServiceParams{
		MailboxReader: &fakeMailboxReader{pending: 1},
		NudgeClaimer:  &fakeAgents{agent: &after, claimed: true},
		PaneReader:    NewPaneLiveness(cli),
		PaneTyper:     panes,
	})

	got, err := svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, DeliveryLater, got, "delivery to an agent whose CLI exited")
	assert.Empty(t, panes.text, "nothing is typed at an agent whose CLI exited")
	assert.Empty(t, panes.keys, "and no keys are sent to it either")
}

func mustRegistry(t *testing.T, harnesses ...harness.Harness) *harness.Registry {
	t.Helper()
	registry, err := harness.NewRegistry(harnesses...)
	require.NoError(t, err)

	return registry
}

// TestReconcileHoldsAPaneOneListingLeftOut pins that a pane missing from a single listing is not
// acted on.
func TestReconcileHoldsAPaneOneListingLeftOut(t *testing.T) {
	it := tmuxtest.New(t)
	st := newStore(t)
	ctx := t.Context()
	rec := NewReconciler(NewReconcilerParams{Panes: it.Client(), Harnesses: mustRegistry(t, claude.NewHarness(claude.NewHarnessParams{ProjectsDir: t.TempDir()})), States: st.Agents})

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

// TestReconcileHoldsAPaneWhateverElseTheListingKept pins that the hold is per pane rather
// than measured against the size of the loss, so a listing that dropped a minority of the
// board is held on the same terms as one that dropped all of it.
func TestReconcileHoldsAPaneWhateverElseTheListingKept(t *testing.T) {
	it := tmuxtest.New(t)
	st := newStore(t)
	ctx := t.Context()
	rec := NewReconciler(NewReconcilerParams{Panes: it.Client(), Harnesses: mustRegistry(t, claude.NewHarness(claude.NewHarnessParams{ProjectsDir: t.TempDir()})), States: st.Agents})

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

// TestReconcileForgetsAPaneThatStaysGone pins that an ordinary exit still settles, the hold
// costing it the passes it takes to tell an exit from a listing that came back short.
func TestReconcileForgetsAPaneThatStaysGone(t *testing.T) {
	it := tmuxtest.New(t)
	st := newStore(t)
	ctx := t.Context()
	rec := NewReconciler(NewReconcilerParams{Panes: it.Client(), Harnesses: mustRegistry(t, claude.NewHarness(claude.NewHarnessParams{ProjectsDir: t.TempDir()})), States: st.Agents})

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

// TestReconcileReturnsAHoldWhenTheListingComesBack pins that a pane the listing mentions
// again returns what its absence had accrued, so a run of blips never adds up to an exit.
func TestReconcileReturnsAHoldWhenTheListingComesBack(t *testing.T) {
	it := tmuxtest.New(t)
	st := newStore(t)
	ctx := t.Context()
	dropping := &listingDroppingPanes{Client: it.Client()}
	rec := NewReconciler(NewReconcilerParams{Panes: dropping, Harnesses: mustRegistry(t, claude.NewHarness(claude.NewHarnessParams{ProjectsDir: t.TempDir()})), States: st.Agents})

	board := seedBoard(t, it, st, 5)

	// Explainer: this last agent sits on a pane that captures nothing, because a tmux degraded
	// enough to drop panes from a listing is the same tmux whose captures come back empty, and
	// an empty capture returns before apply. Every other agent here reaches apply on a present
	// pass, and apply clears the absences along with the rest of the debounce, so without this
	// one the test would pass whether or not a returning listing gives a hold back.
	quietPane := it.Spawn("quiet", `sh -c 'exec cat'`)
	board = append(board, seedRunning(t, st, it.Client(), "quiet", quietPane, agentstore.StateThinking))
	require.Empty(t, it.Client().CapturePane(quietPane), "the quiet agent's pane captures nothing")

	for range listingsBeforePaneGone * 3 {
		dropping.drop = !dropping.drop
		require.NoError(t, rec.Reconcile(ctx))
	}

	for _, agent := range board {
		after := agentByID(t, st, agent.ID)
		assert.Equal(t, agent.Pane, after.Pane, "%s keeps its pane", agent.Name)
		assert.Nil(t, after.InterruptedAt, "%s records no interruption", agent.Name)
	}
}

// listingDroppingPanes is a tmux that answers about panes that are all still running while
// leaving every one of them out of the listing, which is the shape of a degraded read.
type listingDroppingPanes struct {
	*tmux.Client
	drop bool
}

func (l *listingDroppingPanes) GetPanes() (map[string]tmux.PaneStatus, error) {
	panes, err := l.Client.GetPanes()
	if err != nil || !l.drop {
		return panes, err
	}

	return map[string]tmux.PaneStatus{}, nil
}

// seedBoard spawns count agents that are all working, which is the board a listing that came
// back short has the most to take from.
func seedBoard(t *testing.T, it *tmuxtest.Server, st *testStore, count int) []agentstore.Agent {
	t.Helper()

	board := make([]agentstore.Agent, 0, count)
	for i := range count {
		name := fmt.Sprintf("agent-%d", i)
		pane := it.Spawn(name, `sh -c 'printf "\033[H· Cooking… (3s · down 1k tokens)\n"; exec cat'`)
		board = append(board, seedRunning(t, st, it.Client(), name, pane, agentstore.StateThinking))
		it.WaitForFrame(pane, "Cooking…")
	}

	return board
}
