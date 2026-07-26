package store

import (
	"claude-agents/internal/pgtest"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetDistinguishesMissingFromError checks that Get separates a missing row
// from a real failure. A missing row is (nil, nil), and an actual failure such
// as a closed database surfaces as an error.
func TestGetDistinguishesMissingFromError(t *testing.T) {
	st, err := Open(t.Context(), pgtest.URL(t))
	require.NoError(t, err)
	ctx := context.Background()

	got, err := st.Get(ctx, "missing")
	require.NoError(t, err, "a missing row is not an error")
	assert.Nil(t, got)

	require.NoError(t, st.Close())

	_, err = st.Get(ctx, "anything")
	assert.Error(t, err, "a real DB failure must surface, not read as not-found")
}
