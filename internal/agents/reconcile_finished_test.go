package agents

import (
	"context"
	"testing"
	"time"

	agentstore "weave/internal/store/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func agentOf(t *testing.T, st *testStore) agentstore.Agent {
	t.Helper()
	got, err := st.Get(context.Background(), "agent")
	require.NoError(t, err)
	require.NotNil(t, got)

	return *got
}

// watchIdle runs the passes it takes for a pane the harness reads as idle to settle, against an
// agent whose pane is its own, which is the transition a finish is recorded from.
func watchIdle(t *testing.T, rec *Reconciler, tmux *fakeTmux) {
	t.Helper()

	tmux.captures["%1"] = idleFrame
	for range reconcileMisses {
		require.NoError(t, rec.Reconcile(context.Background()))
	}
}

func ownedReconciler(t *testing.T, st *testStore, agent agentstore.Agent) (*Reconciler, *fakeTmux) {
	t.Helper()

	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID

	return NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents), tmux
}

// TestReconcileStampsTheTurnItWatchedFinish covers the whole point of the mark: an agent that
// stops working while nobody is looking has to leave something behind saying so.
func TestReconcileStampsTheTurnItWatchedFinish(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)
	rec, tmux := ownedReconciler(t, st, agent)

	watchIdle(t, rec, tmux)

	finished := agentOf(t, st)
	require.Equal(t, agentstore.StateIdle, finished.State)
	require.NotNil(t, finished.FinishedAt)
	assert.Nil(t, finished.InterruptedAt, "the agent finished rather than being cut off")
	assert.True(t, finished.Unviewed())
}

// TestReconcileStampsOnceUntilTheAgentWorksAgain pins that a row sitting idle pass after pass
// keeps the finish it already has, since a moving stamp would make a mark impossible to clear.
func TestReconcileStampsOnceUntilTheAgentWorksAgain(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)
	rec, tmux := ownedReconciler(t, st, agent)

	watchIdle(t, rec, tmux)
	first := agentOf(t, st)
	require.NotNil(t, first.FinishedAt)

	require.NoError(t, rec.Reconcile(context.Background()))
	assert.Equal(t, first.FinishedAt.UTC(), agentOf(t, st).FinishedAt.UTC())

	tmux.captures["%1"] = busyFrame
	require.NoError(t, rec.Reconcile(context.Background()))
	require.Equal(t, agentstore.StateThinking, agentOf(t, st).State)

	watchIdle(t, rec, tmux)
	assert.True(t, agentOf(t, st).FinishedAt.After(*first.FinishedAt),
		"a second turn that ends is a second thing to read")
}

// TestReconcileStampsNothingForAPaneThatWentAway covers the agent nobody wants to be sent back
// to, one whose pane died or was taken over part-way through a turn: it is written idle because
// nothing is running, but it finished nothing, and a mark earned that way could never be cleared
// at all, since a dormant row stages a placeholder rather than a pane.
func TestReconcileStampsNothingForAPaneThatWentAway(t *testing.T) {
	tests := []struct {
		name    string
		command string
	}{
		{name: "the pane is not this agent's any more"},
		{name: "the pane went back to a bare shell", command: "zsh"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

			tmux := newFakeTmux()
			if tt.command != "" {
				tmux.owners["%1"] = agent.ID
				tmux.commands["%1"] = tt.command
			}

			rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents)
			at := time.Now()
			rec.now = func() time.Time { return at }

			// A pane showing a shell is given the grace a CLI gets to start before it counts.
			require.NoError(t, rec.Reconcile(context.Background()))
			at = at.Add(reconcileShellGrace + time.Second)
			require.NoError(t, rec.Reconcile(context.Background()))

			after := agentOf(t, st)
			require.Equal(t, agentstore.StateIdle, after.State)
			require.Empty(t, after.Pane, "the pane is gone")
			assert.Nil(t, after.FinishedAt, "a turn that was cut off is not a turn that ended")
			assert.False(t, after.Unviewed())
		})
	}
}

// TestSetStateByIDDoesNotStampAFinish pins the hazard the stamp is placed around: a row is also
// written idle when it is created and when a resume rebuilds its pane, and neither of those is
// an agent finishing anything.
func TestSetStateByIDDoesNotStampAFinish(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	require.NoError(t, st.Agents.SetStateByID(context.Background(), agent.ID, agentstore.StateIdle))

	after := agentOf(t, st)
	assert.Nil(t, after.FinishedAt)
	assert.False(t, after.Unviewed())
}
