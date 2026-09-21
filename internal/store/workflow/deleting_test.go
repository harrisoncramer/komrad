package workflow

import (
	"testing"

	"weave/internal/dbtest"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	"weave/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeletingClaimHidesTheWorkflow covers the archive claim taking only an archived workflow
// holding archived agents out of the page, and an agent's release giving its workflow back.
func TestDeletingClaimHidesTheWorkflow(t *testing.T) {
	tests := []struct {
		name         string
		archive      bool
		holdsAgent   bool
		releaseAgent bool
		releaseAll   bool
		wantHidden   bool
	}{
		{
			name:       "a live workflow is left alone",
			holdsAgent: true,
		},
		{
			name:    "an archived workflow holding nothing is left alone",
			archive: true,
		},
		{
			name:       "an archived workflow holding an archived agent is claimed",
			archive:    true,
			holdsAgent: true,
			wantHidden: true,
		},
		{
			name:         "the agent's release gives the workflow back",
			archive:      true,
			holdsAgent:   true,
			releaseAgent: true,
		},
		{
			name:       "a release of everything gives the workflow back",
			archive:    true,
			holdsAgent: true,
			releaseAll: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool, err := store.Open(t.Context(), dbtest.URL(t))
			require.NoError(t, err)
			t.Cleanup(func() { _ = pool.Close() })
			f, agents := New(pool.GetDB()), agentstore.New(pool.GetDB())
			ctx := t.Context()

			created, err := f.Create(ctx, Config{Name: "backend"})
			require.NoError(t, err)
			agentID := ""
			if tt.holdsAgent {
				require.NoError(t, agents.Create(ctx, agentstore.Agent{Name: "author", WorkflowID: created.ID, State: agentstore.StateIdle}))
				author, err := agents.GetInWorkflow(ctx, created.ID, "author")
				require.NoError(t, err)
				agentID = author.ID
				_, err = agents.Archive(ctx, []string{agentID})
				require.NoError(t, err)
			}
			if tt.archive {
				require.NoError(t, f.Archive(ctx, created.ID))
			}

			require.NoError(t, f.MarkArchivedDeleting(ctx))
			if tt.releaseAgent {
				require.NoError(t, agents.ClearDeleting(ctx, []string{agentID}))
			}
			if tt.releaseAll {
				require.NoError(t, f.ClearAllDeleting(ctx))
			}

			page, err := f.ListPage(ctx, 10, nil, tt.archive)
			require.NoError(t, err)
			listed := xslice.Map(page, func(w Workflow) string { return w.Name })
			if tt.wantHidden {
				assert.Empty(t, listed, "a claimed workflow is out of the page")
			} else {
				assert.Equal(t, []string{"backend"}, listed)
			}

			got, err := f.GetIncludingArchived(ctx, created.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantHidden, got.DeletingAt != nil)
		})
	}
}
