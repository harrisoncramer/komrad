package agents

import (
	"context"
	"fmt"
	"testing"

	"weave/internal/harness"
	"weave/internal/harness/claude"
	agentstore "weave/internal/store/agent"
	"weave/internal/tmux"
	"weave/internal/tmuxtest"
)

// TestReconcileAgainstRealTmux pins that the states the reconciler infers from a real pane
// match what the fakes stand in for.
func TestReconcileAgainstRealTmux(t *testing.T) {
	it := tmuxtest.New(t)
	cli := it.Client()
	st := newStore(t)
	ctx := context.Background()
	rec := NewReconciler(cli, mustRegistry(t, claude.NewHarness(claude.NewHarnessParams{ProjectsDir: t.TempDir()})), st.Agents)

	busyPane := it.Spawn("busy", `sh -c 'printf "\033[H· Cooking… (3s · down 1k tokens)\n"; exec cat'`)
	seedRunning(t, st, cli, "busy", busyPane, agentstore.StateIdle)
	it.WaitForFrame(busyPane, "Cooking…")

	idlePane := it.Spawn("idle", `sh -c 'printf "\033[H───────\n❯ \n───────\n-- INSERT --\n"; exec cat'`)
	seedRunning(t, st, cli, "idle", idlePane, agentstore.StateThinking)
	it.WaitForFrame(idlePane, "INSERT")

	pagerPane := it.Spawn("pager", `sh -c 'printf "\033[Hcommit 528341be\ncommit c107b42c\n:\n"; exec cat'`)
	seedRunning(t, st, cli, "pager", pagerPane, agentstore.StateThinking)
	it.WaitForFrame(pagerPane, "commit")

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
		if err := rec.Reconcile(ctx); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
	}

	waitOutTheShellGrace(rec)
	if err := rec.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if got := stateOf(t, st, "busy"); got != agentstore.StateThinking {
		t.Fatalf("busy agent: want thinking, got %q", got)
	}
	if got := stateOf(t, st, "idle"); got != agentstore.StateIdle {
		t.Fatalf("idle agent: want idle, got %q", got)
	}
	if got := stateOf(t, st, "wait"); got != agentstore.StateWaiting {
		t.Fatalf("waiting agent: want waiting, got %q", got)
	}
	if got := stateOf(t, st, "pager"); got != agentstore.StateUncertain {
		t.Fatalf("agent whose pane a pager took: want uncertain, got %q", got)
	}
	if got := stateOf(t, st, "dead"); got != agentstore.StateIdle {
		t.Fatalf("dead agent: want idle, got %q", got)
	}
	if got := stateOf(t, st, "squat"); got != agentstore.StateIdle {
		t.Fatalf("agent holding another pane's id: want idle, got %q", got)
	}

	after := agentByID(t, st, squatter.ID)
	if after.Pane != "" {
		t.Fatalf("agent holding another pane's id: want the claim dropped, still holds %q", after.Pane)
	}

	if got := stateOf(t, st, "exited"); got != agentstore.StateIdle {
		t.Fatalf("agent whose CLI exited: want idle, got %q", got)
	}

	gone := agentByID(t, st, exited.ID)
	if gone.Pane != "" {
		t.Fatalf("agent whose CLI exited: want the pane forgotten, still holds %q", gone.Pane)
	}
	if owner := cli.GetPaneOwner(exitedPane); owner != "" {
		t.Fatalf("agent whose CLI exited: want the abandoned pane's stamp cleared, got %q", owner)
	}
	if !cli.IsPaneAlive(exitedPane) {
		t.Fatalf("agent whose CLI exited: the shell's pane should be left standing")
	}
}

