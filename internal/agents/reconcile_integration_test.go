package agents

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-agents/internal/harness"
	"claude-agents/internal/harness/claude"
	agentstore "claude-agents/internal/store/agent"
	"claude-agents/internal/tmux"
)

// itmux drives a private tmux server so the reconciler can be exercised against
// a real capture-pane and has-session, not a fake. Each test gets its own socket
// and tears the server down on cleanup, so it never touches the user's tmux.
type itmux struct {
	t      *testing.T
	socket string
}

func newITmux(t *testing.T) *itmux {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

	// A slash-free, per-test socket name keeps servers isolated between tests.
	socket := "cca-" + filepath.Base(t.TempDir())
	it := &itmux{t: t, socket: socket}
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", socket, "kill-server").Run() })
	return it
}

func (it *itmux) tmux(args ...string) string {
	it.t.Helper()
	out, err := exec.Command("tmux", append([]string{"-L", it.socket}, args...)...).Output()
	if err != nil {
		it.t.Fatalf("tmux %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// spawn starts a detached tmux session running command and returns its pane id.
func (it *itmux) spawn(name, command string) string {
	it.t.Helper()
	it.tmux("new-session", "-d", "-s", name, "-c", it.t.TempDir(), command)
	return it.tmux("display-message", "-p", "-t", name, "#{pane_id}")
}

func (it *itmux) kill(name string) { it.t.Helper(); it.tmux("kill-session", "-t", name) }

// waitForFrame polls the pane until its capture contains want, so a test never
// races the shell's first render.
func waitForFrame(t *testing.T, cli *tmux.Client, pane, want string) {
	t.Helper()
	for range 40 {
		if strings.Contains(cli.CapturePane(pane), want) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("pane %s never showed %q; last frame:\n%s", pane, want, cli.CapturePane(pane))
}

func TestReconcileAgainstRealTmux(t *testing.T) {
	it := newITmux(t)
	cli := tmux.NewClient(it.socket)
	st := newStore(t)
	ctx := context.Background()
	rec := NewReconciler(cli, mustRegistry(t, claude.New(t.TempDir())), st.Agents)

	// A busy agent seeded new. The pane keeps a realistic working line, so the
	// reconciler must promote it to thinking.
	busyPane := it.spawn("busy", `sh -c 'while true; do printf "\033[H· Cooking… (3s · down 1k tokens)\n"; sleep 1; done'`)
	seedRunning(t, st, cli, "busy", busyPane, agentstore.StateNew)
	waitForFrame(t, cli, busyPane, "Cooking…")

	// An idle agent renders a Claude-like input box with no timer, the way a real
	// agent sits when interrupted. A bare sleep would capture empty, which the
	// reconciler treats as inconclusive rather than idle.
	idlePane := it.spawn("idle", `sh -c 'printf "\033[H❯ \n-- INSERT --\n"; sleep 300'`)
	seedRunning(t, st, cli, "idle", idlePane, agentstore.StateThinking)
	waitForFrame(t, cli, idlePane, "INSERT")

	// A waiting agent shows an interactive selection with its footer, the general
	// signal for any prompt Claude is blocked on.
	waitPane := it.spawn("wait", `sh -c 'printf "\033[HPick one\n❯ 1. A\n  2. B\nEnter to select · Esc to cancel\n"; sleep 300'`)
	seedRunning(t, st, cli, "wait", waitPane, agentstore.StateThinking)
	waitForFrame(t, cli, waitPane, "Enter to select")

	// A killed agent. Seed it, then tear the session down so nothing answers for the
	// pane at all.
	deadPane := it.spawn("dead", "sleep 300")
	seedRunning(t, st, cli, "dead", deadPane, agentstore.StateThinking)
	it.kill("dead")

	// An agent whose recorded pane is alive but is not its own, which is what a pane
	// id reissued after a server restart looks like. It shows the same working frame
	// as the busy agent, so a reconciler reading the pane rather than its owner would
	// promote this one to thinking off somebody else's work.
	squatPane := it.spawn("squat", `sh -c 'while true; do printf "\033[H· Cooking… (3s · down 1k tokens)\n"; sleep 1; done'`)
	squatter := seed(t, st, "squat", squatPane, agentstore.StateThinking)
	waitForFrame(t, cli, squatPane, "Cooking…")

	for range reconcileMisses {
		if err := rec.Reconcile(ctx); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
	}

	if got := stateOf(t, st, "busy"); got != agentstore.StateThinking {
		t.Fatalf("busy agent: want thinking, got %q", got)
	}
	if got := stateOf(t, st, "idle"); got != agentstore.StateDone {
		t.Fatalf("idle agent: want done, got %q", got)
	}
	if got := stateOf(t, st, "wait"); got != agentstore.StateWaiting {
		t.Fatalf("waiting agent: want waiting, got %q", got)
	}
	if got := stateOf(t, st, "dead"); got != agentstore.StateDone {
		t.Fatalf("dead agent: want done, got %q", got)
	}
	if got := stateOf(t, st, "squat"); got != agentstore.StateDone {
		t.Fatalf("agent holding another pane's id: want done, got %q", got)
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
