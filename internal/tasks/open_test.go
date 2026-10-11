package tasks

import (
	"testing"

	"komrad/internal/store"
	"komrad/internal/storetest"

	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"

	"github.com/stretchr/testify/require"
)

type fakeMoves struct {
	moving string
}

func (f fakeMoves) IsMoving(dir string) bool {
	return dir == f.moving
}

// TestClaimWriterRefusesACheckoutKomradIsMoving keeps a launch out of a tree komrad is rebasing, pushing or aborting in.
func TestClaimWriterRefusesACheckoutKomradIsMoving(t *testing.T) {
	stores := storetest.Open(t)
	flow := stores.SeedWorkflow(t, workflowstore.Config{Name: "flow", Worktree: workflowstore.WorktreeConfig{Enabled: true}})
	task := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: flow.ID, Slug: "work"})
	dir := t.TempDir()
	require.NoError(t, stores.Tasks.SetCheckoutDir(t.Context(), task.ID, dir))
	svc := NewTaskService(NewTaskServiceParams{
		Tasks: stores.Tasks,
		Moves: fakeMoves{moving: dir},
		Tx:    stores.Pool,
	})

	err := stores.Pool.InTx(t.Context(), func(db store.DBTX) error {
		_, _, err := svc.ClaimWriter(t.Context(), db, ClaimWriterParams{TaskID: task.ID, Cwd: dir})

		return err
	})

	require.ErrorIs(t, err, errCheckoutMoving)
}