// TestExitedCLIStopsBeingANudgeTarget pins that mail is not typed at an agent whose CLI has
// exited, since its pane looks just like one waiting to be told.
func TestExitedCLIStopsBeingANudgeTarget(t *testing.T) {
	it := tmuxtest.New(t)
	cli := it.Client()
	st := newStore(t)
	ctx := context.Background()
	rec := NewReconciler(cli, mustRegistry(t, claude.NewHarness(claude.NewHarnessParams{ProjectsDir: t.TempDir()})), st.Agents)

	pane := it.Spawn("exited", `sh -c 'printf "\033[H❯ ready\n"; exec sh'`)
	agent := seedRunning(t, st, cli, "exited", pane, agentstore.StateThinking)
	it.WaitForFrame(pane, "ready")

	for range reconcileMisses {
		if err := rec.Reconcile(ctx); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
	}

	waitOutTheShellGrace(rec)
	if err := rec.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	after := agentByID(t, st, agent.ID)
	panes := &fakePanes{}
	svc := NewDeliveryService(NewDeliveryServiceParams{
		MailboxReader: &fakeMailboxReader{pending: 1},
		NudgeClaimer:  &fakeAgents{agent: &after, claimed: true},
		PaneReader:    NewPaneLiveness(cli),
		PaneTyper:     panes,
	})

	got, err := svc.Deliver(ctx, agent.ID)
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if got != DeliveryLater {
		t.Fatalf("delivery to an agent whose CLI exited: want DeliveryLater, got %v", got)
	}
	if len(panes.text) != 0 || len(panes.keys) != 0 {
		t.Fatalf("delivery to an agent whose CLI exited: want nothing typed, got %v and %v", panes.text, panes.keys)
	}
}

func mustRegistry(t *testing.T, harnesses ...harness.Harness) *harness.Registry {
	t.Helper()
	registry, err := harness.NewRegistry(harnesses...)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}

	return registry
}

// TestReconcileHoldsAPaneOneListingLeftOut pins that a pane missing from a single listing is
// not acted on, since a listing that came back short reads exactly like one that came back
// right and forgetting the pane costs the agent a binding nothing restores.
func TestReconcileHoldsAPaneOneListingLeftOut(t *testing.T) {
	it := tmuxtest.New(t)
	st := newStore(t)
	ctx := context.Background()
	rec := NewReconciler(it.Client(), mustRegistry(t, claude.NewHarness(claude.NewHarnessParams{ProjectsDir: t.TempDir()})), st.Agents)

	it.Spawn("bystander", "sleep 300")
	board := seedBoard(t, it, st, 5)

	for _, agent := range board {
		it.Kill(agent.Name)
	}

	for range listingsBeforePaneGone - 1 {
		if err := rec.Reconcile(ctx); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
	}

	for _, agent := range board {
		held := agentByID(t, st, agent.ID)
		if held.Pane != agent.Pane {
			t.Fatalf("%s: want the pane kept, got %q", agent.Name, held.Pane)
		}
		if held.State != agentstore.StateThinking {
			t.Fatalf("%s: want thinking, got %q", agent.Name, held.State)
		}
		if held.InterruptedAt != nil {
			t.Fatalf("%s: want no interruption recorded, got %v", agent.Name, *held.InterruptedAt)
		}
	}

	if err := rec.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	for _, agent := range board {
		after := agentByID(t, st, agent.ID)
		if after.Pane != "" {
			t.Fatalf("%s: want the pane forgotten, still holds %q", agent.Name, after.Pane)
		}
		if after.State != agentstore.StateIdle {
			t.Fatalf("%s: want idle, got %q", agent.Name, after.State)
		}
		if after.InterruptedAt == nil {
			t.Fatalf("%s: want the unfinished turn recorded as interrupted", agent.Name)
		}
	}
}

