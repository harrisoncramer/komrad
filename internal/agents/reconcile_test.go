package agents

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"weave/internal/harness"
	"weave/internal/harness/pane"
	agentstore "weave/internal/store/agent"
	"weave/internal/tmux"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReconcileSinglePass covers the state one pass settles on for an agent, given its prior
// state and what its pane shows.
func TestReconcileSinglePass(t *testing.T) {
	tests := []struct {
		name      string
		fromState agentstore.State
		owned     bool
		command   string
		frame     string
		want      agentstore.State
	}{
		{
			name:      "a pane that is not the agent's settles thinking to idle",
			fromState: agentstore.StateThinking,
			owned:     false,
			want:      agentstore.StateIdle,
		},
		{
			name:      "a pane that is not the agent's settles waiting to idle",
			fromState: agentstore.StateWaiting,
			owned:     false,
			want:      agentstore.StateIdle,
		},
		{
			name:      "a pane that is not the agent's leaves idle untouched",
			fromState: agentstore.StateIdle,
			owned:     false,
			want:      agentstore.StateIdle,
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
			name:      "idle frame leaves idle untouched",
			fromState: agentstore.StateIdle,
			owned:     true,
			frame:     idleFrame,
			want:      agentstore.StateIdle,
		},
		{
			name:      "waiting frame moves thinking to waiting",
			fromState: agentstore.StateThinking,
			owned:     true,
			frame:     waitFrame,
			want:      agentstore.StateWaiting,
		},
		{
			name:      "work promotes idle to thinking",
			fromState: agentstore.StateIdle,
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
		{
			name:      "a pane back at its shell does not demote in one pass, since a launch looks the same",
			fromState: agentstore.StateThinking,
			owned:     true,
			command:   "zsh",
			frame:     busyFrame,
			want:      agentstore.StateThinking,
		},
		{
			name:      "a foreground program weave does not recognise is taken for the CLI",
			fromState: agentstore.StateIdle,
			owned:     true,
			command:   "2.1.241",
			frame:     busyFrame,
			want:      agentstore.StateThinking,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			agent := seed(t, st, "agent", "%1", tt.fromState)

			tmux := newFakeTmux()
			tmux.owners["%1"] = ""
			if tt.owned {
				tmux.owners["%1"] = agent.ID
			}
			tmux.commands["%1"] = tt.command
			tmux.captures["%1"] = tt.frame

			rec := NewReconciler(NewReconcilerParams{
				Panes:    tmux,
				Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
				States:   st.Agents,
			})
			require.NoError(t, rec.Reconcile(t.Context()))
			assert.Equal(t, tt.want, stateOf(t, st, "agent"))
		})
	}
}

// TestReconcileRecordsTheFrameItRead covers the recording, which exists so a reading nobody
// was watching can still be argued about afterwards.
func TestReconcileRecordsTheFrameItRead(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "agent", "%1", agentstore.StateIdle)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.captures["%1"] = busyFrame

	recorder := &fakeFrameRecorder{}
	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
		States:   st.Agents,
	}).WithFrameRecorder(recorder)
	require.NoError(t, rec.Reconcile(t.Context()))

	require.Len(t, recorder.recorded, 1)
	assert.Equal(t, busyFrame, recorder.recorded[0].Frame)
	assert.Equal(t, "working", recorder.recorded[0].Activity)
	assert.True(t, recorder.recorded[0].Disputes, "an idle agent whose pane is working is the reading a recording is turned on for")
	assert.Equal(t, 1, recorder.forgotten)
}

// TestReconcileRecordsAnUnrecognizedFrameDisputingAnIdleAgent covers a row the panel calls
// finished whose pane weave cannot read, which is the reading the recording exists for.
func TestReconcileRecordsAnUnrecognizedFrameDisputingAnIdleAgent(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "agent", "%1", agentstore.StateIdle)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.captures["%1"] = unrecognizedFrame

	recorder := &fakeFrameRecorder{}
	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
		States:   st.Agents,
	}).WithFrameRecorder(recorder)
	require.NoError(t, rec.Reconcile(t.Context()))

	require.Len(t, recorder.recorded, 1)
	assert.Equal(t, "unrecognized", recorder.recorded[0].Activity)
	assert.True(t, recorder.recorded[0].Disputes, "an unrecognised frame settles uncertain, which is not where the row is")
}

