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
	rec := NewReconciler(cli, mustRegistry(t, claude.NewHarness(claude.NewHarnessParams{ProjectsDir: t.TempDir()})), st.Agents)

	// A busy agent seeded idle. The pane keeps a realistic working line, so the
	// reconciler must promote it to thinking. Every pane standing in for a live agent
	// hands itself to cat once it has rendered, because a pane whose foreground program
	// is the shell is how weave reads a CLI that has exited.
	busyPane := it.Spawn("busy", `sh -c 'printf "\033[H· Cooking… (3s · down 1k tokens)\n"; exec cat'`)
	seedRunning(t, st, cli, "busy", busyPane, agentstore.StateIdle)
	it.WaitForFrame(busyPane, "Cooking…")

	// An idle agent renders a Claude-like input box with no timer, the way a real
	// agent sits when interrupted. A bare sleep would capture empty, which the
	// reconciler treats as inconclusive rather than idle.
	idlePane := it.Spawn("idle", `sh -c 'printf "\033[H❯ \n-- INSERT --\n"; exec cat'`)
	seedRunning(t, st, cli, "idle", idlePane, agentstore.StateThinking)
	it.WaitForFrame(idlePane, "INSERT")

	// A waiting agent shows an interactive selection with its footer, the general
	// signal for any prompt Claude is blocked on.
	waitPane := it.Spawn("wait", `sh -c 'printf "\033[HPick one\n❯ 1. A\n  2. B\nEnter to select · Esc to cancel\n"; exec cat'`)
	seedRunning(t, st, cli, "wait", waitPane, agentstore.StateThinking)
	it.WaitForFrame(waitPane, "Enter to select")

	// A killed agent. Seed it, then tear the session down so nothing answers for the
	// pane at all.
	deadPane := it.Spawn("dead", "sleep 300")
	seedRunning(t, st, cli, "dead", deadPane, agentstore.StateThinking)
	it.Kill("dead")

	// An agent whose CLI exited. Weave types the launch command into a shell, so the
	// shell outlives the CLI and holds a pane that is still alive and still stamped for
	// this agent. Nothing about the frame says the CLI is gone: the last thing it drew
	// is still on screen above the prompt.
	exitedPane := it.Spawn("exited", `sh -c 'printf "\033[H❯ ready\n"; exec sh'`)
	exited := seedRunning(t, st, cli, "exited", exitedPane, agentstore.StateThinking)
	it.WaitForFrame(exitedPane, "ready")

	// An agent whose recorded pane is alive but is not its own, which is what a pane
	// id reissued after a server restart looks like. It shows the same working frame
	// as the busy agent, so a reconciler reading the pane rather than its owner would
	// promote this one to thinking off somebody else's work.
	squatPane := it.Spawn("squat", `sh -c 'printf "\033[H· Cooking… (3s · down 1k tokens)\n"; exec cat'`)
	squatter := seed(t, st, "squat", squatPane, agentstore.StateThinking)
	it.WaitForFrame(squatPane, "Cooking…")

	for range reconcileMisses {
		if err := rec.Reconcile(ctx); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
	}

	// A pane back at a shell is given a while before its CLI is taken for gone, so that a
	// launch is not read as an exit. The clock moves rather than the test waiting.
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

	// Forgetting the pane is what the rest falls out of: liveness is a pane lookup, so
	// an agent with no pane reads as not running and can be resumed again.
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

// TestExitedCLIStopsBeingANudgeTarget covers what getting liveness wrong costs. Mail is
// delivered by typing at an agent that has gone quiet, so an agent whose CLI exited looks
// exactly like one waiting to be told, and the line goes into the shell that outlived it.
// The claim is spent either way, so that mail is not retried for this idle stretch.
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
		PaneReader:    NewAgentService(NewAgentServiceParams{Tmux: cli}),
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

// mustRegistry indexes the harnesses a test drives, which is what the loops get
// an agent's own harness through.
func mustRegistry(t *testing.T, harnesses ...harness.Harness) *harness.Registry {
	t.Helper()
	registry, err := harness.NewRegistry(harnesses...)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}

	return registry
}
