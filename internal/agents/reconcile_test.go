package agents

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"weave/internal/harness"
	"weave/internal/harness/pane"
	"weave/internal/harnesstest"
	"weave/internal/mail"
	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	schedulestore "weave/internal/store/schedule"
	"weave/internal/tmux"
	"weave/internal/tmuxtest"
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
			frame:     harnesstest.FrameIdle,
			want:      agentstore.StateThinking,
		},
		{
			name:      "idle frame leaves idle untouched",
			fromState: agentstore.StateIdle,
			owned:     true,
			frame:     harnesstest.FrameIdle,
			want:      agentstore.StateIdle,
		},
		{
			name:      "waiting frame moves thinking to waiting",
			fromState: agentstore.StateThinking,
			owned:     true,
			frame:     harnesstest.FrameWaiting,
			want:      agentstore.StateWaiting,
		},
		{
			name:      "work promotes idle to thinking",
			fromState: agentstore.StateIdle,
			owned:     true,
			frame:     harnesstest.FrameWorking,
			want:      agentstore.StateThinking,
		},
		{
			name:      "work keeps thinking",
			fromState: agentstore.StateThinking,
			owned:     true,
			frame:     harnesstest.FrameWorking,
			want:      agentstore.StateThinking,
		},
		{
			name:      "a pane back at its shell does not demote in one pass, since a launch looks the same",
			fromState: agentstore.StateThinking,
			owned:     true,
			command:   "zsh",
			frame:     harnesstest.FrameWorking,
			want:      agentstore.StateThinking,
		},
		{
			name:      "a foreground program weave does not recognise is taken for the CLI",
			fromState: agentstore.StateIdle,
			owned:     true,
			command:   "2.1.241",
			frame:     harnesstest.FrameWorking,
			want:      agentstore.StateThinking,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			agent := seed(t, st, "agent", "%1", tt.fromState)

			tmux := tmuxtest.NewFake()
			tmux.Owners["%1"] = ""
			if tt.owned {
				tmux.Owners["%1"] = agent.ID
			}
			tmux.Commands["%1"] = tt.command
			tmux.Captures["%1"] = tt.frame

			rec := NewReconciler(NewReconcilerParams{
				Panes:    tmux,
				Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
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

	tmux := tmuxtest.NewFake()
	tmux.Owners["%1"] = agent.ID
	tmux.Captures["%1"] = harnesstest.FrameWorking

	recorder := &fakeFrameRecorder{}
	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
		States:   st.Agents,
	}).WithFrameRecorder(recorder)
	require.NoError(t, rec.Reconcile(t.Context()))

	require.Len(t, recorder.recorded, 1)
	assert.Equal(t, harnesstest.FrameWorking, recorder.recorded[0].Frame)
	assert.Equal(t, "working", recorder.recorded[0].Activity)
	assert.True(t, recorder.recorded[0].Disputes, "an idle agent whose pane is working is the reading a recording is turned on for")
	assert.Equal(t, 1, recorder.forgotten)
}

// TestReconcileRecordsAnUnrecognizedFrameDisputingAnIdleAgent covers a row the panel calls finished
// whose pane weave cannot read.
func TestReconcileRecordsAnUnrecognizedFrameDisputingAnIdleAgent(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "agent", "%1", agentstore.StateIdle)

	tmux := tmuxtest.NewFake()
	tmux.Owners["%1"] = agent.ID
	tmux.Captures["%1"] = harnesstest.FrameUnrecognized

	recorder := &fakeFrameRecorder{}
	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
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

	tmux := tmuxtest.NewFake()
	tmux.Owners["%1"] = agent.ID
	tmux.Captures["%1"] = harnesstest.FrameUnrecognized

	recorder := &fakeFrameRecorder{}
	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
		States:   st.Agents,
	}).WithFrameRecorder(recorder)
	require.NoError(t, rec.Reconcile(t.Context()))

	require.Len(t, recorder.recorded, 1)
	assert.False(t, recorder.recorded[0].Disputes)
}

// TestReconcileRecordsAnUnrecognizedFrameDisputingAThinkingAgent is the same frame on an agent the
// panel still shows as working.
func TestReconcileRecordsAnUnrecognizedFrameDisputingAThinkingAgent(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := tmuxtest.NewFake()
	tmux.Owners["%1"] = agent.ID
	tmux.Captures["%1"] = harnesstest.FrameUnrecognized

	recorder := &fakeFrameRecorder{}
	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
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

	tmux := tmuxtest.NewFake()
	tmux.Owners["%1"] = agent.ID
	tmux.Captures["%1"] = harnesstest.FrameWorking

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
		States:   st.Agents,
	})

	require.NoError(t, rec.Reconcile(t.Context()))
}

