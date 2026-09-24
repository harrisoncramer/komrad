package checkouts

import (
	"context"
	"errors"
	"testing"

	"weave/internal/git"
	"weave/internal/gittest"
	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
	"weave/internal/storetest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCwdSyncerSyncRecordsAMoveSinceTheNewestRound pins the newest round's snapshot being written
// while the worktree no longer hashes to it.
func TestCwdSyncerSyncRecordsAMoveSinceTheNewestRound(t *testing.T) {
	tests := []struct {
		name         string
		newest       string
		snapshotTree string
		probe        string
		want         string
	}{
		{
			name:         "a round git cannot read records no move",
			newest:       "snapshot-one",
			snapshotTree: "",
			probe:        "tree-moved",
			want:         "",
		},
		{
			name:         "an agent with no round records no move",
			newest:       "",
			snapshotTree: "",
			probe:        "tree-moved",
			want:         "",
		},
		{
			name:         "a worktree that still hashes to its newest round records no move",
			newest:       "snapshot-one",
			snapshotTree: "tree-one",
			probe:        "tree-one",
			want:         "",
		},
		{
			name:         "a worktree that moved records its newest round",
			newest:       "snapshot-one",
			snapshotTree: "tree-one",
			probe:        "tree-moved",
			want:         "snapshot-one",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			cwd := fakeCheckout(t)
			agent := seedAgentAt(t, st, cwd)

			rounds := fakeRounds{}
			if tt.newest != "" {
				rounds.newest = []reviewstore.NewestRound{{AgentID: agent.ID, SnapshotSHA: tt.newest}}
			}
			syncer := NewCwdSyncer(NewCwdSyncerParams{
				Agents: st.Agents,
				Heads:  &gittest.Heads{States: map[string]git.HeadState{cwd: {Root: cwd, SnapshotTree: tt.snapshotTree}}},
				Trees:  &gittest.Trees{AllMoved: true, Probes: map[string]string{cwd: tt.probe}},
				Rounds: rounds,
				Bases:  fakeBases{},
			})

			require.NoError(t, syncer.Sync(t.Context()))

			assert.Equal(t, tt.want, readMovedSinceRound(t, st, agent.ID))
		})
	}
}

// TestCwdSyncerSyncHashesTheWorktreeOnceForBothRounds pins one probe measured against both the
// concluded round and the newest one.
func TestCwdSyncerSyncHashesTheWorktreeOnceForBothRounds(t *testing.T) {
	st := storetest.Open(t)
	cwd := fakeCheckout(t)
	agent := seedAgentAt(t, st, cwd)

	approved := reviewstore.VerdictApproved
	trees := &gittest.Trees{AllMoved: true, Probes: map[string]string{cwd: "tree-moved"}}
	syncer := NewCwdSyncer(NewCwdSyncerParams{
		Agents: st.Agents,
		Heads:  &gittest.Heads{States: map[string]git.HeadState{cwd: {Root: cwd, SnapshotTree: "tree-two"}}},
		Trees:  trees,
		Rounds: fakeRounds{
			tallies: []reviewstore.Tally{{
				AgentID:     agent.ID,
				Verdict:     &approved,
				SnapshotSHA: "snapshot-one",
			}},
			newest: []reviewstore.NewestRound{{AgentID: agent.ID, SnapshotSHA: "snapshot-two"}},
		},
		Bases: fakeBases{},
	})

	require.NoError(t, syncer.Sync(t.Context()))

	assert.Equal(t, []string{cwd}, trees.Probed, "the worktree is hashed once")
	assert.Equal(t, []string{"tree-moved"}, trees.Given, "and the standing reads that hash")
	assert.Equal(t, "snapshot-two", readMovedSinceRound(t, st, agent.ID))
	assert.Equal(t, git.StandingMoved, standingOn(t, st, agent.ID, "snapshot-one"))
}

// TestCwdSyncerSyncKeepsAMoveItCouldNotProbe pins that a worktree the pass cannot hash is said
// and leaves the move the row already records.
func TestCwdSyncerSyncKeepsAMoveItCouldNotProbe(t *testing.T) {
	st := storetest.Open(t)
	cwd := fakeCheckout(t)
	agent := seedAgentAt(t, st, cwd)

	trees := &gittest.Trees{AllMoved: true, Probes: map[string]string{cwd: "tree-moved"}}
	syncer := NewCwdSyncer(NewCwdSyncerParams{
		Agents: st.Agents,
		Heads:  &gittest.Heads{States: map[string]git.HeadState{cwd: {Root: cwd, SnapshotTree: "tree-one"}}},
		Trees:  trees,
		Rounds: fakeRounds{newest: []reviewstore.NewestRound{{AgentID: agent.ID, SnapshotSHA: "snapshot-one"}}},
		Bases:  fakeBases{},
	})

	require.NoError(t, syncer.Sync(t.Context()))
	require.Equal(t, "snapshot-one", readMovedSinceRound(t, st, agent.ID))

	trees.ProbeErr = errors.New("the index could not be read")
	err := syncer.Sync(t.Context())

	require.Error(t, err)
	assert.Contains(t, err.Error(), `failed to hash the worktree of "a"`)
	assert.Equal(t, "snapshot-one", readMovedSinceRound(t, st, agent.ID))
}

// fakeRounds is what the review store would list of every agent's rounds.
type fakeRounds struct {
	tallies []reviewstore.Tally
	newest  []reviewstore.NewestRound
}

func (f fakeRounds) ListTallies(context.Context) ([]reviewstore.Tally, error) {
	return f.tallies, nil
}

func (f fakeRounds) ListNewestRounds(context.Context) ([]reviewstore.NewestRound, error) {
	return f.newest, nil
}

// seedAgentAt is an idle agent named a whose cwd is the given directory.
func seedAgentAt(t *testing.T, st *storetest.Stores, cwd string) *agentstore.Agent {
	t.Helper()

	workflowID := seedWorkflow(t, st)
	require.NoError(t, st.Agents.Create(t.Context(), agentstore.Agent{
		Name:       "a",
		State:      agentstore.StateIdle,
		WorkflowID: workflowID,
		Cwd:        cwd,
	}))

	agent, err := st.Agents.GetInWorkflow(t.Context(), workflowID, "a")
	require.NoError(t, err)
	require.NotNil(t, agent)

	return agent
}

// readMovedSinceRound is the round the row says the worktree has moved since.
func readMovedSinceRound(t *testing.T, st *storetest.Stores, id string) string {
	t.Helper()

	agent, err := st.Agents.GetByID(t.Context(), id)
	require.NoError(t, err)
	require.NotNil(t, agent)

	return agent.MovedSinceRound
}
