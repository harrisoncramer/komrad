package agent

import (
	"testing"

	"weave/lib/xptr"

	"github.com/google/uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetMark pins that a mark lands on one live agent at a time and that setting a letter
// takes it off whichever agent held it, since two rows answering to one letter is a jump with
// no answer.
func TestSetMark(t *testing.T) {
	t.Run("an id no agent holds", func(t *testing.T) {
		st := open(t)
		ctx := t.Context()

		result, err := st.SetMark(ctx, uuid.NewString(), xptr.Ptr("a"))
		require.NoError(t, err)
		assert.False(t, result.Marked)
	})

	t.Run("a value that is not an id at all", func(t *testing.T) {
		st := open(t)
		ctx := t.Context()

		result, err := st.SetMark(ctx, "not-an-id", xptr.Ptr("a"))
		require.NoError(t, err)
		assert.False(t, result.Marked)
	})

	t.Run("an archived agent, which has no row to be marked on", func(t *testing.T) {
		st := open(t)
		ctx := t.Context()
		agent := createMarkAgent(t, st, "alpha")
		require.NoError(t, st.archiveByName(ctx, agent.Name))

		result, err := st.SetMark(ctx, agent.ID, xptr.Ptr("a"))
		require.NoError(t, err)
		assert.False(t, result.Marked)
	})

	t.Run("marking a live agent", func(t *testing.T) {
		st := open(t)
		ctx := t.Context()
		agent := createMarkAgent(t, st, "alpha")

		result, err := st.SetMark(ctx, agent.ID, xptr.Ptr("a"))
		require.NoError(t, err)
		assert.True(t, result.Marked)
		assert.Empty(t, result.ClearedFrom)
		assert.Equal(t, xptr.Ptr("a"), markOf(t, st, agent.ID))
	})

	t.Run("a letter another agent holds moves to the agent named", func(t *testing.T) {
		st := open(t)
		ctx := t.Context()
		held := createMarkAgent(t, st, "alpha")
		taking := createMarkAgent(t, st, "beta")

		_, err := st.SetMark(ctx, held.ID, xptr.Ptr("a"))
		require.NoError(t, err)

		result, err := st.SetMark(ctx, taking.ID, xptr.Ptr("a"))
		require.NoError(t, err)
		assert.True(t, result.Marked)
		assert.Equal(t, []string{held.Name}, result.ClearedFrom)
		assert.Nil(t, markOf(t, st, held.ID))
		assert.Equal(t, xptr.Ptr("a"), markOf(t, st, taking.ID))
	})

	t.Run("setting the letter an agent already holds says nothing was taken off anyone", func(t *testing.T) {
		st := open(t)
		ctx := t.Context()
		agent := createMarkAgent(t, st, "alpha")

		_, err := st.SetMark(ctx, agent.ID, xptr.Ptr("a"))
		require.NoError(t, err)

		result, err := st.SetMark(ctx, agent.ID, xptr.Ptr("a"))
		require.NoError(t, err)
		assert.True(t, result.Marked)
		assert.Empty(t, result.ClearedFrom)
		assert.Equal(t, xptr.Ptr("a"), markOf(t, st, agent.ID))
	})

	t.Run("an unset mark clears the agent's own letter and leaves the others alone", func(t *testing.T) {
		st := open(t)
		ctx := t.Context()
		clearing := createMarkAgent(t, st, "alpha")
		other := createMarkAgent(t, st, "beta")

		_, err := st.SetMark(ctx, clearing.ID, xptr.Ptr("a"))
		require.NoError(t, err)
		_, err = st.SetMark(ctx, other.ID, xptr.Ptr("b"))
		require.NoError(t, err)

		result, err := st.SetMark(ctx, clearing.ID, nil)
		require.NoError(t, err)
		assert.True(t, result.Marked)
		assert.Empty(t, result.ClearedFrom)
		assert.Nil(t, markOf(t, st, clearing.ID))
		assert.Equal(t, xptr.Ptr("b"), markOf(t, st, other.ID))
	})

	t.Run("an agent archived since the row was drawn takes the letter off nobody", func(t *testing.T) {
		st := open(t)
		ctx := t.Context()
		holder := createMarkAgent(t, st, "alpha")
		gone := createMarkAgent(t, st, "beta")

		_, err := st.SetMark(ctx, holder.ID, xptr.Ptr("a"))
		require.NoError(t, err)
		require.NoError(t, st.archiveByName(ctx, gone.Name))

		result, err := st.SetMark(ctx, gone.ID, xptr.Ptr("a"))
		require.NoError(t, err)
		assert.False(t, result.Marked)
		assert.Empty(t, result.ClearedFrom)
		assert.Equal(t, xptr.Ptr("a"), markOf(t, st, holder.ID), "the agent that holds the letter keeps it")
	})

	t.Run("archiving a marked agent frees the letter", func(t *testing.T) {
		st := open(t)
		ctx := t.Context()
		archiving := createMarkAgent(t, st, "alpha")
		taking := createMarkAgent(t, st, "beta")

		_, err := st.SetMark(ctx, archiving.ID, xptr.Ptr("a"))
		require.NoError(t, err)
		require.NoError(t, st.archiveByName(ctx, archiving.Name))
		assert.Nil(t, markOf(t, st, archiving.ID))

		result, err := st.SetMark(ctx, taking.ID, xptr.Ptr("a"))
		require.NoError(t, err)
		assert.True(t, result.Marked)
		assert.Empty(t, result.ClearedFrom, "an archived agent is not one the letter was taken off")
	})

	t.Run("archiving one agent of a workflow frees its letter", func(t *testing.T) {
		st := open(t)
		ctx := t.Context()
		archiving := createMarkAgent(t, st, "alpha")

		_, err := st.SetMark(ctx, archiving.ID, xptr.Ptr("a"))
		require.NoError(t, err)

		_, err = st.ArchiveWorkflowAgents(ctx, archiving.WorkflowID)
		require.NoError(t, err)
		assert.Nil(t, markOf(t, st, archiving.ID))
	})

	t.Run("archiving an unchanged agent frees its letter", func(t *testing.T) {
		st := open(t)
		ctx := t.Context()
		archiving := createMarkAgent(t, st, "alpha")

		_, err := st.SetMark(ctx, archiving.ID, xptr.Ptr("a"))
		require.NoError(t, err)

		current, err := st.GetByID(ctx, archiving.ID)
		require.NoError(t, err)
		require.NotNil(t, current)

		archived, err := st.ArchiveIfUnchanged(ctx, ArchiveIfUnchangedParams{ID: current.ID, UpdatedAt: current.UpdatedAt})
		require.NoError(t, err)
		require.True(t, archived)
		assert.Nil(t, markOf(t, st, archiving.ID))
	})

	t.Run("a restored agent comes back unmarked, so its letter cannot be held twice", func(t *testing.T) {
		st := open(t)
		ctx := t.Context()
		restoring := createMarkAgent(t, st, "alpha")
		other := createMarkAgent(t, st, "beta")

		_, err := st.SetMark(ctx, restoring.ID, xptr.Ptr("a"))
		require.NoError(t, err)
		require.NoError(t, st.archiveByName(ctx, restoring.Name))

		_, err = st.SetMark(ctx, other.ID, xptr.Ptr("a"))
		require.NoError(t, err)

		require.NoError(t, st.Restore(ctx, []string{restoring.ID}))
		assert.Nil(t, markOf(t, st, restoring.ID))
		assert.Equal(t, xptr.Ptr("a"), markOf(t, st, other.ID))
	})

	t.Run("an archived row that kept its letter burns nothing", func(t *testing.T) {
		st := open(t)
		ctx := t.Context()
		stuck := createMarkAgent(t, st, "alpha")
		other := createMarkAgent(t, st, "beta")

		_, err := st.SetMark(ctx, stuck.ID, xptr.Ptr("a"))
		require.NoError(t, err)

		// Archiving clears the mark, so the state a missed archive path would leave has to be
		// written by hand to be tested at all.
		_, err = st.db.Exec(ctx, `UPDATE agents SET archived_at = NOW() WHERE id = $1`, stuck.ID)
		require.NoError(t, err)

		result, err := st.SetMark(ctx, other.ID, xptr.Ptr("a"))
		require.NoError(t, err, "the letter is still free to hand out")
		assert.True(t, result.Marked)

		require.NoError(t, st.Restore(ctx, []string{stuck.ID}), "and the row can still come back")
		assert.Nil(t, markOf(t, st, stuck.ID))
		assert.Equal(t, xptr.Ptr("a"), markOf(t, st, other.ID))
	})

	t.Run("an archived row of a workflow that kept its letter can come back too", func(t *testing.T) {
		st := open(t)
		ctx := t.Context()
		stuck := createMarkAgent(t, st, "alpha")

		_, err := st.SetMark(ctx, stuck.ID, xptr.Ptr("a"))
		require.NoError(t, err)

		_, err = st.db.Exec(ctx, `UPDATE agents SET archived_at = NOW() WHERE id = $1`, stuck.ID)
		require.NoError(t, err)

		other := createMarkAgent(t, st, "beta")
		_, err = st.SetMark(ctx, other.ID, xptr.Ptr("a"))
		require.NoError(t, err)

		require.NoError(t, st.RestoreWorkflowAgents(ctx, stuck.WorkflowID))
		assert.Nil(t, markOf(t, st, stuck.ID))
	})

	t.Run("a workflow restored whole comes back unmarked too", func(t *testing.T) {
		st := open(t)
		ctx := t.Context()
		restoring := createMarkAgent(t, st, "alpha")

		_, err := st.SetMark(ctx, restoring.ID, xptr.Ptr("a"))
		require.NoError(t, err)
		require.NoError(t, st.archiveByName(ctx, restoring.Name))

		require.NoError(t, st.RestoreWorkflowAgents(ctx, restoring.WorkflowID))
		assert.Nil(t, markOf(t, st, restoring.ID))
	})
}

func createMarkAgent(t *testing.T, st *fixture, name string) Agent {
	t.Helper()

	require.NoError(t, st.Create(t.Context(), Agent{Name: name, State: StateIdle}))

	got, err := st.Get(t.Context(), name)
	require.NoError(t, err)
	require.NotNil(t, got)

	return *got
}

func markOf(t *testing.T, st *fixture, id string) *string {
	t.Helper()

	after, err := st.GetByID(t.Context(), id)
	require.NoError(t, err)
	require.NotNil(t, after)

	return after.Mark
}

// TestSetHidden pins that a hide write reports what it landed on, so the panel never draws
// a row the caller believes it took away.
func TestSetHidden(t *testing.T) {
	tests := []struct {
		name        string
		create      bool
		archive     bool
		id          func(created Agent) string
		before      bool
		hidden      bool
		wantChanged bool
	}{
		{
			name:   "an id no agent holds",
			id:     func(Agent) string { return uuid.NewString() },
			hidden: true,
		},
		{
			name:   "a value that is not an id at all",
			id:     func(Agent) string { return "not-an-id" },
			hidden: true,
		},
		{
			name:    "an archived agent, which has no listing to be hidden from",
			create:  true,
			archive: true,
			id:      func(created Agent) string { return created.ID },
			hidden:  true,
		},
		{
			name:        "hiding a live agent",
			create:      true,
			id:          func(created Agent) string { return created.ID },
			hidden:      true,
			wantChanged: true,
		},
		{
			name:        "bringing a hidden agent back",
			create:      true,
			id:          func(created Agent) string { return created.ID },
			before:      true,
			wantChanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			var created Agent
			if tt.create {
				require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle}))
				got, err := st.Get(ctx, "agent")
				require.NoError(t, err)
				require.NotNil(t, got)
				created = *got
				assert.False(t, got.Hidden, "an agent starts out listed")
			}
			if tt.before {
				changed, err := st.SetHidden(ctx, created.ID, true)
				require.NoError(t, err)
				require.True(t, changed)
			}
			if tt.archive {
				require.NoError(t, st.archiveByName(ctx, "agent"))
			}

			changed, err := st.SetHidden(ctx, tt.id(created), tt.hidden)
			require.NoError(t, err)
			assert.Equal(t, tt.wantChanged, changed)

			if !tt.wantChanged {
				return
			}

			after, err := st.Get(ctx, "agent")
			require.NoError(t, err)
			require.NotNil(t, after)
			assert.Equal(t, tt.hidden, after.Hidden)
		})
	}
}