// TestReconcileForgetsAnAbandonedPane pins that a dormant agent drops its pane id and that the
// shell its CLI left behind is closed.
func TestReconcileForgetsAnAbandonedPane(t *testing.T) {
	tests := []struct {
		name       string
		mine       bool
		command    string
		wantStamp  string
		wantKilled []string
	}{
		{
			name:       "a pane whose CLI exited is killed as well as forgotten",
			mine:       true,
			command:    "-zsh",
			wantStamp:  "",
			wantKilled: []string{"%1"},
		},
		{
			name:       "a pane that belongs to another agent is left alone",
			command:    "2.1.241",
			wantStamp:  "other-agent",
			wantKilled: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			ctx := t.Context()
			agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

			tmux := tmuxtest.NewFake()
			tmux.Owners["%1"] = "other-agent"
			if tt.mine {
				tmux.Owners["%1"] = agent.ID
			}
			tmux.Commands["%1"] = tt.command
			tmux.Captures["%1"] = harnesstest.FrameWorking

			rec := NewReconciler(NewReconcilerParams{
				Panes:    tmux,
				Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
				States:   st.Agents,
			})
			require.NoError(t, rec.Reconcile(ctx))
			waitOutTheShellGrace(rec)
			require.NoError(t, rec.Reconcile(ctx))

			after := agentByID(t, st, agent.ID)
			assert.Equal(t, agentstore.StateIdle, after.State)
			assert.Empty(t, after.Pane, "the agent stops claiming a pane it is not running in")
			assert.Equal(t, tt.wantStamp, tmux.Owners["%1"])
			assert.Equal(t, tt.wantKilled, tmux.KilledPanes)
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

	tmux := tmuxtest.NewFake()
	tmux.Owners["%1"] = agent.ID
	tmux.Commands["%1"] = "zsh"
	tmux.Captures["%1"] = harnesstest.FrameWorking

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
		States:   st.Agents,
	})
	require.NoError(t, rec.Reconcile(ctx))
	require.NoError(t, rec.Reconcile(ctx))

	during := agentByID(t, st, agent.ID)
	assert.Equal(t, "%1", during.Pane, "the pane is still the agent's while its CLI comes up")

	tmux.Commands["%1"] = "2.1.241"
	waitOutTheShellGrace(rec)
	require.NoError(t, rec.Reconcile(ctx))

	after := agentByID(t, st, agent.ID)
	assert.Equal(t, agentstore.StateThinking, after.State)
	assert.Equal(t, "%1", after.Pane)
	assert.Equal(t, agent.ID, tmux.Owners["%1"], "the stamp is left where the launcher put it")
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
			passes:         repeatFrame(harnesstest.FrameIdle, reconcileMisses),
			wantBeforeLast: agentstore.StateThinking,
			want:           agentstore.StateIdle,
		},
		{
			name:       "a busy pass clears the accrued misses, so a trailing idle one cannot demote",
			passes:     []string{harnesstest.FrameIdle, harnesstest.FrameWorking, harnesstest.FrameIdle},
			want:       agentstore.StateThinking,
			wantMisses: xptr.Ptr(1),
		},
		{
			name:           "a prompt moves it to waiting, and answering lets it settle to idle",
			passes:         append([]string{harnesstest.FrameWaiting}, repeatFrame(harnesstest.FrameIdle, reconcileMisses)...),
			wantAfterFirst: agentstore.StateWaiting,
			want:           agentstore.StateIdle,
		},
		{
			name:           "a prompt somebody is typing into is still a finished turn",
			passes:         repeatFrame(harnesstest.FrameComposing, reconcileMisses),
			wantBeforeLast: agentstore.StateThinking,
			want:           agentstore.StateIdle,
		},
		{
			name:              "an agent that leaves the listing has its misses reaped",
			passes:            []string{harnesstest.FrameIdle, harnesstest.FrameIdle},
			archiveBeforeLast: true,
			wantMisses:        xptr.Ptr(0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

			tmux := tmuxtest.NewFake()
			tmux.Owners["%1"] = agent.ID

			rec := NewReconciler(NewReconcilerParams{
				Panes:    tmux,
				Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
				States:   st.Agents,
			})
			ctx := t.Context()

			for i, frame := range tt.passes {
				last := i == len(tt.passes)-1
				if last && tt.archiveBeforeLast {
					require.NoError(t, st.SoftDelete(ctx, "agent"))
					assert.NotZero(t, rec.tally.read(agent.ID).misses, "the earlier idle pass should have recorded a miss")
				}

				tmux.Captures["%1"] = frame
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
				assert.Equal(t, *tt.wantMisses, rec.tally.read(agent.ID).misses)
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

	tmux := tmuxtest.NewFake()
	tmux.Owners["%1"] = agent.ID
	tmux.Captures["%1"] = harnesstest.FrameIdle

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}, Only: []string{harnesstest.FakeID}}}),
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
			frame: harnesstest.FrameWorking,
			want:  agentstore.StateThinking,
		},
		{
			name:  "a boundary against an idle frame skips the miss counter",
			frame: harnesstest.FrameIdle,
			want:  agentstore.StateIdle,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			ctx := t.Context()
			agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

			tmux := tmuxtest.NewFake()
			tmux.Owners["%1"] = agent.ID
			tmux.Captures["%1"] = tt.frame

			rec := NewReconciler(NewReconcilerParams{
				Panes:    tmux,
				Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
				States:   st.Agents,
			})
			rec.tally.now = func() time.Time { return agent.StateSeenAt.Add(time.Second) }
			rec.RecordTurnBoundary(agent.ID)

			require.NoError(t, rec.Reconcile(ctx))
			assert.Equal(t, tt.want, stateOf(t, st, "agent"))
			assert.Zero(t, rec.tally.read(agent.ID).misses, "the boundary settles it in one pass rather than accruing a miss")
		})
	}
}

