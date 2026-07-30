package agents

import (
	"context"
	"testing"

	agentstore "claude-agents/internal/store/agent"
	"claude-agents/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReconcileSinglePass covers the state a single reconcile pass settles on for
// one agent, given its prior state and what its pane shows. Multi-pass behaviour
// like the idle debounce is covered separately.
func TestReconcileSinglePass(t *testing.T) {
	tests := []struct {
		name      string
		fromState string
		// owned is whether the pane recorded against the agent is still stamped with
		// that agent's id, which is the only thing that counts as it still running.
		owned bool
		frame string
		want  string
	}{
		{
			name:      "a pane that is not the agent's settles thinking to done",
			fromState: agentstore.StateThinking,
			owned:     false,
			want:      agentstore.StateDone,
		},
		{
			name:      "a pane that is not the agent's settles waiting to done",
			fromState: agentstore.StateWaiting,
			owned:     false,
			want:      agentstore.StateDone,
		},
		{
			name:      "a pane that is not the agent's leaves new untouched",
			fromState: agentstore.StateNew,
			owned:     false,
			want:      agentstore.StateNew,
		},
		{
			name:      "a pane that is not the agent's leaves done untouched",
			fromState: agentstore.StateDone,
			owned:     false,
			want:      agentstore.StateDone,
		},
		{
			name:      "empty capture is inconclusive",
			fromState: agentstore.StateThinking,
			owned:     true,
			frame:     "",
			want:      agentstore.StateThinking,
		},
		{
			name:      "idle frame does not demote in one pass",
			fromState: agentstore.StateThinking,
			owned:     true,
			frame:     idleFrame,
			want:      agentstore.StateThinking,
		},
		{
			name:      "idle frame leaves new untouched",
			fromState: agentstore.StateNew,
			owned:     true,
			frame:     idleFrame,
			want:      agentstore.StateNew,
		},
		{
			name:      "idle frame leaves done untouched",
			fromState: agentstore.StateDone,
			owned:     true,
			frame:     idleFrame,
			want:      agentstore.StateDone,
		},
		{
			name:      "waiting frame moves thinking to waiting",
			fromState: agentstore.StateThinking,
			owned:     true,
			frame:     waitFrame,
			want:      agentstore.StateWaiting,
		},
		{
			name:      "work promotes new to thinking",
			fromState: agentstore.StateNew,
			owned:     true,
			frame:     busyFrame,
			want:      agentstore.StateThinking,
		},
		{
			name:      "work promotes done to thinking",
			fromState: agentstore.StateDone,
			owned:     true,
			frame:     busyFrame,
			want:      agentstore.StateThinking,
		},
		{
			name:      "work keeps thinking",
			fromState: agentstore.StateThinking,
			owned:     true,
			frame:     busyFrame,
			want:      agentstore.StateThinking,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			agent := seed(t, st, "agent", "%1", tt.fromState)

			tmux := newFakeTmux()
			if tt.owned {
				tmux.owners["%1"] = agent.ID
			}
			tmux.captures["%1"] = tt.frame

			rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents)
			require.NoError(t, rec.Reconcile(context.Background()))
			assert.Equal(t, tt.want, stateOf(t, st, "agent"))
		})
	}
}

// TestReconcileLeavesTheMark pins who the mark belongs to. The reconciler writes
// what it observes, and a mark is not something it can observe. The user sets it and
// only the user clears it, so a pass that moves an agent's state leaves it standing.
func TestReconcileLeavesTheMark(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	agent := seed(t, st, "agent", "%1", agentstore.StateNew)

	marked, err := st.Agents.SetMark(ctx, agent.ID, xptr.Ptr("●"))
	require.NoError(t, err)
	require.True(t, marked)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.captures["%1"] = busyFrame

	rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents)
	require.NoError(t, rec.Reconcile(ctx))

	after, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, after)
	assert.Equal(t, agentstore.StateThinking, after.State, "the pass wrote a state")
	assert.Equal(t, "●", *after.Mark, "the user's mark survives a state write")
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
			tmux.owners["%1"] = agent.ID

			rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents)
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

// TestReconcileLeavesAnUnknownHarnessAlone covers an agent recorded against a CLI
// this binary does not run, which a removed harness looks like. Nothing here can
// read its pane, so the only honest act is to leave the state it already holds:
// demoting it would report work as finished off a frame nobody interpreted.
func TestReconcileLeavesAnUnknownHarnessAlone(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	require.NoError(t, st.Create(ctx, agentstore.Agent{
		Name:    "orphan",
		Pane:    "%1",
		State:   agentstore.StateThinking,
		Harness: "gemini",
	}))
	agent, err := st.Get(ctx, "orphan")
	require.NoError(t, err)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.captures["%1"] = idleFrame

	rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}, only: []string{testHarnessID}}, st.Agents)
	for range reconcileMisses + 1 {
		require.NoError(t, rec.Reconcile(ctx))
	}

	assert.Equal(t, agentstore.StateThinking, stateOf(t, st, "orphan"))
	assert.Empty(t, rec.misses, "an unread frame is not an idle observation")
}
