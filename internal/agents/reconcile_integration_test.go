package agents

import (
	"context"
	"testing"

	"weave/internal/harness"
	"weave/internal/harness/claude"
	agentstore "weave/internal/store/agent"
	"weave/internal/tmuxtest"
)

func TestReconcileAgainstRealTmux(t *testing.T) {
	it := tmuxtest.New(t)
	cli := it.Client()
	st := newStore(t)
	ctx := context.Background()
	rec := NewReconciler(cli, mustRegistry(t, claude.New(t.TempDir())), st.Agents)

	// A busy agent seeded idle. The pane keeps a realistic working line, so the
	// reconciler must promote it to thinking.
	busyPane := it.Spawn("busy", `sh -c 'while true; do printf "\033[H· Cooking… (3s · down 1k tokens)\n"; sleep 1; done'`)
	seedRunning(t, st, cli, "busy", busyPane, agentstore.StateIdle)
	it.WaitForFrame(busyPane, "Cooking…")

	// An idle agent renders a Claude-like input box with no timer, the way a real
	// agent sits when interrupted. A bare sleep would capture empty, which the
	// reconciler treats as inconclusive rather than idle.
	idlePane := it.Spawn("idle", `sh -c 'printf "\033[H❯ \n-- INSERT --\n"; sleep 300'`)
	seedRunning(t, st, cli, "idle", idlePane, agentstore.StateThinking)
	it.WaitForFrame(idlePane, "INSERT")

	// A waiting agent shows an interactive selection with its footer, the general
	// signal for any prompt Claude is blocked on.
	waitPane := it.Spawn("wait", `sh -c 'printf "\033[HPick one\n❯ 1. A\n  2. B\nEnter to select · Esc to cancel\n"; sleep 300'`)
	seedRunning(t, st, cli, "wait", waitPane, agentstore.StateThinking)
	it.WaitForFrame(waitPane, "Enter to select")

	// A killed agent. Seed it, then tear the session down so nothing answers for the
	// pane at all.
	deadPane := it.Spawn("dead", "sleep 300")
	seedRunning(t, st, cli, "dead", deadPane, agentstore.StateThinking)
	it.Kill("dead")

	// An agent whose recorded pane is alive but is not its own, which is what a pane
	// id reissued after a server restart looks like. It shows the same working frame
	// as the busy agent, so a reconciler reading the pane rather than its owner would
	// promote this one to thinking off somebody else's work.
	squatPane := it.Spawn("squat", `sh -c 'while true; do printf "\033[H· Cooking… (3s · down 1k tokens)\n"; sleep 1; done'`)
	squatter := seed(t, st, "squat", squatPane, agentstore.StateThinking)
	it.WaitForFrame(squatPane, "Cooking…")

	for range reconcileMisses {
		if err := rec.Reconcile(ctx); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
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
}

// mustRegistry indexes the harnesses a test drives, which is what the loops resolve
// an agent's own harness through.
func mustRegistry(t *testing.T, harnesses ...harness.Harness) *harness.Registry {
	t.Helper()
	registry, err := harness.NewRegistry(harnesses...)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}

	return registry
}