// TestReconcileTurnBoundaryOlderThanTheLastObservationDoesNothing pins that a boundary from
// before the last look is stale and is ignored.
func TestReconcileTurnBoundaryOlderThanTheLastObservationDoesNothing(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := tmuxtest.NewFake()
	tmux.Owners["%1"] = agent.ID
	tmux.Captures["%1"] = harnesstest.FrameIdle

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
		States:   st.Agents,
	})
	rec.tally.now = func() time.Time { return agent.StateSeenAt.Add(-time.Second) }
	rec.RecordTurnBoundary(agent.ID)
	rec.tally.now = time.Now

	require.NoError(t, rec.Reconcile(ctx))
	assert.Equal(t, agentstore.StateThinking, stateOf(t, st, "agent"), "a stale boundary is not licence to skip a miss")
	assert.Equal(t, 1, rec.tally.read(agent.ID).misses, "the pass still counted as an ordinary miss")
}

// TestReconcileUnrecognizedFrameSettlesUncertainWhenStatic pins that a frame nothing recognizes
// moves the agent off thinking once it has stopped changing, and lands on uncertain rather than
// claiming the agent finished.
func TestReconcileUnrecognizedFrameSettlesUncertainWhenStatic(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := tmuxtest.NewFake()
	tmux.Owners["%1"] = agent.ID
	tmux.Captures["%1"] = harnesstest.FrameUnrecognized

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
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

	tmux := tmuxtest.NewFake()
	tmux.Owners["%1"] = agent.ID

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
		States:   st.Agents,
	})

	tmux.Captures["%1"] = harnesstest.FrameUnrecognized + "-0"
	require.NoError(t, rec.Reconcile(ctx))

	for i := 1; i <= reconcileMisses+2; i++ {
		tmux.Captures["%1"] = fmt.Sprintf("%s-%d", harnesstest.FrameUnrecognized, i)
		require.NoError(t, rec.Reconcile(ctx))
		assert.Equal(t, agentstore.StateThinking, stateOf(t, st, "agent"))
		assert.Zero(t, rec.tally.read(agent.ID).misses)
	}
}

