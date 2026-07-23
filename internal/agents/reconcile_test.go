package agents

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"claude-agents/internal/store"
)

// fakeTmux is a scripted tmux probe: which sessions are alive and what each pane
// currently shows.
type fakeTmux struct {
	alive    map[string]bool
	captures map[string]string
}

func (f *fakeTmux) HasSession(name string) bool    { return f.alive[name] }
func (f *fakeTmux) CapturePane(pane string) string { return f.captures[pane] }

func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "agents.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// seed inserts an agent in a given state and returns it (with its generated id).
func seed(t *testing.T, st *store.Store, name, pane, state string) store.Agent {
	t.Helper()
	ctx := context.Background()
	if err := st.Upsert(ctx, store.Agent{Name: name, Pane: pane, State: state}); err != nil {
		t.Fatalf("seed %q: %v", name, err)
	}
	got, err := st.Get(ctx, name)
	if err != nil || got == nil {
		t.Fatalf("seed get %q: %v", name, err)
	}
	return *got
}

func stateOf(t *testing.T, st *store.Store, name string) string {
	t.Helper()
	got, err := st.Get(context.Background(), name)
	if err != nil || got == nil {
		t.Fatalf("state of %q: %v", name, err)
	}
	return got.State
}

// busyFrame is a real Claude Code working line. idleFrame is the input box shown
// when it sits at the prompt. waitFrame is the confirmation dialog. All shaped
// after the live UI.
const busyFrame = "· Cooking… (1m 50s · ↓ 5.4k tokens)\n───────\n❯ \n───────"
const idleFrame = "───────────────\n❯ \n───────────────\n  -- INSERT -- ⏵⏵ bypass permissions on"
const waitFrame = "Teleport anywhere for one day. Where?\n❯ 1. Tokyo\n  2. Patagonia\n\nEnter to select · ↑/↓ to navigate · Esc to cancel"

func TestFrameShowsWork(t *testing.T) {
	working := []string{
		"· Cooking… (1m 50s · ↓ 5.4k tokens)",
		"✽ Cogitating… (12s)",
		"· Working… (3s · esc to interrupt)",
		"…(0s",
	}
	for _, f := range working {
		if !frameShowsWork(f) {
			t.Fatalf("want working for %q", f)
		}
	}

	idle := []string{
		idleFrame,
		waitFrame, // a confirmation prompt is not "working"
		"✻ Brewed for 2m 36s",
		"❯ \n  -- INSERT --",
		"",
	}
	for _, f := range idle {
		if frameShowsWork(f) {
			t.Fatalf("want idle for %q", f)
		}
	}
}

func TestFrameShowsWaiting(t *testing.T) {
	waiting := []string{
		waitFrame, // a multiple-choice question menu
		"Do you want to proceed?\n❯ 1. Yes\n  2. No\nEnter to select · Esc to cancel",
	}
	for _, f := range waiting {
		if !frameShowsWaiting(f) {
			t.Fatalf("selection prompt should read as waiting: %q", f)
		}
	}
	for _, f := range []string{busyFrame, idleFrame, ""} {
		if frameShowsWaiting(f) {
			t.Fatalf("non-prompt frame read as waiting: %q", f)
		}
	}
}

// TestDetectorsIgnoreTranscriptProse pins the fix for a real false positive:
// text high in the transcript that happens to contain a marker (the assistant
// discussing "Enter to select", "do you want", or a "· Cooking… (1m)" timer)
// must not be read as a live prompt or live work. Only the bottom UI region
// counts, and here it is the idle input box.
func TestDetectorsIgnoreTranscriptProse(t *testing.T) {
	prose := "I match `Enter to select` and `Do you want`, and the timer `· Cooking… (1m 50s)`."
	frame := prose + strings.Repeat("\n", liveRegionLines) + idleFrame

	if frameShowsWaiting(frame) {
		t.Fatal("transcript prose must not read as a live waiting prompt")
	}
	if frameShowsWork(frame) {
		t.Fatal("transcript prose must not read as working")
	}
}

