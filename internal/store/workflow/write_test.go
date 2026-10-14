package workflow

import (
	"testing"

	"komrad/internal/dbtest"
	"komrad/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDelete pins that a hard delete keyed by id takes the config rows a workflow alone owns
// with it.
func TestDelete(t *testing.T) {
	tests := []struct {
		name   string
		create bool
	}{
		{
			name: "an id nobody holds is not an error",
		},
		{
			name:   "a workflow row and its config rows are all removed",
			create: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool, err := store.Open(t.Context(), dbtest.URL(t))
			require.NoError(t, err)
			t.Cleanup(func() { _ = pool.Close() })

			st := NewWorkflowStore(pool.GetDB())
			ctx := t.Context()

			id := missingID
			if tt.create {
				created, err := st.Create(ctx, Config{Name: "backend", Worktree: WorktreeConfig{BaseBranch: "main"}})
				require.NoError(t, err)
				id = created.ID
			}

			require.NoError(t, st.Delete(ctx, id))

			_, err = st.GetIncludingArchived(ctx, id)
			require.ErrorIs(t, err, store.ErrNotFound, "the row is gone")

			if !tt.create {
				return
			}

			var configs, provisionerConfigs int
			require.NoError(t, pool.GetRawPool().QueryRow(ctx, "SELECT count(*) FROM workflow_configs").Scan(&configs))
			require.NoError(t, pool.GetRawPool().QueryRow(ctx, "SELECT count(*) FROM workflow_provisioner_configs").Scan(&provisionerConfigs))
			assert.Zero(t, configs, "its config row goes with it")
			assert.Zero(t, provisionerConfigs, "and its provisioner config row too")
		})
	}
}
