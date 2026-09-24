package agents

import (
	"context"
	"testing"

	"weave/internal/harness/pane"
	"weave/internal/harnesstest"
	agentstore "weave/internal/store/agent"
	"weave/internal/tmuxtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingStates is the agent store with its liveness writes counted.
type countingStates struct {
	agentStateReadWriter
	aliveWrites int
}

func (c *countingStates) SetAlive(ctx context.Context, ids []string, alive bool) error {
	c.aliveWrites++

	return c.agentStateReadWriter.SetAlive(ctx, ids, alive)
}

// TestReconcileWritesLivenessAgainstTheRow pins that a pass writes whether an agent owns its
// pane only where the row says otherwise: a row that already agrees with tmux is left alone,
// whatever earlier passes saw.
func TestReconcileWritesLivenessAgainstTheRow(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	agent := seed(t, st, "agent", "%1", agentstore.StateIdle)
	require.True(t, agent.Alive, "a row written with a pane is alive")

	tmux := tmuxtest.NewFake()
	tmux.Owners["%1"] = agent.ID
	tmux.Captures["%1"] = harnesstest.FrameWorking
	states := &countingStates{agentStateReadWriter: st.Agents}
	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}}),
		States:   states,
	})

	require.NoError(t, rec.Reconcile(ctx))
	assert.Equal(t, 0, states.aliveWrites, "a pane the row already says is there is not written again")

	// A row that says dormant while the pane is there is what a service restart or a restore
	// leaves behind, and the first pass sets it right.
	require.NoError(t, st.Agents.SetAlive(ctx, []string{agent.ID}, false))
	require.NoError(t, rec.Reconcile(ctx))
	assert.Equal(t, 1, states.aliveWrites)
	assert.True(t, aliveOf(t, st, agent.ID), "a pane found where the row says none is written alive")

	require.NoError(t, rec.Reconcile(ctx))
	assert.Equal(t, 1, states.aliveWrites, "and the next pass finds the row agreeing")

	tmux.Owners["%1"] = ""
	require.NoError(t, rec.Reconcile(ctx))
	assert.Equal(t, 2, states.aliveWrites)
	assert.False(t, aliveOf(t, st, agent.ID), "a pane that went is written dormant")

	require.NoError(t, rec.Reconcile(ctx))
	assert.Equal(t, 2, states.aliveWrites, "a pane that stays gone is not written again")
}

func aliveOf(t *testing.T, st *testStore, id string) bool {
	t.Helper()

	agent, err := st.Agents.GetByID(t.Context(), id)
	require.NoError(t, err)
	require.NotNil(t, agent)

	return agent.Alive
}
