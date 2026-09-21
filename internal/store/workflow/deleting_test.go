package workflow

import (
	"context"
	"testing"

	"weave/internal/dbtest"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	"weave/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeletingClaimHidesTheWorkflow covers a claim on agents taking their archived workflow out
// of the page once nothing unclaimed is left inside it, and a release giving it back.
func TestDeletingClaimHidesTheWorkflow(t *testing.T) {
	tests := []struct {
		name       string
		archive    bool
		agents     int
		failed     bool
		claim      func(ctx context.Context, agents *agentstore.Store, ids []string) error
		release    func(ctx context.Context, f *Store, agents *agentstore.Store, ids []string) error
		wantHidden bool
	}{
		{
			name:    "a live workflow is left alone",
			archive: false,
			agents:  1,
			claim: func(ctx context.Context, agents *agentstore.Store, _ []string) error {
				return agents.MarkArchivedDeleting(ctx)
			},
		},
		{
			name:    "an archived workflow holding nothing is left alone",
			archive: true,
			agents:  0,
			claim: func(ctx context.Context, agents *agentstore.Store, _ []string) error {
				return agents.MarkArchivedDeleting(ctx)
			},
		},
		{
			name:    "an archived workflow holding a failed agent is left alone",
			archive: true,
			agents:  1,
			failed:  true,
			claim: func(ctx context.Context, agents *agentstore.Store, _ []string) error {
				return agents.MarkArchivedDeleting(ctx)
			},
		},
		{
			name:    "a claim on one of two agents leaves the workflow",
			archive: true,
			agents:  2,
			claim: func(ctx context.Context, agents *agentstore.Store, ids []string) error {
				return agents.MarkDeleting(ctx, ids[:1])
			},
		},
		{
			name:    "the archive claim takes the workflow with its agents",
			archive: true,
			agents:  2,
			claim: func(ctx context.Context, agents *agentstore.Store, _ []string) error {
				return agents.MarkArchivedDeleting(ctx)
			},
			wantHidden: true,
		},
		{
			name:    "a claim by id on the last agent takes the workflow",
			archive: true,
			agents:  2,
			claim: func(ctx context.Context, agents *agentstore.Store, ids []string) error {
				if err := agents.MarkDeleting(ctx, ids[:1]); err != nil {
					return err
				}

				return agents.MarkDeleting(ctx, ids[1:])
			},
			wantHidden: true,
		},
		{
			name:    "an archived claim by id on the last agent takes the workflow",
			archive: true,
			agents:  1,
			claim: func(ctx context.Context, agents *agentstore.Store, ids []string) error {
				return agents.MarkArchivedDeletingByID(ctx, ids)
			},
			wantHidden: true,
		},
		{
			name:    "an agent's release gives the workflow back",
			archive: true,
			agents:  2,
			claim: func(ctx context.Context, agents *agentstore.Store, _ []string) error {
				return agents.MarkArchivedDeleting(ctx)
			},
			release: func(ctx context.Context, _ *Store, agents *agentstore.Store, ids []string) error {
				return agents.ClearDeleting(ctx, ids[:1])
			},
		},
		{
			name:    "a release of everything gives the workflow back",
			archive: true,
			agents:  1,
			claim: func(ctx context.Context, agents *agentstore.Store, _ []string) error {
				return agents.MarkArchivedDeleting(ctx)
			},
			release: func(ctx context.Context, f *Store, _ *agentstore.Store, _ []string) error {
				return f.ClearAllDeleting(ctx)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool, err := store.Open(t.Context(), dbtest.URL(t))
			require.NoError(t, err)
			t.Cleanup(func() { _ = pool.Close() })
			f, agents := New(pool.GetDB()), agentstore.New(pool.GetDB())
			ctx := t.Context()

			created, err := f.Create(ctx, Config{Name: "backend", Worktree: WorktreeConfig{BaseBranch: "main"}})
			require.NoError(t, err)
			var ids []string
			for i := range tt.agents {
				name := "author-" + string(rune('a'+i))
				require.NoError(t, agents.Create(ctx, agentstore.Agent{Name: name, WorkflowID: created.ID, State: agentstore.StateIdle}))
				author, err := agents.GetInWorkflow(ctx, created.ID, name)
				require.NoError(t, err)
				ids = append(ids, author.ID)
				_, err = agents.Archive(ctx, []string{author.ID})
				require.NoError(t, err)
				if tt.failed {
					author.State = agentstore.StateFailedToCleanup
					require.NoError(t, agents.Update(ctx, *author))
				}
			}
			if tt.archive {
				require.NoError(t, f.Archive(ctx, created.ID))
			}

			require.NoError(t, tt.claim(ctx, agents, ids))
			if tt.release != nil {
				require.NoError(t, tt.release(ctx, f, agents, ids))
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