// TestReconcileRecordsAnUnrecognizedFrameAgreeingWithAnUncertainAgent is the same frame on a row
// already drawn as uncertain, which nothing about the pane disputes.
func TestReconcileRecordsAnUnrecognizedFrameAgreeingWithAnUncertainAgent(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "agent", "%1", agentstore.StateUncertain)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.captures["%1"] = unrecognizedFrame

	recorder := &fakeFrameRecorder{}
	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
		States:   st.Agents,
	}).WithFrameRecorder(recorder)
	require.NoError(t, rec.Reconcile(t.Context()))

	require.Len(t, recorder.recorded, 1)
	assert.False(t, recorder.recorded[0].Disputes)
}

// TestReconcileRecordsAnUnrecognizedFrameDisputingAThinkingAgent is the same frame on an agent
// the panel still shows as working, which is a reading worth keeping every time.
func TestReconcileRecordsAnUnrecognizedFrameDisputingAThinkingAgent(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.captures["%1"] = unrecognizedFrame

	recorder := &fakeFrameRecorder{}
	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
		States:   st.Agents,
	}).WithFrameRecorder(recorder)
	require.NoError(t, rec.Reconcile(t.Context()))

	require.Len(t, recorder.recorded, 1)
	assert.True(t, recorder.recorded[0].Disputes)
}

// TestReconcileWithoutARecorderRecordsNothing covers the ordinary install, where no recording
// has been asked for and the reconciler holds none.
func TestReconcileWithoutARecorderRecordsNothing(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "agent", "%1", agentstore.StateIdle)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.captures["%1"] = busyFrame

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
		States:   st.Agents,
	})

	require.NoError(t, rec.Reconcile(t.Context()))
}

// TestReconcileForgetsAnAbandonedPane pins that a dormant agent drops its pane id, which it
// would otherwise be typed at through and could not be resumed from.
func TestReconcileForgetsAnAbandonedPane(t *testing.T) {
	tests := []struct {
		name      string
		mine      bool
		command   string
		wantStamp string
	}{
		{
			name:      "a pane whose CLI exited is unstamped as well as forgotten",
			mine:      true,
			command:   "-zsh",
			wantStamp: "",
		},
		{
			name:      "a pane that belongs to another agent keeps its stamp",
			command:   "2.1.241",
			wantStamp: "other-agent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			ctx := t.Context()
			agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

			tmux := newFakeTmux()
			tmux.owners["%1"] = "other-agent"
			if tt.mine {
				tmux.owners["%1"] = agent.ID
			}
			tmux.commands["%1"] = tt.command
			tmux.captures["%1"] = busyFrame

			rec := NewReconciler(NewReconcilerParams{
				Panes:    tmux,
				Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
				States:   st.Agents,
			})
			require.NoError(t, rec.Reconcile(ctx))
			waitOutTheShellGrace(rec)
			require.NoError(t, rec.Reconcile(ctx))

			after := agentByID(t, st, agent.ID)
			assert.Equal(t, agentstore.StateIdle, after.State)
			assert.Empty(t, after.Pane, "the agent stops claiming a pane it is not running in")
			assert.Equal(t, tt.wantStamp, tmux.owners["%1"])
		})
	}
}

func waitOutTheShellGrace(rec *Reconciler) {
	at := time.Now().Add(reconcileShellGrace + time.Second)
	rec.tally.now = func() time.Time { return at }
}

// TestReconcileWaitsOutALaunch pins that a just-opened pane is not read as an exit.
func TestReconcileWaitsOutALaunch(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	agent := seed(t, st, "agent", "%1", agentstore.StateIdle)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.commands["%1"] = "zsh"
	tmux.captures["%1"] = busyFrame

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
		States:   st.Agents,
	})
	require.NoError(t, rec.Reconcile(ctx))
	require.NoError(t, rec.Reconcile(ctx))

	during := agentByID(t, st, agent.ID)
	assert.Equal(t, "%1", during.Pane, "the pane is still the agent's while its CLI comes up")

	tmux.commands["%1"] = "2.1.241"
	waitOutTheShellGrace(rec)
	require.NoError(t, rec.Reconcile(ctx))

	after := agentByID(t, st, agent.ID)
	assert.Equal(t, agentstore.StateThinking, after.State)
	assert.Equal(t, "%1", after.Pane)
	assert.Equal(t, agent.ID, tmux.owners["%1"], "the stamp is left where the launcher put it")
}