// TestReconcileMissesAccrueRatherThanRestartOnceTheHoldIsCapped pins that a frame changing
// forever stops resetting the miss counter, so the hold cannot run on without end.
func TestReconcileMissesAccrueRatherThanRestartOnceTheHoldIsCapped(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := tmuxtest.NewFake()
	tmux.Owners["%1"] = agent.ID

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
		States:   st.Agents,
	})

	tmux.Captures["%1"] = harnesstest.FrameUnrecognized + "-0"
	require.NoError(t, rec.Reconcile(ctx))

	for i := 1; i <= unrecognizedHoldLimit; i++ {
		tmux.Captures["%1"] = fmt.Sprintf("%s-%d", harnesstest.FrameUnrecognized, i)
		require.NoError(t, rec.Reconcile(ctx))
	}

	for i := 1; i < reconcileMisses; i++ {
		tmux.Captures["%1"] = fmt.Sprintf("%s-%d", harnesstest.FrameUnrecognized, unrecognizedHoldLimit+i)
		require.NoError(t, rec.Reconcile(ctx))
		assert.Equal(t, agentstore.StateThinking, stateOf(t, st, "agent"),
			"a still-changing frame past the cap must not restart the hold")
	}

	tmux.Captures["%1"] = fmt.Sprintf("%s-%d", harnesstest.FrameUnrecognized, unrecognizedHoldLimit+reconcileMisses)
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

	tmux := tmuxtest.NewFake()
	tmux.Owners["%1"] = agent.ID
	tmux.Captures["%1"] = harnesstest.FrameUnrecognized

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
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

	tmux := tmuxtest.NewFake()
	tmux.Owners["%1"] = agent.ID
	tmux.Captures["%1"] = harnesstest.FrameWorking
	tmux.PaneOwnersErr = errors.New("no server running")

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
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
			frame:         harnesstest.FrameWorking,
			wantConfirmed: true,
		},
		{
			name:          "so does a prompt",
			passes:        1,
			frame:         harnesstest.FrameWaiting,
			wantConfirmed: true,
		},
		{
			name:          "an idle frame confirms once it has settled the demotion",
			passes:        reconcileMisses,
			frame:         harnesstest.FrameIdle,
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
			frame:         harnesstest.FrameWorking,
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

			tmux := tmuxtest.NewFake()
			tmux.Owners["%1"] = agent.ID
			tmux.Captures["%1"] = tt.frame

			rec := NewReconciler(NewReconcilerParams{
				Panes:    tmux,
				Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}, Only: []string{harnesstest.FakeID}}}),
				States:   st.Agents,
			})
			for range tt.passes {
				require.NoError(t, rec.Reconcile(ctx))
			}

			after := agentByID(t, st, agent.ID)
			if tt.wantConfirmed {
				assert.True(t, after.StateSeenAt.After(before), "the pass confirmed the state")
				assert.False(t, after.IsStateUnconfirmed())

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

	tmux := tmuxtest.NewFake()
	tmux.Owners["%1"] = agent.ID
	tmux.Captures["%1"] = harnesstest.FrameWorking

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
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
	paneKiller
	resume func()
}

func (p *resumeAfterSnapshot) GetPanes() (map[string]tmux.PaneStatus, error) {
	panes, err := p.paneKiller.GetPanes()
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

			tmux := tmuxtest.NewFake()

			resumed := agent
			resumed.Pane = tt.pane
			resumed.State = agentstore.StateIdle

			observer := &resumeAfterSnapshot{paneKiller: tmux, resume: func() {
				require.NoError(t, tmux.SetPaneOwner(tt.pane, agent.ID))
				require.NoError(t, st.Agents.Update(ctx, resumed))
			}}

			rec := NewReconciler(NewReconcilerParams{
				Panes:    observer,
				Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
				States:   st.Agents,
			})
			require.NoError(t, rec.Reconcile(ctx))

			after := agentByID(t, st, agent.ID)
			assert.Equal(t, tt.pane, after.Pane, "a pass holding a stale row does not forget the pane a resume just wrote")
			assert.Nil(t, after.InterruptedAt, "a resumed agent's turn was not cut off by the pass that was mid-walk")
		})
	}
}

// fakeObserver reports a reading of its own rather than reading any screen.
type fakeObserver struct {
	observation harness.Observation
	err         error
}

func (f *fakeObserver) Observe(context.Context, harness.Target) (harness.Observation, error) {
	return f.observation, f.err
}

