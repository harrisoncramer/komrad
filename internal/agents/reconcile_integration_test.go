package agents

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-agents/internal/store"
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

// spawn starts a detached session running command and returns its pane id.
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
	rec := NewReconciler(cli, st)

	// A busy agent: the pane keeps a realistic working line on screen, so the
	// detector is exercised against the same shape the real UI renders.
	busyPane := it.spawn("busy", `sh -c 'while true; do printf "\033[H· Cooking… (3s · down 1k tokens)\n"; sleep 1; done'`)
	seed(t, st, "busy", busyPane, store.StateThinking)
	waitForFrame(t, cli, busyPane, "Cooking…")

	// An idle agent: it renders a Claude-like input box with no timer, the way a
	// real agent sits when interrupted. A bare sleep would capture empty, which
	// the reconciler treats as inconclusive rather than idle.
	idlePane := it.spawn("idle", `sh -c 'printf "\033[H❯ \n-- INSERT --\n"; sleep 300'`)
	seed(t, st, "idle", idlePane, store.StateThinking)
	waitForFrame(t, cli, idlePane, "INSERT")

	// A killed agent: seed it, then tear the session down so has-session is false.
	deadPane := it.spawn("dead", "sleep 300")
	seed(t, st, "dead", deadPane, store.StateThinking)
	it.kill("dead")

	for range reconcileMisses {
		if err := rec.Reconcile(ctx); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
	}

	if got := stateOf(t, st, "busy"); got != store.StateThinking {
		t.Fatalf("busy agent: want thinking, got %q", got)
	}
	if got := stateOf(t, st, "idle"); got != store.StateDone {
		t.Fatalf("idle agent: want done, got %q", got)
	}
	if got := stateOf(t, st, "dead"); got != store.StateDone {
		t.Fatalf("dead agent: want done, got %q", got)
	}
}