// TestReconcileOverPasses covers what only shows up across several passes, the
// idle debounce that keeps a lull between tool calls from demoting live work,
// and the miss count that debounce is built on.
func TestReconcileOverPasses(t *testing.T) {
	tests := []struct {
		name              string
		passes            []string
		archiveBeforeLast bool
		wantAfterFirst    agentstore.State
		wantBeforeLast    agentstore.State
		want              agentstore.State
		wantMisses        *int
	}{
		{
			name:           "an idle pane only demotes once the misses run out",
			passes:         repeatFrame(idleFrame, reconcileMisses),
			wantBeforeLast: agentstore.StateThinking,
			want:           agentstore.StateIdle,
		},
		{
			name:       "a busy pass clears the accrued misses, so a trailing idle one cannot demote",
			passes:     []string{idleFrame, busyFrame, idleFrame},
			want:       agentstore.StateThinking,
			wantMisses: xptr.Ptr(1),
		},
		{
			name:           "a prompt moves it to waiting, and answering lets it settle to idle",
			passes:         append([]string{waitFrame}, repeatFrame(idleFrame, reconcileMisses)...),
			wantAfterFirst: agentstore.StateWaiting,
			want:           agentstore.StateIdle,
		},
		{
			name:           "a prompt somebody is typing into is still a finished turn",
			passes:         repeatFrame(composingFrame, reconcileMisses),
			wantBeforeLast: agentstore.StateThinking,
			want:           agentstore.StateIdle,
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

			rec := NewReconciler(NewReconcilerParams{
				Panes:    tmux,
				Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
				States:   st.Agents,
			})
			ctx := t.Context()

			for i, frame := range tt.passes {
				last := i == len(tt.passes)-1
				if last && tt.archiveBeforeLast {
					require.NoError(t, st.SoftDelete(ctx, "agent"))
					assert.NotZero(t, rec.tally.countMisses(agent.ID), "the earlier idle pass should have recorded a miss")
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
				assert.Empty(t, rec.tally.tracked, "what was tracked is reaped once the agent is gone")
			}

			if tt.wantMisses != nil {
				assert.Equal(t, *tt.wantMisses, rec.tally.countMisses(agent.ID))
			}
		})
	}
}

func repeatFrame(frame string, n int) []string {
	frames := make([]string, n)
	for i := range frames {
		frames[i] = frame
	}

	return frames
}

// TestReconcileLeavesAnUnknownHarnessAlone pins that an agent whose CLI this binary cannot run
// keeps the state it holds.
func TestReconcileLeavesAnUnknownHarnessAlone(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()

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

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}, only: []string{testHarnessID}}}),
		States:   st.Agents,
	})
	for range reconcileMisses + 1 {
		require.NoError(t, rec.Reconcile(ctx))
	}

	assert.Equal(t, agentstore.StateThinking, stateOf(t, st, "orphan"))
	assert.Empty(t, rec.tally.tracked, "an unread frame is not an idle observation")
}

// TestReconcileTurnBoundaryOnlyAcceleratesWhatThePaneAgrees pins that a reported boundary
// hurries a state change the frame already supports rather than overriding what the pane says.
func TestReconcileTurnBoundaryOnlyAcceleratesWhatThePaneAgrees(t *testing.T) {
	tests := []struct {
		name  string
		frame string
		want  agentstore.State
	}{
		{
			name:  "a boundary against a frame still showing work does not move it",
			frame: busyFrame,
			want:  agentstore.StateThinking,
		},
		{
			name:  "a boundary against an idle frame skips the miss counter",
			frame: idleFrame,
			want:  agentstore.StateIdle,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			ctx := t.Context()
			agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

			tmux := newFakeTmux()
			tmux.owners["%1"] = agent.ID
			tmux.captures["%1"] = tt.frame

			rec := NewReconciler(NewReconcilerParams{
				Panes:    tmux,
				Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
				States:   st.Agents,
			})
			rec.tally.now = func() time.Time { return agent.StateSeenAt.Add(time.Second) }
			rec.RecordTurnBoundary(agent.ID)

			require.NoError(t, rec.Reconcile(ctx))
			assert.Equal(t, tt.want, stateOf(t, st, "agent"))
			assert.Zero(t, rec.tally.countMisses(agent.ID), "the boundary settles it in one pass rather than accruing a miss")
		})
	}
}

