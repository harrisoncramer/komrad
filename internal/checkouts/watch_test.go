package checkouts

import (
	"testing"

	"weave/internal/git"
	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
	"weave/internal/storetest"
	"weave/internal/watchtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCwdSyncerTouchesAWorktreeThatMoved pins that a pass names the agents whose worktree it
// found standing somewhere new against their round, which no write records, and nothing on a
// pass that found them where the last one did.
func TestCwdSyncerTouchesAWorktreeThatMoved(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	dir := fakeCheckout(t)
	require.NoError(t, st.Agents.Create(ctx, agentstore.Agent{Name: "a", State: agentstore.StateIdle, WorkflowID: workflowID, Cwd: dir}))
	agent, err := st.Agents.GetInWorkflow(ctx, workflowID, "a")
	require.NoError(t, err)

	approved := reviewstore.VerdictApproved
	trees := fakeTrees{dir: git.StandingSame}
	moved := watchtest.NewRecorder()
	syncer := NewCwdSyncer(NewCwdSyncerParams{
		Agents:  st.Agents,
		Heads:   fakeHeads{dir: {Branch: "feature/x"}},
		Trees:   trees,
		Tallies: fakeTallies{{AgentID: agent.ID, Verdict: &approved, SnapshotSHA: "snapshot"}},
		Bases:   fakeBases{},
		Watch:   moved,
	})

	require.NoError(t, syncer.Sync(ctx))
	assert.Equal(t, []string{agent.ID}, moved.Agents(), "the first pass finds a standing where there was none")

	moved.Reset()
	require.NoError(t, syncer.Sync(ctx))
	assert.Empty(t, moved.Agents(), "a pass that finds the worktree where it was says nothing")

	trees[dir] = git.StandingMoved
	require.NoError(t, syncer.Sync(ctx))
	assert.Equal(t, []string{agent.ID}, moved.Agents(), "a worktree that moved against its round is named")
}
