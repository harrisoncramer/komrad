package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"weave/internal/jobs/internaljobs"
	agentstore "weave/internal/store/agent"
	worktreestore "weave/internal/store/worktree"
	"weave/lib/xslice"

	"github.com/jackc/pgx/v5"
)

type WorktreeDelete struct {
	queue *Enqueuer
}

// NewWorktreeDelete builds the queuer of the job that deletes worktree directories.
func NewWorktreeDelete(queue *Enqueuer) *WorktreeDelete {
	return &WorktreeDelete{queue: queue}
}

// DeleteWorktrees claims the archived agents the directories were cut for, forgets the
// directories' rows and queues one delete of the directories in the same transaction.
func (d *WorktreeDelete) DeleteWorktrees(ctx context.Context, dirs []string) error {
	return d.queue.InsertTx(ctx, internaljobs.DeleteWorktreesArgs{Dirs: dirs}, func(tx pgx.Tx) error {
		if err := agentstore.NewAgentStore(tx).MarkArchivedDeletingByID(ctx, xslice.Map(dirs, filepath.Base)); err != nil {
			return err
		}

		return worktreestore.NewWorktreeStore(tx).Delete(ctx, dirs)
	})
}

// ListClaimedDirs is every directory an unfinished worktree delete names.
func (d *WorktreeDelete) ListClaimedDirs(ctx context.Context) ([]string, error) {
	jobs, err := d.queue.ListUnfinished(ctx, internaljobs.DeleteWorktreesArgs{}.Kind())
	if err != nil {
		return nil, err
	}

	var dirs []string
	for _, job := range jobs {
		var args internaljobs.DeleteWorktreesArgs
		if err := json.Unmarshal(job.EncodedArgs, &args); err != nil {
			return nil, fmt.Errorf("failed to read the arguments of job %d: %w", job.ID, err)
		}
		dirs = append(dirs, args.Dirs...)
	}

	return dirs, nil
}