// TestReconcileTurnBoundaryOlderThanTheLastObservationDoesNothing pins that a boundary from
// before the last look is stale and is ignored.
func TestReconcileTurnBoundaryOlderThanTheLastObservationDoesNothing(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.captures["%1"] = idleFrame

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
		States:   st.Agents,
	})
	rec.tally.now = func() time.Time { return agent.StateSeenAt.Add(-time.Second) }
	rec.RecordTurnBoundary(agent.ID)
	rec.tally.now = time.Now

	require.NoError(t, rec.Reconcile(ctx))
	assert.Equal(t, agentstore.StateThinking, stateOf(t, st, "agent"), "a stale boundary is not licence to skip a miss")
	assert.Equal(t, 1, rec.tally.countMisses(agent.ID), "the pass still counted as an ordinary miss")
}

// TestReconcileUnrecognizedFrameSettlesUncertainWhenStatic pins that a frame nothing recognizes
// moves the agent off thinking once it has stopped changing, and lands on uncertain rather than
// claiming the agent finished.
func TestReconcileUnrecognizedFrameSettlesUncertainWhenStatic(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.captures["%1"] = unrecognizedFrame

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
		States:   st.Agents,
	})
	for range reconcileMisses - 1 {
		require.NoError(t, rec.Reconcile(ctx))
		assert.Equal(t, agentstore.StateThinking, stateOf(t, st, "agent"))
	}

	require.NoError(t, rec.Reconcile(ctx))
	assert.Equal(t, agentstore.StateUncertain, stateOf(t, st, "agent"))
}

// TestReconcileUnrecognizedFrameThatKeepsChangingHolds pins that a frame nothing recognizes
// but which keeps moving is work in progress rather than an idle pane.
func TestReconcileUnrecognizedFrameThatKeepsChangingHolds(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
		States:   st.Agents,
	})

	tmux.captures["%1"] = unrecognizedFrame + "-0"
	require.NoError(t, rec.Reconcile(ctx))

	for i := 1; i <= reconcileMisses+2; i++ {
		tmux.captures["%1"] = fmt.Sprintf("%s-%d", unrecognizedFrame, i)
		require.NoError(t, rec.Reconcile(ctx))
		assert.Equal(t, agentstore.StateThinking, stateOf(t, st, "agent"))
		assert.Zero(t, rec.tally.countMisses(agent.ID))
	}
}

// TestReconcileMissesAccrueRatherThanRestartOnceTheHoldIsCapped pins that a frame changing
// forever stops resetting the miss counter, so the hold cannot run on without end.
func TestReconcileMissesAccrueRatherThanRestartOnceTheHoldIsCapped(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
		States:   st.Agents,
	})

	tmux.captures["%1"] = unrecognizedFrame + "-0"
	require.NoError(t, rec.Reconcile(ctx))

	for i := 1; i <= unrecognizedHoldLimit; i++ {
		tmux.captures["%1"] = fmt.Sprintf("%s-%d", unrecognizedFrame, i)
		require.NoError(t, rec.Reconcile(ctx))
	}

	for i := 1; i < reconcileMisses; i++ {
		tmux.captures["%1"] = fmt.Sprintf("%s-%d", unrecognizedFrame, unrecognizedHoldLimit+i)
		require.NoError(t, rec.Reconcile(ctx))
		assert.Equal(t, agentstore.StateThinking, stateOf(t, st, "agent"),
			"a still-changing frame past the cap must not restart the hold")
	}

	tmux.captures["%1"] = fmt.Sprintf("%s-%d", unrecognizedFrame, unrecognizedHoldLimit+reconcileMisses)
	require.NoError(t, rec.Reconcile(ctx))
	assert.Equal(t, agentstore.StateUncertain, stateOf(t, st, "agent"),
		"the accrued misses, not a restarted hold, are what finally demote it")
}

