package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetDistinguishesMissingFromError pins the fix that Get no longer reports a
// real DB failure as "not found": a missing row is (nil, nil), but an actual
// failure (here, a closed database) surfaces as an error rather than being
// swallowed into a false not-found.
func TestGetDistinguishesMissingFromError(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "agents.db"))
	require.NoError(t, err)
	ctx := context.Background()

	got, err := st.Get(ctx, "missing")
	require.NoError(t, err, "a missing row is not an error")
	assert.Nil(t, got)

	require.NoError(t, st.Close())

	_, err = st.Get(ctx, "anything")
	assert.Error(t, err, "a real DB failure must surface, not read as not-found")
}
