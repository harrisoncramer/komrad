package workflow

import (
	"testing"

	"weave/internal/dbtest"
	"weave/internal/store"

	"github.com/stretchr/testify/require"
)

const missingID = "00000000-0000-0000-0000-000000000000"

func open(t *testing.T) *Store {
	t.Helper()

	pool, err := store.Open(t.Context(), dbtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	return New(pool.GetDB())
}