// TestReconcileCountsUnrecognizedFrames pins the running count of frames nothing recognized,
// which is what says a harness has changed its output.
func TestReconcileCountsUnrecognizedFrames(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.captures["%1"] = unrecognizedFrame

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
		States:   st.Agents,
	})
	require.NoError(t, rec.Reconcile(ctx))
	require.NoError(t, rec.Reconcile(ctx))

	assert.Equal(t, 2, rec.unrecognizedTotal)
}

// TestReconcileWithoutTmux covers a pass that could not read tmux at all, where treating
// the failure as an answer would clear every pane and demote every agent at once.
func TestReconcileWithoutTmux(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, agentstore.Agent{
		Name:  "busy",
		Pane:  "%1",
		State: agentstore.StateThinking,
	}))
	agent, err := st.Get(ctx, "busy")
	require.NoError(t, err)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.captures["%1"] = busyFrame
	tmux.paneOwnersErr = errors.New("no server running")

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
		States:   st.Agents,
	})
	require.Error(t, rec.Reconcile(ctx), "a pass that observed nothing is a failed pass")

	after := agentByID(t, st, agent.ID)
	assert.Equal(t, "%1", after.Pane, "the pane is kept, since nothing said it was gone")
	assert.Equal(t, agentstore.StateThinking, after.State, "and the state stands")
}

// TestReconcileStampsWhatItConfirmed pins that only a pass which read the frame moves the
// stamp, so an agent nothing can observe is never mistaken for an observed one.
func TestReconcileStampsWhatItConfirmed(t *testing.T) {
	tests := []struct {
		name          string
		harness       string
		passes        int
		frame         string
		wantConfirmed bool
	}{
		{
			name:          "a working frame confirms the state",
			passes:        1,
			frame:         busyFrame,
			wantConfirmed: true,
		},
		{
			name:          "so does a prompt",
			passes:        1,
			frame:         waitFrame,
			wantConfirmed: true,
		},
		{
			name:          "an idle frame confirms once it has settled the demotion",
			passes:        reconcileMisses,
			frame:         idleFrame,
			wantConfirmed: true,
		},
		{
			name:          "a capture that came back empty confirms nothing",
			passes:        reconcileMisses + 1,
			frame:         "",
			wantConfirmed: false,
		},
		{
			name:          "and neither does a harness nothing here implements",
			harness:       "gemini",
			passes:        reconcileMisses + 1,
			frame:         busyFrame,
			wantConfirmed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			ctx := t.Context()

			require.NoError(t, st.Create(ctx, agentstore.Agent{
				Name:    "agent",
				Pane:    "%1",
				State:   agentstore.StateThinking,
				Harness: tt.harness,
			}))
			agent, err := st.Get(ctx, "agent")
			require.NoError(t, err)

			backdateStateSeen(t, st, agent.ID, stateSeenCoalesce+time.Minute)
			before := agentByID(t, st, agent.ID).StateSeenAt

			tmux := newFakeTmux()
			tmux.owners["%1"] = agent.ID
			tmux.captures["%1"] = tt.frame

			rec := NewReconciler(NewReconcilerParams{
				Panes:    tmux,
				Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}, only: []string{testHarnessID}}}),
				States:   st.Agents,
			})
			for range tt.passes {
				require.NoError(t, rec.Reconcile(ctx))
			}

			after := agentByID(t, st, agent.ID)
			if tt.wantConfirmed {
				assert.True(t, after.StateSeenAt.After(before), "the pass confirmed the state")
				assert.False(t, isStateUnconfirmed(after))

				return
			}

			assert.Equal(t, before, after.StateSeenAt, "nothing read the pane, so nothing was confirmed")
		})
	}
}

