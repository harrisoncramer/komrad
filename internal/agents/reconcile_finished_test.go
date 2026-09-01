package agents

import (
	"context"
	"testing"

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

// TestReconcileStampsTheTurnItWatchedFinish covers the whole point of the mark: an agent that
// stops working while nobody is looking has to leave something behind saying so.
func TestReconcileStampsTheTurnItWatchedFinish(t *testing.T) {
	st := newStore(t)
	seed(t, st, "agent", "%1", agentstore.StateThinking)

	rec := NewReconciler(newFakeTmux(), fakeHarnesses{harness: &fakeHarness{}}, st.Agents)
	require.NoError(t, rec.Reconcile(context.Background()))

	finished := agentOf(t, st)
	require.Equal(t, agentstore.StateIdle, finished.State)
	require.NotNil(t, finished.FinishedAt)
	assert.True(t, finished.Unviewed())
}

// TestReconcileStampsOnceUntilTheAgentWorksAgain pins that a row sitting idle pass after pass
// keeps the finish it already has, since a moving stamp would make a mark impossible to clear.
func TestReconcileStampsOnceUntilTheAgentWorksAgain(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "agent", "%1", agentstore.StateThinking)

	rec := NewReconciler(newFakeTmux(), fakeHarnesses{harness: &fakeHarness{}}, st.Agents)
	require.NoError(t, rec.Reconcile(context.Background()))
	first := agentOf(t, st)
	require.NotNil(t, first.FinishedAt)

	require.NoError(t, rec.Reconcile(context.Background()))
	assert.Equal(t, first.FinishedAt.UTC(), agentOf(t, st).FinishedAt.UTC())

	require.NoError(t, st.Agents.SetStateByID(context.Background(), agent.ID, agentstore.StateThinking))
	require.NoError(t, rec.Reconcile(context.Background()))
	assert.True(t, agentOf(t, st).FinishedAt.After(*first.FinishedAt),
		"a second turn that ends is a second thing to read")
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
