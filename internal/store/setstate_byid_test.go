package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openForByID(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "agents.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	return st
}

// TestSetStateByIDTargetsExactRow is the reconciler's safety net: a write keyed
// by id must land on the row that was observed, never on a different row that
// happens to share its name. A soft-deleted row and a live row can share a name
// (the name was reused), so keying by name would hit the wrong one.
func TestSetStateByIDTargetsExactRow(t *testing.T) {
	ctx := context.Background()
	st := openForByID(t)

	// Original "dup", then deleted; a fresh live "dup" reuses the name.
	require.NoError(t, st.Upsert(ctx, Agent{Name: "dup", State: StateThinking}))
	deleted, err := st.Get(ctx, "dup")
	require.NoError(t, err)
	require.NoError(t, st.SoftDelete(ctx, "dup"))
	require.NoError(t, st.Upsert(ctx, Agent{Name: "dup", State: StateNew}))
	live, err := st.Get(ctx, "dup")
	require.NoError(t, err)

	// Write by the deleted row's id: only that row changes.
	require.NoError(t, st.SetStateByID(ctx, deleted.ID, StateDone))

	all, err := st.ListAll(ctx)
	require.NoError(t, err)
	for _, a := range all {
		switch a.ID {
		case deleted.ID:
			assert.Equal(t, StateDone, a.State, "the id'd row is updated")
		case live.ID:
			assert.Equal(t, StateNew, a.State, "the live namesake is untouched")
		}
	}
}