// TestReconcileCoalescesTheConfirmationStamp pins the stamp not being written every pass,
// which would be an update per agent every couple of seconds.
func TestReconcileCoalescesTheConfirmationStamp(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.captures["%1"] = busyFrame

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
		States:   st.Agents,
	})
	for range 5 {
		require.NoError(t, rec.Reconcile(ctx))
	}

	fresh := agentByID(t, st, agent.ID)
	assert.Equal(t, agent.UpdatedAt, fresh.UpdatedAt, "a stamp still fresh is not worth a write")

	backdateStateSeen(t, st, agent.ID, stateSeenCoalesce+time.Second)
	require.NoError(t, rec.Reconcile(ctx))

	stamped := agentByID(t, st, agent.ID)
	assert.True(t, stamped.StateSeenAt.After(fresh.StateSeenAt), "once it has gone stale it is written")
	assert.Equal(t, agent.StateChangedAt, stamped.StateChangedAt, "and the state itself never moved")
}

// resumeAfterSnapshot is a pane probe that moves the world on once a pass has read the panes,
// which is the last moment before the walk and the window a resume lands in.
type resumeAfterSnapshot struct {
	paneObserver
	resume func()
}

func (p *resumeAfterSnapshot) GetPanes() (map[string]tmux.PaneStatus, error) {
	panes, err := p.paneObserver.GetPanes()
	if err != nil {
		return nil, err
	}

	p.resume()

	return panes, nil
}

// TestReconcileLeavesARowAResumeHasMovedOn pins that a pass does not write its conclusion over
// a row a resume has since given a live pane, including one on a reused pane id.
func TestReconcileLeavesARowAResumeHasMovedOn(t *testing.T) {
	tests := []struct {
		name string
		pane string
	}{
		{
			name: "a resume into a new pane",
			pane: "%2",
		},
		{
			name: "a resume into a pane id the restarted server handed out again",
			pane: "%1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			ctx := t.Context()
			agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

			tmux := newFakeTmux()

			resumed := agent
			resumed.Pane = tt.pane
			resumed.State = agentstore.StateIdle

			observer := &resumeAfterSnapshot{paneObserver: tmux, resume: func() {
				require.NoError(t, tmux.SetPaneOwner(tt.pane, agent.ID))
				require.NoError(t, st.Agents.Update(ctx, resumed))
			}}

			rec := NewReconciler(NewReconcilerParams{
				Panes:    observer,
				Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
				States:   st.Agents,
			})
			require.NoError(t, rec.Reconcile(ctx))

			after := agentByID(t, st, agent.ID)
			assert.Equal(t, tt.pane, after.Pane, "a pass holding a stale row does not forget the pane a resume just wrote")
			assert.Nil(t, after.InterruptedAt, "a resumed agent's turn was not cut off by the pass that was mid-walk")
		})
	}
}

// fakeObserver reports a reading of its own rather than reading any screen, which is what a
// source weave is told by rather than watches looks like.
type fakeObserver struct {
	observation harness.Observation
	err         error
}

func (f *fakeObserver) Observe(context.Context, harness.Target) (harness.Observation, error) {
	return f.observation, f.err
}

// TestReconcileSettlesOnASourceThatReadsNoScreen pins that an observer carrying no frame moves
// an agent's state and leaves the recorder nothing, since a recording exists to replay a
// misread frame and there is none.
func TestReconcileSettlesOnASourceThatReadsNoScreen(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID

	recorder := &fakeFrameRecorder{}
	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: &fakeObserver{observation: harness.Observation{Activity: harness.ActivityIdle}},
		States:   st.Agents,
	}).WithFrameRecorder(recorder)

	for range reconcileMisses {
		require.NoError(t, rec.Reconcile(ctx))
	}

	assert.Equal(t, agentstore.StateIdle, stateOf(t, st, "agent"))
	assert.Empty(t, recorder.recorded, "a source with no frame has nothing to record")
}

// TestReconcileLeavesAnAgentAloneWhenTheObserverHasNothingToSay pins that a reading carrying
// neither an activity nor a frame writes nothing at all.
func TestReconcileLeavesAnAgentAloneWhenTheObserverHasNothingToSay(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID

	recorder := &fakeFrameRecorder{}
	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: &fakeObserver{},
		States:   st.Agents,
	}).WithFrameRecorder(recorder)

	for range reconcileMisses {
		require.NoError(t, rec.Reconcile(ctx))
	}

	assert.Equal(t, agentstore.StateThinking, stateOf(t, st, "agent"))
	assert.Empty(t, recorder.recorded)
}