// TestReconcileHoldsAPaneWhateverElseTheListingKept pins that the hold is per pane rather
// than measured against the size of the loss, so a listing that dropped a minority of the
// board is held on the same terms as one that dropped all of it.
func TestReconcileHoldsAPaneWhateverElseTheListingKept(t *testing.T) {
	it := tmuxtest.New(t)
	st := newStore(t)
	ctx := context.Background()
	rec := NewReconciler(it.Client(), mustRegistry(t, claude.NewHarness(claude.NewHarnessParams{ProjectsDir: t.TempDir()})), st.Agents)

	board := seedBoard(t, it, st, 8)
	for _, agent := range board[:3] {
		it.Kill(agent.Name)
	}

	if err := rec.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	for _, agent := range board {
		after := agentByID(t, st, agent.ID)
		if after.Pane != agent.Pane {
			t.Fatalf("%s: want the pane kept, got %q", agent.Name, after.Pane)
		}
		if after.InterruptedAt != nil {
			t.Fatalf("%s: want no interruption recorded, got %v", agent.Name, *after.InterruptedAt)
		}
	}
}

// TestReconcileForgetsAPaneThatStaysGone pins that an ordinary exit still settles, the hold
// costing it the passes it takes to tell an exit from a listing that came back short.
func TestReconcileForgetsAPaneThatStaysGone(t *testing.T) {
	it := tmuxtest.New(t)
	st := newStore(t)
	ctx := context.Background()
	rec := NewReconciler(it.Client(), mustRegistry(t, claude.NewHarness(claude.NewHarnessParams{ProjectsDir: t.TempDir()})), st.Agents)

	board := seedBoard(t, it, st, 5)
	gone := board[0]
	it.Kill(gone.Name)

	for range listingsBeforePaneGone {
		if err := rec.Reconcile(ctx); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
	}

	after := agentByID(t, st, gone.ID)
	if after.Pane != "" {
		t.Fatalf("the agent that exited: want the pane forgotten, still holds %q", after.Pane)
	}
	if after.State != agentstore.StateIdle {
		t.Fatalf("the agent that exited: want idle, got %q", after.State)
	}

	for _, agent := range board[1:] {
		still := agentByID(t, st, agent.ID)
		if still.Pane != agent.Pane {
			t.Fatalf("%s: want the pane kept, got %q", agent.Name, still.Pane)
		}
		if still.State != agentstore.StateThinking {
			t.Fatalf("%s: want thinking, got %q", agent.Name, still.State)
		}
	}
}

// TestReconcileReturnsAHoldWhenTheListingComesBack pins that a pane the listing mentions
// again returns what its absence had accrued, so a run of blips never adds up to an exit.
func TestReconcileReturnsAHoldWhenTheListingComesBack(t *testing.T) {
	it := tmuxtest.New(t)
	st := newStore(t)
	ctx := context.Background()
	dropping := &listingDroppingPanes{Client: it.Client()}
	rec := NewReconciler(dropping, mustRegistry(t, claude.NewHarness(claude.NewHarnessParams{ProjectsDir: t.TempDir()})), st.Agents)

	board := seedBoard(t, it, st, 5)

	// Explainer: this last agent sits on a pane that captures nothing, because a tmux degraded
	// enough to drop panes from a listing is the same tmux whose captures come back empty, and
	// an empty capture returns before apply. Every other agent here reaches apply on a present
	// pass, and apply clears the absences along with the rest of the debounce, so without this
	// one the test would pass whether or not a returning listing gives a hold back.
	quietPane := it.Spawn("quiet", `sh -c 'exec cat'`)
	board = append(board, seedRunning(t, st, it.Client(), "quiet", quietPane, agentstore.StateThinking))
	if frame := it.Client().CapturePane(quietPane); frame != "" {
		t.Fatalf("the quiet agent: want a pane that captures nothing, got %q", frame)
	}

	for range listingsBeforePaneGone * 3 {
		dropping.drop = !dropping.drop
		if err := rec.Reconcile(ctx); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
	}

	for _, agent := range board {
		after := agentByID(t, st, agent.ID)
		if after.Pane != agent.Pane {
			t.Fatalf("%s: want the pane kept, got %q", agent.Name, after.Pane)
		}
		if after.InterruptedAt != nil {
			t.Fatalf("%s: want no interruption recorded, got %v", agent.Name, *after.InterruptedAt)
		}
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
