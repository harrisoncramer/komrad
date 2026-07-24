package agents

import (
	"context"
	"strings"
	"testing"

	"claude-agents/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// busyFrame is a real Claude Code working line. idleFrame is the input box shown
// when it sits at the prompt. waitFrame is the confirmation dialog. All shaped
// after the live UI.
const (
	busyFrame = "· Cooking… (1m 50s · ↓ 5.4k tokens)\n───────\n❯ \n───────"
	idleFrame = "───────────────\n❯ \n───────────────\n  -- INSERT -- ⏵⏵ bypass permissions on"
	waitFrame = "Teleport anywhere for one day. Where?\n❯ 1. Tokyo\n  2. Patagonia\n\nEnter to select · ↑/↓ to navigate · Esc to cancel"
)

func TestFrameShowsWork(t *testing.T) {
	tests := []struct {
		name  string
		frame string
		want  bool
	}{
		{
			name:  "confirmation prompt is not working",
			frame: waitFrame,
			want:  false,
		},
		{
			name:  "finished summary line is not working",
			frame: "✻ Brewed for 2m 36s",
			want:  false,
		},
		{
			name:  "idle input box is not working",
			frame: idleFrame,
			want:  false,
		},
		{
			name:  "empty frame is not working",
			frame: "",
			want:  false,
		},
		{
			name:  "token-count timer reads as working",
			frame: "· Cooking… (1m 50s · ↓ 5.4k tokens)",
			want:  true,
		},
		{
			name:  "short verb timer reads as working",
			frame: "✽ Cogitating… (12s)",
			want:  true,
		},
		{
			name:  "legacy esc-to-interrupt reads as working",
			frame: "· Working… (3s · esc to interrupt)",
			want:  true,
		},
		{
			name:  "bare ellipsis timer reads as working",
			frame: "…(0s",
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, frameShowsWork(tt.frame))
		})
	}
}

func TestFrameShowsWaiting(t *testing.T) {
	tests := []struct {
		name  string
		frame string
		want  bool
	}{
		{
			name:  "busy frame is not waiting",
			frame: busyFrame,
			want:  false,
		},
		{
			name:  "idle input box is not waiting",
			frame: idleFrame,
			want:  false,
		},
		{
			name:  "empty frame is not waiting",
			frame: "",
			want:  false,
		},
		{
			name:  "multiple-choice menu is waiting",
			frame: waitFrame,
			want:  true,
		},
		{
			name:  "yes/no confirmation is waiting",
			frame: "Do you want to proceed?\n❯ 1. Yes\n  2. No\nEnter to select · Esc to cancel",
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, frameShowsWaiting(tt.frame))
		})
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

	assert.False(t, frameShowsWaiting(frame), "transcript prose must not read as a live waiting prompt")
	assert.False(t, frameShowsWork(frame), "transcript prose must not read as working")
}

// TestReconcileSinglePass covers the state a single reconcile pass settles on for
// one agent, given its prior state and what its pane shows. Multi-pass behaviour
// (the idle debounce) is covered separately.
func TestReconcileSinglePass(t *testing.T) {
	tests := []struct {
		name      string
		fromState string
		alive     bool
		frame     string
		want      string
	}{
		{
			name:      "dead session settles thinking to done",
			fromState: store.StateThinking,
			alive:     false,
			want:      store.StateDone,
		},
		{
			name:      "dead session settles waiting to done",
			fromState: store.StateWaiting,
			alive:     false,
			want:      store.StateDone,
		},
		{
			name:      "dead session leaves new untouched",
			fromState: store.StateNew,
			alive:     false,
			want:      store.StateNew,
		},
		{
			name:      "dead session leaves done untouched",
			fromState: store.StateDone,
			alive:     false,
			want:      store.StateDone,
		},
		{
			name:      "empty capture is inconclusive",
			fromState: store.StateThinking,
			alive:     true,
			frame:     "",
			want:      store.StateThinking,
		},
		{
			name:      "idle frame does not demote in one pass",
			fromState: store.StateThinking,
			alive:     true,
			frame:     idleFrame,
			want:      store.StateThinking,
		},
		{
			name:      "idle frame leaves new untouched",
			fromState: store.StateNew,
			alive:     true,
			frame:     idleFrame,
			want:      store.StateNew,
		},
		{
			name:      "idle frame leaves done untouched",
			fromState: store.StateDone,
			alive:     true,
			frame:     idleFrame,
			want:      store.StateDone,
		},
		{
			name:      "waiting frame moves thinking to waiting",
			fromState: store.StateThinking,
			alive:     true,
			frame:     waitFrame,
			want:      store.StateWaiting,
		},
		{
			name:      "work promotes new to thinking",
			fromState: store.StateNew,
			alive:     true,
			frame:     busyFrame,
			want:      store.StateThinking,
		},
		{
			name:      "work promotes done to thinking",
			fromState: store.StateDone,
			alive:     true,
			frame:     busyFrame,
			want:      store.StateThinking,
		},
		{
			name:      "work keeps thinking",
			fromState: store.StateThinking,
			alive:     true,
			frame:     busyFrame,
			want:      store.StateThinking,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			seed(t, st, "agent", "%1", tt.fromState)

			tmux := newFakeTmux()
			tmux.alive["%1"] = tt.alive
			tmux.captures["%1"] = tt.frame

			rec := NewReconciler(tmux, st)
			require.NoError(t, rec.Reconcile(context.Background()))
			assert.Equal(t, tt.want, stateOf(t, st, "agent"))
		})
	}
}

