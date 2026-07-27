package agents

import (
	"context"
	"strings"
	"testing"

	agentstore "claude-agents/internal/store/agent"
	"claude-agents/lib/xptr"

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

// toolBlock is a plan or todo list rendered below the status line and above the
// input box, which pushes the status line a dozen lines up from the bottom.
const toolBlock = "\n" +
	"  ⎿  ◼ Schema + migration + sqlc for workflows\n" +
	"     ◻ Store layer: Workflow type + WorkflowID + CRUD\n" +
	"     ◻ Panel: workflow scope, picker, move, back-nav\n" +
	"      … +1 pending\n\n" +
	"────────────────\n❯ \n────────────────\n   ~/dir  main\n  -- INSERT --"

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
			name:  "a tool-result line rather than the spinner",
			frame: "⎿ Found… (3 files)",
			want:  false,
		},
		{
			name:  "prose that happens to end in an ellipsis",
			frame: "wait… (2 more)",
			want:  false,
		},
		{
			name:  "the input prompt echoing typed text",
			frame: "❯ my message ends in…",
			want:  false,
		},
		{
			name:  "a box rule",
			frame: "───────────────",
			want:  false,
		},
		{
			name:  "a verb with no timer yet, which is every turn's first frame",
			frame: "✽ Beboppin'…",
			want:  true,
		},
		{
			name:  "a braille spinner",
			frame: "⠋ Thinking…",
			want:  true,
		},
		{
			name:  "a status line above a tool block, with a timer",
			frame: "✢ Building schema, migration, sqlc… (4m 18s · ↓ 15.5k tokens)" + toolBlock,
			want:  true,
		},
		{
			name:  "a multi-word verb above a tool block, no timer yet",
			frame: "✢ Building schema, migration, sqlc…" + toolBlock,
			want:  true,
		},
		{
			// Captured with -e, so the verb is split across colour runs and codes sit
			// between the ellipsis and the timer. Detection strips them or matches
			// nothing.
			name:  "a line shredded by ANSI colour codes",
			frame: "\x1b[38;5;174m✽\x1b[39m \x1b[38;5;174mFl\x1b[38;5;216mamb\x1b[38;5;174méing…\x1b[39m \x1b[38;5;246m(3m 30s · ↓\x1b[39m \x1b[38;5;246m11.6k tokens)\x1b[39m",
			want:  true,
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

// TestDetectorsIgnoreTranscriptProse checks that transcript text carrying a
// marker phrase is not read as a live prompt or live work. Only the bottom of
// the frame counts, and in both cases below it is an idle input box.
func TestDetectorsIgnoreTranscriptProse(t *testing.T) {
	tests := []struct {
		name  string
		frame string
	}{
		{
			name:  "prose well above the live region",
			frame: "I match `Enter to select` and `Do you want`, and the timer `· Cooking… (1m 50s)`." + strings.Repeat("\n", liveRegionLines) + idleFrame,
		},
		{
			// Inside the wide work window but outside the tight waiting one, which is
			// why the two windows are sized differently.
			name:  "waiting phrases a dozen lines up",
			frame: `I split it so "esc to cancel" and "Enter to select" in a message do not read as a prompt.` + "\n\n\n\n\n\n\n\n\n\n───────\n❯ \n───────\n  -- INSERT --",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.False(t, frameShowsWaiting(tt.frame), "prose must not read as a live prompt")
			assert.False(t, frameShowsWork(tt.frame), "prose must not read as working")
		})
	}
}

