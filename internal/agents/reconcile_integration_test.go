package agents

import (
	"context"
	"fmt"
	"testing"

	"weave/internal/harness"
	"weave/internal/harness/claude"
	agentstore "weave/internal/store/agent"
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

	idlePane := it.Spawn("idle", `sh -c 'printf "\033[H❯ \n-- INSERT --\n"; exec cat'`)
	seedRunning(t, st, cli, "idle", idlePane, agentstore.StateThinking)
	it.WaitForFrame(idlePane, "INSERT")

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

// TestReconcileHoldsAListingThatLostTheBoard pins that a listing missing most of the panes is
// treated as tmux answering badly rather than as that many agents exiting at once, since
// acting on it takes the pane off every row while the panes themselves keep running.
func TestReconcileHoldsAListingThatLostTheBoard(t *testing.T) {
	it := tmuxtest.New(t)
	st := newStore(t)
	ctx := context.Background()
	rec := NewReconciler(it.Client(), mustRegistry(t, claude.NewHarness(claude.NewHarnessParams{ProjectsDir: t.TempDir()})), st.Agents)

	it.Spawn("bystander", "sleep 300")
	board := seedBoard(t, it, st, 5)

	for _, agent := range board {
		it.Kill(agent.Name)
	}

	for range paneLossHoldLimit {
		if err := rec.Reconcile(ctx); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
	}

	for _, agent := range board {
		after := agentByID(t, st, agent.ID)
		if after.Pane != agent.Pane {
			t.Fatalf("%s: want the pane kept, got %q", agent.Name, after.Pane)
		}
		if after.State != agentstore.StateThinking {
			t.Fatalf("%s: want thinking, got %q", agent.Name, after.State)
		}
		if after.InterruptedAt != nil {
			t.Fatalf("%s: want no interruption recorded, got %v", agent.Name, *after.InterruptedAt)
		}
	}
}

// TestReconcileBelievesTheLossOnceTheHoldRunsOut pins the other side of that: a tmux server
// that really did restart keeps answering the same way, and the board has to follow it.
func TestReconcileBelievesTheLossOnceTheHoldRunsOut(t *testing.T) {
	it := tmuxtest.New(t)
	st := newStore(t)
	ctx := context.Background()
	rec := NewReconciler(it.Client(), mustRegistry(t, claude.NewHarness(claude.NewHarnessParams{ProjectsDir: t.TempDir()})), st.Agents)

	it.Spawn("bystander", "sleep 300")
	board := seedBoard(t, it, st, 5)

	for _, agent := range board {
		it.Kill(agent.Name)
	}

	for range paneLossHoldLimit + 1 {
		if err := rec.Reconcile(ctx); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
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

// TestReconcileForgetsOneAgentThatExits pins that the guard costs an ordinary exit nothing,
// which is the case it has to stay out of the way of.
func TestReconcileForgetsOneAgentThatExits(t *testing.T) {
	it := tmuxtest.New(t)
	st := newStore(t)
	ctx := context.Background()
	rec := NewReconciler(it.Client(), mustRegistry(t, claude.NewHarness(claude.NewHarnessParams{ProjectsDir: t.TempDir()})), st.Agents)

	board := seedBoard(t, it, st, 5)
	gone := board[0]
	it.Kill(gone.Name)

	if err := rec.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
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

// seedBoard spawns count agents that are all working, which is the board a lost listing has
// the most to take from.
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
