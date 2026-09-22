package agent

import (
	"context"
	"testing"

	"weave/internal/git"
	"weave/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAliveFollowsThePane pins that every write of the pane writes liveness beside it: a pane
// given makes the row alive, and every write that clears the pane makes it dormant.
func TestAliveFollowsThePane(t *testing.T) {
	tests := []struct {
		name  string
		clear func(ctx context.Context, st *fixture, agent Agent) error
	}{
		{
			name: "an update with no pane",
			clear: func(ctx context.Context, st *fixture, agent Agent) error {
				agent.Pane = ""

				return st.Update(ctx, agent)
			},
		},
		{
			name: "a live update with no pane",
			clear: func(ctx context.Context, st *fixture, agent Agent) error {
				agent.Pane = ""
				_, err := st.UpdateIfLive(ctx, agent)

				return err
			},
		},
		{
			name: "forgetting the pane",
			clear: func(ctx context.Context, st *fixture, agent Agent) error {
				_, err := st.ForgetPane(ctx, ForgetPaneParams{ID: agent.ID, Pane: agent.Pane, UpdatedAt: agent.UpdatedAt})

				return err
			},
		},
		{
			name: "archiving",
			clear: func(ctx context.Context, st *fixture, agent Agent) error {
				_, err := st.Archive(ctx, []string{agent.ID})

				return err
			},
		},
		{
			name: "archiving an unchanged row",
			clear: func(ctx context.Context, st *fixture, agent Agent) error {
				_, err := st.ArchiveIfUnchanged(ctx, ArchiveIfUnchangedParams{ID: agent.ID, UpdatedAt: agent.UpdatedAt})

				return err
			},
		},
		{
			name: "archiving the workflow",
			clear: func(ctx context.Context, st *fixture, agent Agent) error {
				_, err := st.ArchiveWorkflowAgents(ctx, agent.WorkflowID)

				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			require.NoError(t, st.Create(ctx, Agent{Name: "dormant", State: StateIdle}))
			dormant, err := st.Get(ctx, "dormant")
			require.NoError(t, err)
			assert.False(t, dormant.Alive, "a row created with no pane is dormant")

			require.NoError(t, st.Create(ctx, Agent{Name: "agent", Pane: "%1", State: StateThinking}))
			created, err := st.Get(ctx, "agent")
			require.NoError(t, err)
			assert.True(t, created.Alive, "a row created with a pane is alive")

			require.NoError(t, tt.clear(ctx, st, *created))

			cleared, err := st.GetByID(ctx, created.ID)
			require.NoError(t, err)
			require.NotNil(t, cleared)
			assert.Empty(t, cleared.Pane)
			assert.False(t, cleared.Alive, "a row whose pane was cleared is dormant")
		})
	}
}

// TestSetAlive covers the reconciler's write: the named rows and no other, either way.
func TestSetAlive(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	for _, name := range []string{"a", "b", "c"} {
		require.NoError(t, st.Create(ctx, Agent{Name: name, Pane: "%" + name, State: StateIdle}))
	}
	a, err := st.Get(ctx, "a")
	require.NoError(t, err)
	b, err := st.Get(ctx, "b")
	require.NoError(t, err)

	require.NoError(t, st.SetAlive(ctx, []string{a.ID, b.ID, "not-a-uuid"}, false))

	alive := map[string]bool{}
	for _, name := range []string{"a", "b", "c"} {
		agent, err := st.Get(ctx, name)
		require.NoError(t, err)
		alive[name] = agent.Alive
	}
	assert.Equal(t, map[string]bool{"a": false, "b": false, "c": true}, alive)

	require.NoError(t, st.SetAlive(ctx, []string{a.ID}, true))
	a, err = st.Get(ctx, "a")
	require.NoError(t, err)
	assert.True(t, a.Alive)

	require.NoError(t, st.SetAlive(ctx, nil, true), "no ids is nothing to write")
}

// TestSetTreeStanding covers the cwd sync's write: the standing and the round it was measured
// against ride the row together.
func TestSetTreeStanding(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	assert.Equal(t, git.StandingSame, created.TreeStanding)
	assert.Empty(t, created.TreeStandingSHA)

	require.NoError(t, st.SetTreeStanding(ctx, created.ID, git.StandingLandedMoved, "snapshot-one"))

	measured, err := st.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, measured)
	assert.Equal(t, git.StandingLandedMoved, measured.TreeStanding)
	assert.Equal(t, "snapshot-one", measured.TreeStandingSHA)

	require.NoError(t, st.SetTreeStanding(ctx, created.ID, git.StandingSame, ""))

	rested, err := st.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, rested)
	assert.Equal(t, git.StandingSame, rested.TreeStanding)
	assert.Empty(t, rested.TreeStandingSHA)

	require.ErrorIs(t, st.SetTreeStanding(ctx, "not-a-uuid", git.StandingMoved, "snapshot-one"), store.ErrNotFound)
}
