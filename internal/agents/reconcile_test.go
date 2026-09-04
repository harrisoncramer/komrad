package agents

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

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

			rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents)
			require.NoError(t, rec.Reconcile(context.Background()))
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
	rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents).WithFrameRecorder(recorder)
	require.NoError(t, rec.Reconcile(context.Background()))

	require.Len(t, recorder.recorded, 1)
	assert.Equal(t, busyFrame, recorder.recorded[0].Frame)
	assert.Equal(t, "working", recorder.recorded[0].Activity)
	assert.True(t, recorder.recorded[0].Disputes, "an idle agent whose pane is working is the reading a recording is turned on for")
	assert.Equal(t, 1, recorder.forgotten)
}

// TestReconcileRecordsAnUnrecognizedFrameAsAgreeingWithIdle covers the frame a stuck pane
// repeats, which must not be the thing that evicts the run-up showing how it got there.
func TestReconcileRecordsAnUnrecognizedFrameAsAgreeingWithIdle(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "agent", "%1", agentstore.StateIdle)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.captures["%1"] = unrecognizedFrame

	recorder := &fakeFrameRecorder{}
	rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents).WithFrameRecorder(recorder)
	require.NoError(t, rec.Reconcile(context.Background()))

	require.Len(t, recorder.recorded, 1)
	assert.Equal(t, "unrecognized", recorder.recorded[0].Activity)
	assert.False(t, recorder.recorded[0].Disputes, "an unrecognised frame settles idle, so on an idle agent it disputes nothing")
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
	rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents).WithFrameRecorder(recorder)
	require.NoError(t, rec.Reconcile(context.Background()))

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

	rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents)

	assert.NoError(t, rec.Reconcile(context.Background()))
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
			ctx := context.Background()
			agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

			tmux := newFakeTmux()
			tmux.owners["%1"] = "other-agent"
			if tt.mine {
				tmux.owners["%1"] = agent.ID
			}
			tmux.commands["%1"] = tt.command
			tmux.captures["%1"] = busyFrame

			rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents)
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
	rec.now = func() time.Time { return at }
}

// TestReconcileWaitsOutALaunch pins that a just-opened pane is not read as an exit, since
// weave types the launch command into a shell that is holding a live agent.
func TestReconcileWaitsOutALaunch(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	agent := seed(t, st, "agent", "%1", agentstore.StateIdle)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.commands["%1"] = "zsh"
	tmux.captures["%1"] = busyFrame

	rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents)
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

// TestReconcileLeavesTheTag pins that a pass moving an agent's state leaves its tag standing,
// since only the user sets or clears one.
func TestReconcileLeavesTheTag(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	agent := seed(t, st, "agent", "%1", agentstore.StateIdle)

	tagged, err := st.Agents.SetTags(ctx, []string{agent.ID}, xptr.Ptr("●"))
	require.NoError(t, err)
	require.Equal(t, 1, tagged)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.captures["%1"] = busyFrame

	rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents)
	require.NoError(t, rec.Reconcile(ctx))

	after, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, after)
	assert.Equal(t, agentstore.StateThinking, after.State, "the pass wrote a state")
	assert.Equal(t, "●", *after.Tag, "the user's tag survives a state write")
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

			rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents)
			ctx := context.Background()

			for i, frame := range tt.passes {
				last := i == len(tt.passes)-1
				if last && tt.archiveBeforeLast {
					require.NoError(t, st.SoftDelete(ctx, "agent"))
					assert.NotZero(t, rec.missCount(agent.ID), "the earlier idle pass should have recorded a miss")
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
				assert.Empty(t, rec.tracked, "what was tracked is reaped once the agent is gone")
			}

			if tt.wantMisses != nil {
				assert.Equal(t, *tt.wantMisses, rec.missCount(agent.ID))
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

// TestReconcileLeavesAnUnknownHarnessAlone pins that an agent whose CLI this binary cannot
// run keeps the state it holds, since demoting it would report work finished off an unread
// frame.
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
	assert.Empty(t, rec.tracked, "an unread frame is not an idle observation")
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
			ctx := context.Background()
			agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

			tmux := newFakeTmux()
			tmux.owners["%1"] = agent.ID
			tmux.captures["%1"] = tt.frame

			rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents)
			rec.now = func() time.Time { return agent.StateSeenAt.Add(time.Second) }
			rec.RecordTurnBoundary(agent.ID)

			require.NoError(t, rec.Reconcile(ctx))
			assert.Equal(t, tt.want, stateOf(t, st, "agent"))
			assert.Zero(t, rec.missCount(agent.ID), "the boundary settles it in one pass rather than accruing a miss")
		})
	}
}

