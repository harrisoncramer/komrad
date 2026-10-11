package tasks

import (
	"testing"

	"komrad/internal/store"
	"komrad/internal/storetest"

	agentstore "komrad/internal/store/agent"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeMoves struct {
	moving string
}

func (f fakeMoves) IsMoving(dir string) bool {
	return dir == f.moving
}

// TestClaimWriter covers a launch joining a task's checkout as its writer, and each reason it is refused.
func TestClaimWriter(t *testing.T) {
	tests := []struct {
		name      string
		moving    bool
		hasWriter bool
		wantErr   error
	}{
		{
			name: "a checkout with no writer and nothing moving it",
		},
		{
			name:    "a checkout komrad is rebasing, pushing or aborting in",
			moving:  true,
			wantErr: errCheckoutMoving,
		},
		{
			name:      "a checkout whose writer is still live",
			hasWriter: true,
			wantErr:   ErrHasWriter,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stores := storetest.Open(t)
			flow := stores.SeedWorkflow(t, workflowstore.Config{Name: "flow", Worktree: workflowstore.WorktreeConfig{Enabled: true}})
			task := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: flow.ID, Slug: "work"})
			dir := t.TempDir()
			require.NoError(t, stores.Tasks.SetCheckoutDir(t.Context(), task.ID, dir))
			if tt.hasWriter {
				stores.SeedAgent(t, agentstore.Agent{Name: "writer", Cwd: dir, WorkflowID: flow.ID, TaskID: &task.ID})
			}
			moves := fakeMoves{}
			if tt.moving {
				moves.moving = dir
			}
			svc := NewTaskService(NewTaskServiceParams{Tasks: stores.Tasks, Moves: moves, Tx: stores.Pool})

			err := stores.Pool.InTx(t.Context(), func(db store.DBTX) error {
				_, _, err := svc.ClaimWriter(t.Context(), db, ClaimWriterParams{TaskID: task.ID, Cwd: dir})

				return err
			})

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			assert.NoError(t, err)
		})
	}
}
