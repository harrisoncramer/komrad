package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"komrad/internal/jobs/internaljobs"
	agentstore "komrad/internal/store/agent"
	worktreestore "komrad/internal/store/worktree"
	"komrad/lib/xslice"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

type WorktreeDelete struct {
	queue *Enqueuer
}

// NewWorktreeDelete builds the queuer of the job that deletes worktree directories.
func NewWorktreeDelete(queue *Enqueuer) *WorktreeDelete {
	return &WorktreeDelete{queue: queue}
}

// DeleteWorktrees claims the archived agents the directories were cut for, forgets the
// directories' rows, and queues a delete of each claimed agent and one delete of the
// directories, all in the same transaction.
func (d *WorktreeDelete) DeleteWorktrees(ctx context.Context, dirs []string) error {
	return d.queue.InsertManyTx(ctx, func(tx pgx.Tx) ([]river.JobArgs, error) {
		claims, err := agentstore.NewAgentStore(tx).MarkArchivedDeletingByID(ctx, xslice.Map(dirs, filepath.Base))
		if err != nil {
			return nil, err
		}

		if err := worktreestore.NewWorktreeStore(tx).Delete(ctx, dirs); err != nil {
			return nil, err
		}

		return append(deleteAgentJobs(agentstore.ClaimRoots(claims)), internaljobs.DeleteWorktreesArgs{Dirs: dirs}), nil
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