func TestReconcileDeadSessionSettlesDone(t *testing.T) {
	for _, from := range []string{store.StateThinking, store.StateWaiting} {
		st := newStore(t)
		seed(t, st, "gone", "%1", from)
		// No live sessions.
		rec := NewReconciler(&fakeTmux{alive: map[string]bool{}, captures: map[string]string{}}, st)

		if err := rec.Reconcile(context.Background()); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		if got := stateOf(t, st, "gone"); got != store.StateDone {
			t.Fatalf("dead session from %q: want done, got %q", from, got)
		}
	}
}

func TestReconcileThinkingButIdleDemotesAfterMisses(t *testing.T) {
	st := newStore(t)
	seed(t, st, "stuck", "%1", store.StateThinking)
	tmux := &fakeTmux{
		alive:    map[string]bool{"stuck": true},
		captures: map[string]string{"%1": idleFrame},
	}
	rec := NewReconciler(tmux, st)
	ctx := context.Background()

	// It must survive the first reconcileMisses-1 idle passes, so a brief lull
	// between tool calls never demotes a genuinely working agent.
	for i := 0; i < reconcileMisses-1; i++ {
		_ = rec.Reconcile(ctx)
		if got := stateOf(t, st, "stuck"); got != store.StateThinking {
			t.Fatalf("pass %d: want still thinking, got %q", i, got)
		}
	}

	_ = rec.Reconcile(ctx)
	if got := stateOf(t, st, "stuck"); got != store.StateDone {
		t.Fatalf("after %d idle passes: want done, got %q", reconcileMisses, got)
	}
}

func TestReconcileBusyPaneStaysThinking(t *testing.T) {
	st := newStore(t)
	seed(t, st, "working", "%1", store.StateThinking)
	tmux := &fakeTmux{
		alive:    map[string]bool{"working": true},
		captures: map[string]string{"%1": busyFrame},
	}
	rec := NewReconciler(tmux, st)

	for i := 0; i < reconcileMisses+2; i++ {
		_ = rec.Reconcile(context.Background())
	}
	if got := stateOf(t, st, "working"); got != store.StateThinking {
		t.Fatalf("busy pane: want thinking, got %q", got)
	}
}

func TestReconcileBusyResetsMissCount(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "flap", "%1", store.StateThinking)
	tmux := &fakeTmux{alive: map[string]bool{"flap": true}, captures: map[string]string{}}
	rec := NewReconciler(tmux, st)
	ctx := context.Background()

	// idle, then busy, then idle: the busy pass clears the miss count, so a
	// single trailing idle pass must not be enough to demote.
	tmux.captures["%1"] = idleFrame
	_ = rec.Reconcile(ctx)
	tmux.captures["%1"] = busyFrame
	_ = rec.Reconcile(ctx)
	tmux.captures["%1"] = idleFrame
	_ = rec.Reconcile(ctx)

	if got := stateOf(t, st, "flap"); got != store.StateThinking {
		t.Fatalf("want thinking after idle/busy/idle, got %q", got)
	}
	if rec.misses[agent.ID] != 1 {
		t.Fatalf("want miss count reset to 1, got %d", rec.misses[agent.ID])
	}
}

// TestReconcileEmptyCaptureIsInconclusive guards a live agent against a flaky
// pane read: an empty capture must never be counted as idle, or a working agent
// could be demoted while tmux momentarily fails to return its frame.
func TestReconcileEmptyCaptureIsInconclusive(t *testing.T) {
	st := newStore(t)
	seed(t, st, "flaky", "%1", store.StateThinking)
	// alive, but the pane capture comes back empty every time.
	tmux := &fakeTmux{alive: map[string]bool{"flaky": true}, captures: map[string]string{}}
	rec := NewReconciler(tmux, st)

	for range reconcileMisses + 3 {
		_ = rec.Reconcile(context.Background())
	}
	if got := stateOf(t, st, "flaky"); got != store.StateThinking {
		t.Fatalf("empty capture must not demote; got %q", got)
	}
}