// TestReconcileLeavesAnAgentAloneWhenTheObserverFails pins that a reading weave could not take
// is not a reading that moves anything.
func TestReconcileLeavesAnAgentAloneWhenTheObserverFails(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID

	rec := NewReconciler(NewReconcilerParams{
		Panes: tmux,
		Observer: &fakeObserver{
			observation: harness.Observation{Activity: harness.ActivityIdle},
			err:         errors.New("the frame went unreadable"),
		},
		States: st.Agents,
	})

	for range reconcileMisses {
		require.NoError(t, rec.Reconcile(ctx))
	}

	assert.Equal(t, agentstore.StateThinking, stateOf(t, st, "agent"))
}

// TestReconcileStampsTheTurnItSettled pins that the pane raises the unread mark when no hook
// reported the boundary.
func TestReconcileStampsTheTurnItSettled(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.captures["%1"] = idleFrame

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
		States:   st.Agents,
	})

	for range reconcileMisses {
		require.NoError(t, rec.Reconcile(ctx))
	}

	require.Equal(t, agentstore.StateIdle, stateOf(t, st, "agent"))
	assert.NotNil(t, agentByID(t, st, agent.ID).TurnEndedAt, "the settled turn is what the mark is measured against")
}

// TestReconcileLeavesTheStampToTheHookWhenItReported pins that a boundary the CLI reported
// settles the agent without the reconciler stamping, so the pane stays the floor under the
// mark rather than a second writer racing the hook for it.
func TestReconcileLeavesTheStampToTheHookWhenItReported(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.captures["%1"] = idleFrame

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
		States:   st.Agents,
	})
	rec.tally.now = func() time.Time { return agent.StateSeenAt.Add(time.Second) }
	rec.RecordTurnBoundary(agent.ID)

	require.NoError(t, rec.Reconcile(ctx))

	require.Equal(t, agentstore.StateIdle, stateOf(t, st, "agent"))
	assert.Nil(t, agentByID(t, st, agent.ID).TurnEndedAt, "the reconciler leaves this path to the hook")
}

// TestReconcileUnrecognizedFrameTakesAnIdleAgentToUncertain pins the reading that used to be
// silent: a row the panel calls finished whose pane weave can no longer read stops claiming the
// agent is done.
func TestReconcileUnrecognizedFrameTakesAnIdleAgentToUncertain(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	agent := seed(t, st, "agent", "%1", agentstore.StateIdle)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.captures["%1"] = unrecognizedFrame

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
		States:   st.Agents,
	})
	for range reconcileMisses {
		require.NoError(t, rec.Reconcile(ctx))
	}

	assert.Equal(t, agentstore.StateUncertain, stateOf(t, st, "agent"))
}

// TestReconcileStampsNoTurnForAFrameItCouldNotRead pins that settling uncertain raises no unread
// mark.
func TestReconcileStampsNoTurnForAFrameItCouldNotRead(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.captures["%1"] = unrecognizedFrame

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
		States:   st.Agents,
	})
	for range reconcileMisses {
		require.NoError(t, rec.Reconcile(ctx))
	}

	require.Equal(t, agentstore.StateUncertain, stateOf(t, st, "agent"))
	assert.Nil(t, agentByID(t, st, agent.ID).TurnEndedAt)
}

// TestReconcileLeavesUncertainOnceTheFrameReadsAgain pins that uncertain is a reading rather
// than a trap, so a pane that comes back is read as whatever it now shows.
func TestReconcileLeavesUncertainOnceTheFrameReadsAgain(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	agent := seed(t, st, "agent", "%1", agentstore.StateUncertain)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.captures["%1"] = busyFrame

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
		States:   st.Agents,
	})
	require.NoError(t, rec.Reconcile(ctx))

	assert.Equal(t, agentstore.StateThinking, stateOf(t, st, "agent"))
}

// TestReconcileStampsNoTurnForAPaneItForgot pins that an agent whose pane went away is not
// marked.
func TestReconcileStampsNoTurnForAPaneItForgot(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := newFakeTmux()

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
		States:   st.Agents,
	})

	for range listingsBeforePaneGone {
		require.NoError(t, rec.Reconcile(ctx))
	}

	require.Equal(t, agentstore.StateIdle, stateOf(t, st, "agent"))
	assert.Nil(t, agentByID(t, st, agent.ID).TurnEndedAt, "a lost pane is an interruption, not a turn end")
}

