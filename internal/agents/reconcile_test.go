package agents

import (
	"context"
	"errors"
	"testing"
	"time"

	agentstore "weave/internal/store/agent"
	"weave/lib/xptr"

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

		owned bool

		command string
		frame   string
		want    string
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

// TestReconcileForgetsAnAbandonedPane covers what a dormant agent leaves behind. Weave's
// only liveness test is a pane lookup, so an agent that keeps a pane id it is not running
// in cannot be resumed and is typed at as though it were listening.
func TestReconcileForgetsAnAbandonedPane(t *testing.T) {
	tests := []struct {
		name string

		mine    bool
		command string

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

// TestReconcileWaitsOutALaunch covers the window a launch opens. Weave types the launch
// command into the pane's shell, so a pane that has just been opened is a shell holding a
// live agent, and reading that as an exit would forget an agent while it was starting.
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

// TestReconcileLeavesTheTag pins who the tag belongs to. The reconciler writes
// what it observes, and a tag is not something it can observe. The user sets it and
// only the user clears it, so a pass that moves an agent's state leaves it standing.
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
		name string

		passes []string

		archiveBeforeLast bool

		wantAfterFirst string
		wantBeforeLast string
		want           string

		wantMisses *int
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

// TestReconcileStampsWhatItConfirmed pins which passes count as having confirmed a state.
// A pass that read the frame did, whatever it read. A pass that could not read one leaves
// the stamp where it was, so an agent nothing can observe is never mistaken for an
// observed one.
func TestReconcileStampsWhatItConfirmed(t *testing.T) {
	tests := []struct {
		name string

		harness string

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

// TestReconcileCoalescesTheConfirmationStamp covers the cost of writing the stamp at all.
// Every pass confirms every agent it can read, so a stamp written each time would be an
// update per agent every couple of seconds, and the panel needs the fact far less precisely
// than that.
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