// TestReconcileTurnBoundaryOlderThanTheLastObservationDoesNothing pins that a boundary from
// before the last look is stale and is ignored.
func TestReconcileTurnBoundaryOlderThanTheLastObservationDoesNothing(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.captures["%1"] = idleFrame

	rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents)
	rec.now = func() time.Time { return agent.StateSeenAt.Add(-time.Second) }
	rec.RecordTurnBoundary(agent.ID)
	rec.now = time.Now

	require.NoError(t, rec.Reconcile(ctx))
	assert.Equal(t, agentstore.StateThinking, stateOf(t, st, "agent"), "a stale boundary is not licence to skip a miss")
	assert.Equal(t, 1, rec.missCount(agent.ID), "the pass still counted as an ordinary miss")
}

// TestReconcileUnrecognizedFrameDemotesWhenStatic pins that a frame nothing recognizes moves
// the agent off thinking once it has stopped changing for long enough.
func TestReconcileUnrecognizedFrameDemotesWhenStatic(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.captures["%1"] = unrecognizedFrame

	rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents)
	for range reconcileMisses - 1 {
		require.NoError(t, rec.Reconcile(ctx))
		assert.Equal(t, agentstore.StateThinking, stateOf(t, st, "agent"))
	}

	require.NoError(t, rec.Reconcile(ctx))
	assert.Equal(t, agentstore.StateIdle, stateOf(t, st, "agent"))
}

// TestReconcileUnrecognizedFrameThatKeepsChangingHolds pins that a frame nothing recognizes
// but which keeps moving is work in progress rather than an idle pane.
func TestReconcileUnrecognizedFrameThatKeepsChangingHolds(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID

	rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents)

	tmux.captures["%1"] = unrecognizedFrame + "-0"
	require.NoError(t, rec.Reconcile(ctx))

	for i := 1; i <= reconcileMisses+2; i++ {
		tmux.captures["%1"] = fmt.Sprintf("%s-%d", unrecognizedFrame, i)
		require.NoError(t, rec.Reconcile(ctx))
		assert.Equal(t, agentstore.StateThinking, stateOf(t, st, "agent"))
		assert.Zero(t, rec.missCount(agent.ID))
	}
}

// TestReconcileMissesAccrueRatherThanRestartOnceTheHoldIsCapped pins that a frame changing
// forever stops resetting the miss counter, so the hold cannot run on without end.
func TestReconcileMissesAccrueRatherThanRestartOnceTheHoldIsCapped(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID

	rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents)

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
	assert.Equal(t, agentstore.StateIdle, stateOf(t, st, "agent"),
		"the accrued misses, not a restarted hold, are what finally demote it")
}

// TestReconcileCountsUnrecognizedFrames pins the running count of frames nothing recognized,
// which is what says a harness has changed its output.
func TestReconcileCountsUnrecognizedFrames(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.captures["%1"] = unrecognizedFrame

	rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents)
	require.NoError(t, rec.Reconcile(ctx))
	require.NoError(t, rec.Reconcile(ctx))

	assert.Equal(t, 2, rec.unrecognizedTotal)
}

// TestReconcileWithoutTmux covers a pass that could not read tmux at all, where treating
// the failure as an answer would clear every pane and demote every agent at once.
func TestReconcileWithoutTmux(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

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

	rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents)
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
			ctx := context.Background()

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

			rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}, only: []string{testHarnessID}}, st.Agents)
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
	ctx := context.Background()
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.captures["%1"] = busyFrame

	rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents)
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
			ctx := context.Background()
			agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

			tmux := newFakeTmux()

			resumed := agent
			resumed.Pane = tt.pane
			resumed.State = agentstore.StateIdle

			observer := &resumeAfterSnapshot{paneObserver: tmux, resume: func() {
				require.NoError(t, tmux.SetPaneOwner(tt.pane, agent.ID))
				require.NoError(t, st.Agents.Update(ctx, resumed))
			}}

			rec := NewReconciler(observer, fakeHarnesses{harness: &fakeHarness{}}, st.Agents)
			require.NoError(t, rec.Reconcile(ctx))

			after := agentByID(t, st, agent.ID)
			assert.Equal(t, tt.pane, after.Pane, "a pass holding a stale row does not forget the pane a resume just wrote")
			assert.Nil(t, after.InterruptedAt, "a resumed agent's turn was not cut off by the pass that was mid-walk")
		})
	}
}
