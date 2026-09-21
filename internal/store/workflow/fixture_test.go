package workflow

import (
	"testing"

	"weave/internal/dbtest"
	"weave/internal/store"
	"weave/internal/watchtest"

	"github.com/stretchr/testify/require"
)

const missingID = "00000000-0000-0000-0000-000000000000"

func open(t *testing.T) *WorkflowStore {
	t.Helper()

	pool, err := store.Open(t.Context(), store.OpenParams{URL: dbtest.URL(t)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	return NewWorkflowStore(pool.GetDB())
}

// openWatched is a store whose touches are recorded.
func openWatched(t *testing.T) (*WorkflowStore, *watchtest.Recorder) {
	t.Helper()

	touched := watchtest.NewRecorder()
	pool, err := store.Open(t.Context(), store.OpenParams{URL: dbtest.URL(t), Publisher: touched})
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	return NewWorkflowStore(pool.GetDB()), touched
}