// TestReconcileSettlesOnASourceThatReadsNoScreen pins that an observer carrying no frame moves
// an agent's state and leaves the recorder nothing.
func TestReconcileSettlesOnASourceThatReadsNoScreen(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := tmuxtest.NewFake()
	tmux.Owners["%1"] = agent.ID

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

	tmux := tmuxtest.NewFake()
	tmux.Owners["%1"] = agent.ID

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

	tmux := tmuxtest.NewFake()
	tmux.Owners["%1"] = agent.ID

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

// TestReconcileStampsTheTurnItSettled pins that the pane stamps the turn end when no hook
// reported the boundary.
func TestReconcileStampsTheTurnItSettled(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := tmuxtest.NewFake()
	tmux.Owners["%1"] = agent.ID
	tmux.Captures["%1"] = harnesstest.FrameIdle

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
		States:   st.Agents,
	})

	for range reconcileMisses {
		require.NoError(t, rec.Reconcile(ctx))
	}

	require.Equal(t, agentstore.StateIdle, stateOf(t, st, "agent"))
	assert.NotNil(t, agentByID(t, st, agent.ID).TurnEndedAt, "the settled turn is stamped")
}

// TestReconcileLeavesTheStampToTheHookWhenItReported pins that a boundary the CLI reported
// settles the agent without the reconciler stamping, so the hook is the only writer of a
// reported turn.
func TestReconcileLeavesTheStampToTheHookWhenItReported(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := tmuxtest.NewFake()
	tmux.Owners["%1"] = agent.ID
	tmux.Captures["%1"] = harnesstest.FrameIdle

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
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

	tmux := tmuxtest.NewFake()
	tmux.Owners["%1"] = agent.ID
	tmux.Captures["%1"] = harnesstest.FrameUnrecognized

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
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

	tmux := tmuxtest.NewFake()
	tmux.Owners["%1"] = agent.ID
	tmux.Captures["%1"] = harnesstest.FrameUnrecognized

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
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

	tmux := tmuxtest.NewFake()
	tmux.Owners["%1"] = agent.ID
	tmux.Captures["%1"] = harnesstest.FrameWorking

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
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

	tmux := tmuxtest.NewFake()

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
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

	tmux := tmuxtest.NewFake()
	tmux.Owners["%1"] = agent.ID
	tmux.Captures["%1"] = harnesstest.FrameIdle

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
		States:   st.Agents,
	})
	rec.tally.now = func() time.Time { return agent.StateSeenAt.Add(time.Second) }
	rec.RecordReportedReason(agent.ID, "Bash")

	require.NoError(t, rec.Reconcile(ctx))
	assert.Equal(t, agentstore.StateThinking, stateOf(t, st, "agent"),
		"a reason says nothing about whether the turn ended")
	assert.Equal(t, 1, rec.tally.read(agent.ID).misses, "the pass counted as an ordinary miss")
	assert.Equal(t, "Bash", rec.tally.read(agent.ID).reportedReason)
}

// TestReconcileTurnBoundaryDoesNotShortcutIntoUncertain pins that the one positive report weave
// has that a turn finished cannot be spent reaching the state that says weave does not know
// whether it finished, so an unreadable frame still earns its misses.
func TestReconcileTurnBoundaryDoesNotShortcutIntoUncertain(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := tmuxtest.NewFake()
	tmux.Owners["%1"] = agent.ID
	tmux.Captures["%1"] = harnesstest.FrameUnrecognized

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
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
			earned:  harnesstest.FrameUnrecognized,
			spentOn: harnesstest.FrameIdle,
			want:    agentstore.StateIdle,
		},
		{
			name:    "and clean idle readings do not pay for declaring it uncertain",
			earned:  harnesstest.FrameIdle,
			spentOn: harnesstest.FrameUnrecognized,
			want:    agentstore.StateUncertain,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			ctx := t.Context()
			agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

			tmux := tmuxtest.NewFake()
			tmux.Owners["%1"] = agent.ID

			rec := NewReconciler(NewReconcilerParams{
				Panes:    tmux,
				Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
				States:   st.Agents,
			})

			tmux.Captures["%1"] = tt.earned
			for range reconcileMisses - 1 {
				require.NoError(t, rec.Reconcile(ctx))
			}
			require.Equal(t, agentstore.StateThinking, stateOf(t, st, "agent"))

			tmux.Captures["%1"] = tt.spentOn
			require.NoError(t, rec.Reconcile(ctx))
			assert.Equal(t, agentstore.StateThinking, stateOf(t, st, "agent"),
				"one reading of its own is not the stretch a change is debounced behind")
			assert.Nil(t, agentByID(t, st, agent.ID).TurnEndedAt,
				"and nothing has finished a turn on the strength of it either")
			assert.Equal(t, 1, rec.tally.read(agent.ID).misses,
				"the run restarts at one, since the reading counts towards its own destination")

			for range reconcileMisses - 1 {
				require.NoError(t, rec.Reconcile(ctx))
			}
			assert.Equal(t, tt.want, stateOf(t, st, "agent"),
				"a stretch that does agree still settles it")
		})
	}
}

