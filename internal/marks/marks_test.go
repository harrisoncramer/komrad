package marks

import (
	"testing"

	"weave/internal/storetest"
	"weave/internal/watchtest"

	agentstore "weave/internal/store/agent"
	markstore "weave/internal/store/mark"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMarksPublishEveryRowALetterMovedBetween pins that the row taking a letter, the row that
// held that letter, and the row the target answered to before all read as moved.
func TestMarksPublishEveryRowALetterMovedBetween(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()
	watch := watchtest.NewRecorder()
	service := NewMarkService(NewMarkServiceParams{Marks: st.Marks, Tx: st.Pool, Watch: watch})

	alpha := st.SeedAgent(t, agentstore.Agent{Name: "alpha"})
	beta := st.SeedAgent(t, agentstore.Agent{Name: "beta"})
	backend := st.SeedWorkflow(t, workflowstore.Config{Name: "backend", Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"}})

	marked, err := service.SetMark(ctx, "a", markstore.OnAgent(alpha.ID))
	require.NoError(t, err)
	require.True(t, marked)
	assert.Equal(t, []string{alpha.ID}, watch.Agents())

	watch.Reset()
	marked, err = service.SetMark(ctx, "b", markstore.OnWorkflow(backend.ID))
	require.NoError(t, err)
	require.True(t, marked)
	assert.Equal(t, []string{backend.ID}, watch.Workflows())

	// The letter leaves alpha, the workflow loses its own letter, and beta takes it.
	watch.Reset()
	marked, err = service.SetMark(ctx, "a", markstore.OnAgent(beta.ID))
	require.NoError(t, err)
	require.True(t, marked)
	assert.ElementsMatch(t, []string{alpha.ID, beta.ID}, watch.Agents())

	watch.Reset()
	cleared, err := service.ClearMark(ctx, markstore.OnAgent(beta.ID))
	require.NoError(t, err)
	require.True(t, cleared)
	assert.Equal(t, []string{beta.ID}, watch.Agents())
}