// TestReconcileIdleDebounce checks a thinking agent whose pane goes idle is only
// demoted after enough consecutive idle passes, so a brief lull between tool
// calls never demotes genuinely-working work.
func TestReconcileIdleDebounce(t *testing.T) {
	st := newStore(t)
	seed(t, st, "stuck", "%1", store.StateThinking)

	tmux := newFakeTmux()
	tmux.alive["%1"] = true
	tmux.captures["%1"] = idleFrame

	rec := NewReconciler(tmux, st)
	ctx := context.Background()

	for i := range reconcileMisses - 1 {
		require.NoError(t, rec.Reconcile(ctx))
		assert.Equal(t, store.StateThinking, stateOf(t, st, "stuck"), "must survive idle pass %d", i)
	}

	require.NoError(t, rec.Reconcile(ctx))
	assert.Equal(t, store.StateDone, stateOf(t, st, "stuck"))
}

// TestReconcileBusyResetsMissCount confirms an intervening busy pass clears the
// accrued idle misses, so a single trailing idle pass cannot demote.
func TestReconcileBusyResetsMissCount(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "flap", "%1", store.StateThinking)

	tmux := newFakeTmux()
	tmux.alive["%1"] = true
	rec := NewReconciler(tmux, st)
	ctx := context.Background()

	tmux.captures["%1"] = idleFrame
	require.NoError(t, rec.Reconcile(ctx))
	tmux.captures["%1"] = busyFrame
	require.NoError(t, rec.Reconcile(ctx))
	tmux.captures["%1"] = idleFrame
	require.NoError(t, rec.Reconcile(ctx))

	assert.Equal(t, store.StateThinking, stateOf(t, st, "flap"))
	assert.Equal(t, 1, rec.misses[agent.ID], "one trailing idle pass after a busy reset")
}

// TestReconcileWaitingThenAnswered covers a confirmation prompt moving an agent
// to waiting, and the prompt clearing letting it settle back to done.
func TestReconcileWaitingThenAnswered(t *testing.T) {
	st := newStore(t)
	seed(t, st, "ask", "%1", store.StateThinking)

	tmux := newFakeTmux()
	tmux.alive["%1"] = true
	tmux.captures["%1"] = waitFrame

	rec := NewReconciler(tmux, st)
	ctx := context.Background()

	require.NoError(t, rec.Reconcile(ctx))
	assert.Equal(t, store.StateWaiting, stateOf(t, st, "ask"))

	tmux.captures["%1"] = idleFrame
	for range reconcileMisses {
		require.NoError(t, rec.Reconcile(ctx))
	}
	assert.Equal(t, store.StateDone, stateOf(t, st, "ask"))
}

// TestReconcileForgetsVanishedAgents confirms an agent's accrued miss count is
// reaped once it leaves the listing, so the map cannot grow without bound.
func TestReconcileForgetsVanishedAgents(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "temp", "%1", store.StateThinking)

	tmux := newFakeTmux()
	tmux.alive["%1"] = true
	tmux.captures["%1"] = idleFrame

	rec := NewReconciler(tmux, st)
	ctx := context.Background()

	require.NoError(t, rec.Reconcile(ctx))
	assert.NotZero(t, rec.misses[agent.ID], "an idle pass should record a miss")

	require.NoError(t, st.SoftDelete(ctx, "temp"))
	require.NoError(t, rec.Reconcile(ctx))
	assert.Empty(t, rec.misses, "miss map should be reaped after the agent vanished")
}