// TestScheduleIsNotBlockedByARunThatNeverStarted pins that a launch which died on the spot
// is not counted as work in flight, which would stop the schedule ever firing again.
func TestScheduleIsNotBlockedByARunThatNeverStarted(t *testing.T) {
	tests := []struct {
		name  string
		state agentstore.State
		frame string
		want  int64
	}{
		{
			name:  "a run that never started leaves the schedule free to fire",
			state: agentstore.StateIdle,
			frame: harnesstest.FrameIdle,
			want:  0,
		},
		{
			name:  "a run still working holds the next tick off",
			state: agentstore.StateThinking,
			frame: harnesstest.FrameWorking,
			want:  1,
		},
		{
			name:  "so does one waiting on an answer, which is work still owed",
			state: agentstore.StateIdle,
			frame: harnesstest.FrameWaiting,
			want:  1,
		},
		{
			name:  "a run that finished does not",
			state: agentstore.StateIdle,
			frame: harnesstest.FrameIdle,
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			ctx := t.Context()

			schedule := st.SeedSchedule(t, schedulestore.Config{
				Name:   "nightly",
				Cron:   "0 2 * * *",
				Prompt: "sweep",
			})
			run := st.SeedAgent(t, agentstore.Agent{
				Name:       "nightly-run",
				Pane:       "%1",
				State:      tt.state,
				WorkflowID: schedule.WorkflowID,
				ScheduleID: &schedule.ID,
			})

			tmux := tmuxtest.NewFake()
			tmux.Owners["%1"] = run.ID
			tmux.Captures["%1"] = tt.frame

			rec := NewReconciler(NewReconcilerParams{
				Panes:    tmux,
				Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
				States:   st.Agents,
			})
			for range reconcileMisses + 2 {
				require.NoError(t, rec.Reconcile(ctx))
			}

			count, err := st.Agents.CountLiveForSchedule(ctx, schedule.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.want, count, "what the next firing sees")
		})
	}
}

// TestDeliverToAnAgentNoPassCanRead pins that an agent whose harness this binary cannot run is
// told once rather than deferred every pass.
func TestDeliverToAnAgentNoPassCanRead(t *testing.T) {
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

	tmux := tmuxtest.NewFake()
	tmux.Owners["%1"] = agent.ID
	tmux.Commands["%1"] = "agent-cli"
	tmux.Captures["%1"] = harnesstest.FrameIdle
	harnesses := harnesstest.Registry{Harness: &harnesstest.Fake{}, Only: []string{harnesstest.FakeID}}

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesses}),
		States:   st.Agents,
	})
	for range 30 {
		require.NoError(t, rec.Reconcile(ctx))
	}

	pinned := agentByID(t, st, agent.ID)
	assert.Equal(t, agentstore.StateThinking, pinned.State, "thirty passes read nothing, so the state stands")
	assert.Equal(t, agent.StateSeenAt, pinned.StateSeenAt, "and none of them confirmed it")

	_, err = st.Mail.Send(ctx, mailstore.SendParams{
		FromName:  "somebody",
		ToAgentID: agent.ID,
		Body:      "please read this",
		Deadline:  time.Hour,
	})
	require.NoError(t, err)

	svc := mail.NewDeliveryService(mail.NewDeliveryServiceParams{
		MailboxReader: st.Mail,
		NudgeClaimer:  st.Agents,
		PaneReader:    NewPaneLiveness(tmux),
		Channel:       pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesses}),
	})

	got, err := svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, mail.DeliverySoon, got, "a state confirmed a moment ago is still worth waiting on")
	assert.Empty(t, tmux.Sent["%1"])

	backdateStateSeen(t, st, agent.ID, agentstore.StateSeenBound+time.Minute)

	got, err = svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, mail.DeliverySoon, got)
	require.Len(t, tmux.Sent["%1"], 1, "an unconfirmed state is not live work, so the agent is told")
	assert.Contains(t, tmux.Sent["%1"][0][0], "read_mail")

	got, err = svc.Deliver(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, mail.DeliveryLater, got)
	assert.Len(t, tmux.Sent["%1"], 1, "and told once, not once a pass")
}