// TestReconcileReportedReasonMovesNoState pins the whole of what a reported reason may do: it is
// kept beside the agent and decides nothing, so a frame the pane still reads as work leaves the
// agent working and the misses a transition needs are still earned one pass at a time.
func TestReconcileReportedReasonMovesNoState(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.captures["%1"] = idleFrame

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
		States:   st.Agents,
	})
	rec.tally.now = func() time.Time { return agent.StateSeenAt.Add(time.Second) }
	rec.RecordReportedReason(agent.ID, "Bash")

	require.NoError(t, rec.Reconcile(ctx))
	assert.Equal(t, agentstore.StateThinking, stateOf(t, st, "agent"),
		"a reason says nothing about whether the turn ended")
	assert.Equal(t, 1, rec.tally.countMisses(agent.ID), "the pass counted as an ordinary miss")
	assert.Equal(t, "Bash", rec.tally.readReportedReason(agent.ID))
}

// TestReconcileTurnBoundaryDoesNotShortcutIntoUncertain pins that the one positive report weave
// has that a turn finished cannot be spent reaching the state that says weave does not know
// whether it finished, so an unreadable frame still earns its misses.
func TestReconcileTurnBoundaryDoesNotShortcutIntoUncertain(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.captures["%1"] = unrecognizedFrame

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
		States:   st.Agents,
	})
	rec.tally.now = func() time.Time { return agent.StateSeenAt.Add(time.Second) }
	rec.RecordTurnBoundary(agent.ID)

	require.NoError(t, rec.Reconcile(ctx))
	assert.Equal(t, agentstore.StateThinking, stateOf(t, st, "agent"),
		"a reported turn end is no evidence for a frame weave could not read")

	for range reconcileMisses - 1 {
		require.NoError(t, rec.Reconcile(ctx))
	}

	assert.Equal(t, agentstore.StateUncertain, stateOf(t, st, "agent"),
		"the misses still take it there, they are only no longer skipped")
}

// TestReconcileMissesOnlyCountTowardsTheStateTheyWereEarnedFor pins that the passes an agent
// accrues towards one destination cannot be spent reaching another, so the three readings a
// change is debounced behind still have to agree with each other about what they saw.
func TestReconcileMissesOnlyCountTowardsTheStateTheyWereEarnedFor(t *testing.T) {
	tests := []struct {
		name    string
		earned  string
		spentOn string
		want    agentstore.State
	}{
		{
			name:    "frames weave could not read do not pay for declaring the agent idle",
			earned:  unrecognizedFrame,
			spentOn: idleFrame,
			want:    agentstore.StateIdle,
		},
		{
			name:    "and clean idle readings do not pay for declaring it uncertain",
			earned:  idleFrame,
			spentOn: unrecognizedFrame,
			want:    agentstore.StateUncertain,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			ctx := t.Context()
			agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

			tmux := newFakeTmux()
			tmux.owners["%1"] = agent.ID

			rec := NewReconciler(NewReconcilerParams{
				Panes:    tmux,
				Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
				States:   st.Agents,
			})

			tmux.captures["%1"] = tt.earned
			for range reconcileMisses - 1 {
				require.NoError(t, rec.Reconcile(ctx))
			}
			require.Equal(t, agentstore.StateThinking, stateOf(t, st, "agent"))

			tmux.captures["%1"] = tt.spentOn
			require.NoError(t, rec.Reconcile(ctx))
			assert.Equal(t, agentstore.StateThinking, stateOf(t, st, "agent"),
				"one reading of its own is not the stretch a change is debounced behind")
			assert.Nil(t, agentByID(t, st, agent.ID).TurnEndedAt,
				"and nothing has finished a turn on the strength of it either")
			assert.Equal(t, 1, rec.tally.countMisses(agent.ID),
				"the run restarts at one, since the reading counts towards its own destination")

			for range reconcileMisses - 1 {
				require.NoError(t, rec.Reconcile(ctx))
			}
			assert.Equal(t, tt.want, stateOf(t, st, "agent"),
				"a stretch that does agree still settles it")
		})
	}
}
