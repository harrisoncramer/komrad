package agents

import (
	"testing"

	"weave/internal/harness/pane"
	agentstore "weave/internal/store/agent"
	"weave/internal/watchtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReconcileTouchesAPaneThatCameOrWent pins that a pass names the agents whose pane came or
// went since the last one, which no write records, and nothing while the pane stays.
func TestReconcileTouchesAPaneThatCameOrWent(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "agent", "%1", agentstore.StateIdle)
	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.captures["%1"] = busyFrame
	flipped := watchtest.NewRecorder()
	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
		States:   st.Agents,
		Watch:    flipped,
	})

	require.NoError(t, rec.Reconcile(t.Context()))
	require.NoError(t, rec.Reconcile(t.Context()))
	assert.Empty(t, flipped.Agents(), "a pane that stays is not a flip")

	tmux.owners["%1"] = ""
	require.NoError(t, rec.Reconcile(t.Context()))
	assert.Equal(t, []string{agent.ID}, flipped.Agents(), "the pane going is said once")
	assert.Contains(t, st.Touched.Agents(), agent.ID, "and the write the pass made is said by the store")
}