// TestReconcilePromotesOnWork covers the reconciler now being the thing that
// starts an agent thinking, replacing the old prompt hook: a new or finished
// agent whose pane shows work is moved to thinking on the next pass.
func TestReconcilePromotesOnWork(t *testing.T) {
	for _, from := range []string{store.StateNew, store.StateDone} {
		st := newStore(t)
		seed(t, st, "revive", "%1", from)
		tmux := &fakeTmux{
			alive:    map[string]bool{"revive": true},
			captures: map[string]string{"%1": busyFrame},
		}
		rec := NewReconciler(tmux, st)

		_ = rec.Reconcile(context.Background())
		if got := stateOf(t, st, "revive"); got != store.StateThinking {
			t.Fatalf("from %q with work on pane: want thinking, got %q", from, got)
		}
	}
}

// TestReconcileDetectsWaiting covers the confirmation prompt moving an agent to
// waiting, and the prompt clearing (answered) letting it settle back to done.
func TestReconcileDetectsWaiting(t *testing.T) {
	st := newStore(t)
	seed(t, st, "ask", "%1", store.StateThinking)
	tmux := &fakeTmux{
		alive:    map[string]bool{"ask": true},
		captures: map[string]string{"%1": waitFrame},
	}
	rec := NewReconciler(tmux, st)
	ctx := context.Background()

	_ = rec.Reconcile(ctx)
	if got := stateOf(t, st, "ask"); got != store.StateWaiting {
		t.Fatalf("confirmation prompt: want waiting, got %q", got)
	}

	// Prompt answered, agent idle: settles to done after the debounce.
	tmux.captures["%1"] = idleFrame
	for range reconcileMisses {
		_ = rec.Reconcile(ctx)
	}
	if got := stateOf(t, st, "ask"); got != store.StateDone {
		t.Fatalf("after the prompt cleared: want done, got %q", got)
	}
}

// TestReconcileLeavesIdleNewAndDone confirms the reconciler does not disturb an
// idle agent that has not started (new) or has finished (done).
func TestReconcileLeavesIdleNewAndDone(t *testing.T) {
	st := newStore(t)
	seed(t, st, "fresh", "%2", store.StateNew)
	seed(t, st, "fin", "%3", store.StateDone)
	tmux := &fakeTmux{
		alive:    map[string]bool{"fresh": true, "fin": true},
		captures: map[string]string{"%2": idleFrame, "%3": idleFrame},
	}
	rec := NewReconciler(tmux, st)

	for i := 0; i < reconcileMisses+1; i++ {
		_ = rec.Reconcile(context.Background())
	}

	if got := stateOf(t, st, "fresh"); got != store.StateNew {
		t.Fatalf("idle new should be untouched, got %q", got)
	}
	if got := stateOf(t, st, "fin"); got != store.StateDone {
		t.Fatalf("idle done should be untouched, got %q", got)
	}
}

func TestReconcileForgetsVanishedAgents(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "temp", "%1", store.StateThinking)
	tmux := &fakeTmux{
		alive:    map[string]bool{"temp": true},
		captures: map[string]string{"%1": idleFrame},
	}
	rec := NewReconciler(tmux, st)
	ctx := context.Background()

	_ = rec.Reconcile(ctx)
	if rec.misses[agent.ID] == 0 {
		t.Fatal("expected a recorded miss for the idle agent")
	}

	// The agent leaves the listing, so its miss entry must be reaped.
	_ = st.SoftDelete(ctx, "temp")
	_ = rec.Reconcile(ctx)
	if len(rec.misses) != 0 {
		t.Fatalf("miss map should be empty after the agent vanished, got %d entries", len(rec.misses))
	}
}
