package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"weave/internal/harness"
	"weave/internal/harness/claude"
	"weave/internal/harness/codex"
	agentstore "weave/internal/store/agent"
	"weave/internal/tmux"
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

// seedAgent inserts an agent running the named harness and stamps its pane, which is
// what a launched agent looks like. The harness matters here because the two read an
// agent through different things: codex through the pane, claude through the status
// files Claude Code writes about its own processes.
func seedAgent(t *testing.T, st *testStore, cli *tmux.Client, agent agentstore.Agent) {
	t.Helper()
	ctx := context.Background()
	if err := st.Create(ctx, agent); err != nil {
		t.Fatalf("create %s: %v", agent.Name, err)
	}

	got, err := st.Get(ctx, agent.Name)
	if err != nil || got == nil {
		t.Fatalf("get %s: %v", agent.Name, err)
	}
	if err := cli.SetPaneOwner(agent.Pane, got.ID); err != nil {
		t.Fatalf("stamp %s: %v", agent.Name, err)
	}
}

// writeSessionStatus puts a Claude status file in dir for a conversation, under this
// test's own pid so the liveness check the reader does against it passes.
func writeSessionStatus(t *testing.T, dir, sessionID, status string) {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"pid":       os.Getpid(),
		"sessionId": sessionID,
		"status":    status,
	})
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.json", os.Getpid())), data, 0o600); err != nil {
		t.Fatalf("write status: %v", err)
	}
}

func TestReconcileAgainstRealTmux(t *testing.T) {
	it := newITmux(t)
	cli := tmux.NewClient(it.socket)
	st := newStore(t)
	ctx := context.Background()

	sessionsDir := t.TempDir()
	registry := mustRegistry(t,
		claude.New(claude.Options{ProjectsDir: t.TempDir(), SessionsDir: sessionsDir}),
		codex.New(codex.Options{SessionsDir: t.TempDir(), ConfigPath: filepath.Join(t.TempDir(), "config.toml")}),
	)
	rec := NewReconciler(cli, registry, st.Agents)

	// A busy agent seeded idle. The pane keeps a realistic working line, so the
	// reconciler must promote it to thinking.
	busyPane := it.spawn("busy", `sh -c 'while true; do printf "\033[H• Working (3s • esc to interrupt)\n"; sleep 1; done'`)
	seedAgent(t, st, cli, agentstore.Agent{Name: "busy", Pane: busyPane, State: agentstore.StateIdle, Harness: "codex"})
	waitForFrame(t, cli, busyPane, "Working")

	// An idle agent renders an input box with no working line, the way a real agent
	// sits when interrupted. A bare sleep would capture empty, which the reconciler
	// treats as inconclusive rather than idle.
	idlePane := it.spawn("idle", `sh -c 'printf "\033[H› \nsummarize recent commits\n"; sleep 300'`)
	seedAgent(t, st, cli, agentstore.Agent{Name: "idle", Pane: idlePane, State: agentstore.StateThinking, Harness: "codex"})
	waitForFrame(t, cli, idlePane, "summarize")

	// A waiting agent shows an approval with its footer, the general signal for any
	// prompt Codex is blocked on.
	waitPane := it.spawn("wait", `sh -c 'printf "\033[HRun this?\n› 1. Yes\n  2. No\nPress enter to confirm or esc to cancel\n"; sleep 300'`)
	seedAgent(t, st, cli, agentstore.Agent{Name: "wait", Pane: waitPane, State: agentstore.StateThinking, Harness: "codex"})
	waitForFrame(t, cli, waitPane, "Press enter to confirm")

	// A Claude agent, which is read through the status file rather than the pane. The
	// pane shows the input box an interrupted agent leaves behind while the file says
	// the session is at a prompt, so only a reconciler ignoring the pane gets this one
	// right.
	claudePane := it.spawn("claude", `sh -c 'printf "\033[H❯ \n-- INSERT --\n"; sleep 300'`)
	seedAgent(t, st, cli, agentstore.Agent{
		Name:      "claude",
		Pane:      claudePane,
		State:     agentstore.StateThinking,
		Harness:   "claude",
		SessionID: "live-session",
	})
	writeSessionStatus(t, sessionsDir, "live-session", "waiting")
	waitForFrame(t, cli, claudePane, "INSERT")

	// A Claude agent no live process holds, which is what one whose CLI died under a
	// pane that outlived it looks like. Nothing writes a status for it, so it settles
	// idle without anything reading the pane.
	goneePane := it.spawn("gone", `sh -c 'printf "\033[H❯ \n-- INSERT --\n"; sleep 300'`)
	seedAgent(t, st, cli, agentstore.Agent{
		Name:      "gone",
		Pane:      goneePane,
		State:     agentstore.StateThinking,
		Harness:   "claude",
		SessionID: "no-such-session",
	})

	// A killed agent. Seed it, then tear the session down so nothing answers for the
	// pane at all.
	deadPane := it.spawn("dead", "sleep 300")
	seedAgent(t, st, cli, agentstore.Agent{Name: "dead", Pane: deadPane, State: agentstore.StateThinking, Harness: "codex"})
	it.kill("dead")

	// An agent whose recorded pane is alive but is not its own, which is what a pane
	// id reissued after a server restart looks like. It shows the same working frame
	// as the busy agent, so a reconciler reading the pane rather than its owner would
	// promote this one to thinking off somebody else's work.
	squatPane := it.spawn("squat", `sh -c 'while true; do printf "\033[H• Working (3s • esc to interrupt)\n"; sleep 1; done'`)
	squatter := seed(t, st, "squat", squatPane, agentstore.StateThinking)
	waitForFrame(t, cli, squatPane, "Working")

	for range reconcileMisses {
		if err := rec.Reconcile(ctx); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
	}

	for _, want := range []struct{ agent, state string }{
		{"busy", agentstore.StateThinking},
		{"idle", agentstore.StateIdle},
		{"wait", agentstore.StateWaiting},
		{"claude", agentstore.StateWaiting},
		{"gone", agentstore.StateIdle},
		{"dead", agentstore.StateIdle},
		{"squat", agentstore.StateIdle},
	} {
		if got := stateOf(t, st, want.agent); got != want.state {
			t.Fatalf("%s agent: want %q, got %q", want.agent, want.state, got)
		}
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
