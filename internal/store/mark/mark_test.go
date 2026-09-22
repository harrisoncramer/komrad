package mark

import (
	"testing"
	"time"

	"weave/internal/dbtest"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fixture struct {
	*MarkStore
	url       string
	agents    *agentstore.AgentStore
	workflows *workflowstore.WorkflowStore
	workflow  string
}

func open(t *testing.T) *fixture {
	t.Helper()

	url := dbtest.URL(t)
	pool, err := store.Open(t.Context(), url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	workflows := workflowstore.NewWorkflowStore(pool.GetDB())
	created, err := workflows.Create(t.Context(), workflowstore.Config{Name: "default", Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"}})
	require.NoError(t, err)

	return &fixture{
		MarkStore: NewMarkStore(pool.GetDB()),
		url:       url,
		agents:    agentstore.NewAgentStore(pool.GetDB()),
		workflows: workflows,
		workflow:  created.ID,
	}
}

func (f *fixture) seedAgent(t *testing.T, name string) string {
	t.Helper()

	id := uuid.NewString()
	require.NoError(t, f.agents.Create(t.Context(), agentstore.Agent{
		ID:         id,
		Name:       name,
		Cwd:        "/repo",
		State:      agentstore.StateIdle,
		WorkflowID: f.workflow,
	}))

	return id
}

func (f *fixture) seedWorkflow(t *testing.T, name string) string {
	t.Helper()

	created, err := f.workflows.Create(t.Context(), workflowstore.Config{Name: name, Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"}})
	require.NoError(t, err)

	return created.ID
}

// set is the pair the service runs in a transaction, which every test here goes through so
// none of them can claim a letter the real caller would have freed first.
func (f *fixture) set(t *testing.T, mark string, target Target) bool {
	t.Helper()

	require.NoError(t, f.ClearConflicts(t.Context(), mark, target))
	marked, err := f.Set(t.Context(), mark, target)
	require.NoError(t, err)

	return marked
}

func (f *fixture) marks(t *testing.T) map[string]string {
	t.Helper()

	listed, err := f.List(t.Context())
	require.NoError(t, err)

	by := make(map[string]string, len(listed))
	for _, mark := range listed {
		switch {
		case mark.AgentID != nil:
			by[*mark.AgentID] = mark.Mark
		case mark.WorkflowID != nil:
			by[*mark.WorkflowID] = mark.Mark
		}
	}

	return by
}

// TestSetMarksAnAgent covers the ordinary case and the two things a letter has to obey: one
// letter to one row, and one row to one letter.
func TestSetMarksAnAgent(t *testing.T) {
	st := open(t)
	alpha := st.seedAgent(t, "alpha")

	assert.True(t, st.set(t, "a", OnAgent(alpha)))
	assert.Equal(t, map[string]string{alpha: "a"}, st.marks(t))
}

// TestSetMarksAWorkflow covers a header taking a letter.
func TestSetMarksAWorkflow(t *testing.T) {
	st := open(t)
	backend := st.seedWorkflow(t, "backend")

	assert.True(t, st.set(t, "a", OnWorkflow(backend)))
	assert.Equal(t, map[string]string{backend: "a"}, st.marks(t))
}

// TestALetterMovesRatherThanDoubling pins the letter leaving the row that held it, across the two
// kinds of row.
func TestALetterMovesRatherThanDoubling(t *testing.T) {
	tests := []struct {
		name  string
		first func(t *testing.T, st *fixture) (string, Target)
		then  func(t *testing.T, st *fixture) (string, Target)
	}{
		{
			name: "from one agent to another",
			first: func(t *testing.T, st *fixture) (string, Target) {
				id := st.seedAgent(t, "alpha")
				return id, OnAgent(id)
			},
			then: func(t *testing.T, st *fixture) (string, Target) {
				id := st.seedAgent(t, "beta")
				return id, OnAgent(id)
			},
		},
		{
			name: "from an agent to a workflow",
			first: func(t *testing.T, st *fixture) (string, Target) {
				id := st.seedAgent(t, "alpha")
				return id, OnAgent(id)
			},
			then: func(t *testing.T, st *fixture) (string, Target) {
				id := st.seedWorkflow(t, "backend")

				return id, OnWorkflow(id)
			},
		},
		{
			name: "from a workflow to an agent",
			first: func(t *testing.T, st *fixture) (string, Target) {
				id := st.seedWorkflow(t, "backend")

				return id, OnWorkflow(id)
			},
			then: func(t *testing.T, st *fixture) (string, Target) {
				id := st.seedAgent(t, "alpha")
				return id, OnAgent(id)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			held, first := tt.first(t, st)
			taking, second := tt.then(t, st)

			require.True(t, st.set(t, "a", first))
			require.True(t, st.set(t, "a", second))

			assert.Equal(t, map[string]string{taking: "a"}, st.marks(t), "the row that held it is left with nothing")
			assert.NotContains(t, st.marks(t), held)
		})
	}
}

// TestARowHoldsOneLetter pins a second letter replacing the first rather than sitting beside it
// in the gutter's one cell.
func TestARowHoldsOneLetter(t *testing.T) {
	st := open(t)
	alpha := st.seedAgent(t, "alpha")

	require.True(t, st.set(t, "a", OnAgent(alpha)))
	require.True(t, st.set(t, "b", OnAgent(alpha)))

	assert.Equal(t, map[string]string{alpha: "b"}, st.marks(t))
}

// TestSetRefusesARowThatIsNotLive pins an archived row taking no letter, and taking none off
// the row that legitimately holds it.
func TestSetRefusesARowThatIsNotLive(t *testing.T) {
	t.Run("an archived agent", func(t *testing.T) {
		st := open(t)
		holder := st.seedAgent(t, "alpha")
		gone := st.seedAgent(t, "beta")

		require.True(t, st.set(t, "a", OnAgent(holder)))
		_, err := st.agents.Archive(t.Context(), []string{gone})
		require.NoError(t, err)

		assert.False(t, st.set(t, "a", OnAgent(gone)))
		assert.Equal(t, map[string]string{holder: "a"}, st.marks(t), "the agent holding the letter keeps it")
	})

	t.Run("an id no row holds", func(t *testing.T) {
		st := open(t)

		assert.False(t, st.set(t, "a", OnAgent(uuid.NewString())))
		assert.Empty(t, st.marks(t))
	})
}

// TestClearTakesTheLetterOffARow covers the second press of the key that set it.
func TestClearTakesTheLetterOffARow(t *testing.T) {
	st := open(t)
	alpha := st.seedAgent(t, "alpha")
	beta := st.seedAgent(t, "beta")

	require.True(t, st.set(t, "a", OnAgent(alpha)))
	require.True(t, st.set(t, "b", OnAgent(beta)))

	cleared, err := st.Clear(t.Context(), OnAgent(alpha))
	require.NoError(t, err)
	assert.True(t, cleared)
	assert.Equal(t, map[string]string{beta: "b"}, st.marks(t), "and leaves the letters on other rows alone")

	again, err := st.Clear(t.Context(), OnAgent(alpha))
	require.NoError(t, err)
	assert.False(t, again, "a row holding nothing says so")
}

// TestArchivingFreesTheLetter pins the letter coming off as the row leaves the screen, through
// every path that takes one off.
func TestArchivingFreesTheLetter(t *testing.T) {
	t.Run("archiving agents by id", func(t *testing.T) {
		st := open(t)
		alpha := st.seedAgent(t, "alpha")
		require.True(t, st.set(t, "a", OnAgent(alpha)))

		_, err := st.agents.Archive(t.Context(), []string{alpha})
		require.NoError(t, err)
		assert.Empty(t, st.marks(t))
	})

	t.Run("archiving an agent that has not changed", func(t *testing.T) {
		st := open(t)
		alpha := st.seedAgent(t, "alpha")
		require.True(t, st.set(t, "a", OnAgent(alpha)))

		current, err := st.agents.GetByID(t.Context(), alpha)
		require.NoError(t, err)
		require.NotNil(t, current)

		archived, err := st.agents.ArchiveIfUnchanged(t.Context(), agentstore.ArchiveIfUnchangedParams{
			ID:        current.ID,
			UpdatedAt: current.UpdatedAt,
		})
		require.NoError(t, err)
		require.True(t, archived)
		assert.Empty(t, st.marks(t))
	})

	t.Run("archiving every agent of a workflow", func(t *testing.T) {
		st := open(t)
		alpha := st.seedAgent(t, "alpha")
		require.True(t, st.set(t, "a", OnAgent(alpha)))

		_, err := st.agents.ArchiveWorkflowAgents(t.Context(), st.workflow)
		require.NoError(t, err)
		assert.Empty(t, st.marks(t))
	})

	t.Run("an archive the row moved under keeps the letter, since the row keeps running", func(t *testing.T) {
		st := open(t)
		alpha := st.seedAgent(t, "alpha")
		require.True(t, st.set(t, "a", OnAgent(alpha)))

		current, err := st.agents.GetByID(t.Context(), alpha)
		require.NoError(t, err)
		require.NotNil(t, current)

		archived, err := st.agents.ArchiveIfUnchanged(t.Context(), agentstore.ArchiveIfUnchangedParams{
			ID:        current.ID,
			UpdatedAt: current.UpdatedAt.Add(-time.Second),
		})
		require.NoError(t, err)
		require.False(t, archived, "the row moved, so the archive is refused")
		assert.Equal(t, map[string]string{alpha: "a"}, st.marks(t))
	})

	t.Run("archiving the workflow itself", func(t *testing.T) {
		st := open(t)
		require.True(t, st.set(t, "a", OnWorkflow(st.workflow)))

		require.NoError(t, st.workflows.Archive(t.Context(), st.workflow))
		assert.Empty(t, st.marks(t))
	})
}

// TestDeletingARowTakesItsLetter pins the cascade, so a letter cannot outlive the row it named.
func TestDeletingARowTakesItsLetter(t *testing.T) {
	st := open(t)
	alpha := st.seedAgent(t, "alpha")
	require.True(t, st.set(t, "a", OnAgent(alpha)))

	_, err := st.agents.Delete(t.Context(), []string{alpha})
	require.NoError(t, err)
	assert.Empty(t, st.marks(t))
}