// TestReconcileSinglePass covers the state a single reconcile pass settles on for
// one agent, given its prior state and what its pane shows. Multi-pass behaviour
// like the idle debounce is covered separately.
func TestReconcileSinglePass(t *testing.T) {
	tests := []struct {
		name      string
		fromState string
		alive     bool
		frame     string
		want      string
	}{
		{
			name:      "dead pane settles thinking to done",
			fromState: agentstore.StateThinking,
			alive:     false,
			want:      agentstore.StateDone,
		},
		{
			name:      "dead pane settles waiting to done",
			fromState: agentstore.StateWaiting,
			alive:     false,
			want:      agentstore.StateDone,
		},
		{
			name:      "dead pane leaves new untouched",
			fromState: agentstore.StateNew,
			alive:     false,
			want:      agentstore.StateNew,
		},
		{
			name:      "dead pane leaves done untouched",
			fromState: agentstore.StateDone,
			alive:     false,
			want:      agentstore.StateDone,
		},
		{
			name:      "empty capture is inconclusive",
			fromState: agentstore.StateThinking,
			alive:     true,
			frame:     "",
			want:      agentstore.StateThinking,
		},
		{
			name:      "idle frame does not demote in one pass",
			fromState: agentstore.StateThinking,
			alive:     true,
			frame:     idleFrame,
			want:      agentstore.StateThinking,
		},
		{
			name:      "idle frame leaves new untouched",
			fromState: agentstore.StateNew,
			alive:     true,
			frame:     idleFrame,
			want:      agentstore.StateNew,
		},
		{
			name:      "idle frame leaves done untouched",
			fromState: agentstore.StateDone,
			alive:     true,
			frame:     idleFrame,
			want:      agentstore.StateDone,
		},
		{
			name:      "waiting frame moves thinking to waiting",
			fromState: agentstore.StateThinking,
			alive:     true,
			frame:     waitFrame,
			want:      agentstore.StateWaiting,
		},
		{
			name:      "work promotes new to thinking",
			fromState: agentstore.StateNew,
			alive:     true,
			frame:     busyFrame,
			want:      agentstore.StateThinking,
		},
		{
			name:      "work promotes done to thinking",
			fromState: agentstore.StateDone,
			alive:     true,
			frame:     busyFrame,
			want:      agentstore.StateThinking,
		},
		{
			name:      "work keeps thinking",
			fromState: agentstore.StateThinking,
			alive:     true,
			frame:     busyFrame,
			want:      agentstore.StateThinking,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			seed(t, st, "agent", "%1", tt.fromState)

			tmux := newFakeTmux()
			tmux.alive["%1"] = tt.alive
			tmux.captures["%1"] = tt.frame

			rec := NewReconciler(tmux, st.Agents)
			require.NoError(t, rec.Reconcile(context.Background()))
			assert.Equal(t, tt.want, stateOf(t, st, "agent"))
		})
	}
}

// TestReconcileOverPasses covers what only shows up across several passes, the
// idle debounce that keeps a lull between tool calls from demoting live work,
// and the miss count that debounce is built on.
func TestReconcileOverPasses(t *testing.T) {
	tests := []struct {
		name string
		// passes is the frame the pane shows on each reconcile pass, one entry per
		// pass.
		passes []string
		// archiveBeforeLast removes the agent from the listing before the final
		// pass, standing in for one deleted while the reconciler is running.
		archiveBeforeLast bool
		// wantAfterFirst and wantBeforeLast, when set, are the states expected after
		// the first pass and after every pass but the last.
		wantAfterFirst string
		wantBeforeLast string
		want           string
		// wantMisses, when set, is the miss count the agent should be left holding.
		wantMisses *int
	}{
		{
			name:           "an idle pane only demotes once the misses run out",
			passes:         repeatFrame(idleFrame, reconcileMisses),
			wantBeforeLast: agentstore.StateThinking,
			want:           agentstore.StateDone,
		},
		{
			name:       "a busy pass clears the accrued misses, so a trailing idle one cannot demote",
			passes:     []string{idleFrame, busyFrame, idleFrame},
			want:       agentstore.StateThinking,
			wantMisses: xptr.Ptr(1),
		},
		{
			name:           "a prompt moves it to waiting, and answering lets it settle to done",
			passes:         append([]string{waitFrame}, repeatFrame(idleFrame, reconcileMisses)...),
			wantAfterFirst: agentstore.StateWaiting,
			want:           agentstore.StateDone,
		},
		{
			name:              "an agent that leaves the listing has its misses reaped",
			passes:            []string{idleFrame, idleFrame},
			archiveBeforeLast: true,
			wantMisses:        xptr.Ptr(0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

			tmux := newFakeTmux()
			tmux.alive["%1"] = true

			rec := NewReconciler(tmux, st.Agents)
			ctx := context.Background()

			for i, frame := range tt.passes {
				last := i == len(tt.passes)-1
				if last && tt.archiveBeforeLast {
					require.NoError(t, st.SoftDelete(ctx, "agent"))
					assert.NotZero(t, rec.misses[agent.ID], "the earlier idle pass should have recorded a miss")
				}

				tmux.captures["%1"] = frame
				require.NoError(t, rec.Reconcile(ctx))

				if tt.archiveBeforeLast && last {
					break
				}
				if i == 0 && tt.wantAfterFirst != "" {
					assert.Equal(t, tt.wantAfterFirst, stateOf(t, st, "agent"))
				}
				if !last && tt.wantBeforeLast != "" {
					assert.Equal(t, tt.wantBeforeLast, stateOf(t, st, "agent"), "must survive pass %d", i)
				}
			}

			if !tt.archiveBeforeLast {
				assert.Equal(t, tt.want, stateOf(t, st, "agent"))
			} else {
				assert.Empty(t, rec.misses, "the miss map is reaped once the agent is gone")
			}

			if tt.wantMisses != nil {
				assert.Equal(t, *tt.wantMisses, rec.misses[agent.ID])
			}
		})
	}
}

// repeatFrame is n passes all showing the same frame, for the cases that only
// care how many consecutive passes it takes to settle.
func repeatFrame(frame string, n int) []string {
	frames := make([]string, n)
	for i := range frames {
		frames[i] = frame
	}

	return frames
}
