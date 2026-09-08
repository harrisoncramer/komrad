package agents

import (
	"testing"

	"weave/internal/store"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeleteIfEmpty covers which workflows the check destroys: a workflow it never touched, or
// one still holding an agent, is left exactly where it was, and only an archived one with
// nothing left in it is destroyed.
func TestDeleteIfEmpty(t *testing.T) {
	tests := []struct {
		name     string
		create   bool
		archive  bool
		addAgent bool
		wantGone bool
	}{
		{
			name: "an id nobody holds",
		},
		{
			name:   "a live workflow is left alone even with nothing in it",
			create: true,
		},
		{
			name:     "an archived workflow still holding an agent is left alone",
			create:   true,
			addAgent: true,
			archive:  true,
		},
		{
			name:     "an archived workflow with nothing left is deleted",
			create:   true,
			archive:  true,
			wantGone: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := newFakeTmux()
			manager := newManager(tmux, st, "")
			workflows := workflowsFor(st, tmux)
			ctx := t.Context()

			id := "00000000-0000-0000-0000-000000000000"
			if tt.create {
				created, err := workflows.NewWorkflow(ctx, workflowstore.Config{Name: "backend"})
				require.NoError(t, err)
				id = created.ID
			}
			if tt.addAgent {
				_, err := manager.launch.NewAgent(ctx, NewAgentParams{
					Title:      "agent",
					Dir:        t.TempDir(),
					WorkflowID: id,
				})
				require.NoError(t, err)
			}
			if tt.archive {
				require.NoError(t, workflows.ArchiveWorkflow(ctx, id))
			}

			require.NoError(t, manager.emptied.DeleteIfEmpty(ctx, id))

			got, err := st.Workflows.GetIncludingArchived(ctx, id)

			if tt.wantGone || !tt.create {
				require.ErrorIs(t, err, store.ErrNotFound, "the workflow row is gone")

				return
			}

			require.NoError(t, err)
			assert.NotNil(t, got, "the workflow is left where it was")
		})
	}
}
