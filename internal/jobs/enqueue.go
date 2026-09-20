package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"weave/internal/jobs/internaljobs"
	agentstore "weave/internal/store/agent"
	"weave/lib/xslice"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// maxListedJobs is how many unfinished deletes one listing reads.
const maxListedJobs = 1000

// unfinishedStates are the states a job is in while its work is still owed.
var unfinishedStates = []rivertype.JobState{
	rivertype.JobStateAvailable,
	rivertype.JobStatePending,
	rivertype.JobStateRetryable,
	rivertype.JobStateRunning,
	rivertype.JobStateScheduled,
}

type txBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type Enqueuer struct {
	client *river.Client[pgx.Tx]
	db     txBeginner
}

// NewEnqueuer wraps the River client for the callers that only queue work, over the database
// the rows a delete claims are written in.
func NewEnqueuer(client *river.Client[pgx.Tx], db txBeginner) *Enqueuer {
	return &Enqueuer{client: client, db: db}
}

// EmptyArchive claims every archived agent and queues the sweep that deletes them, in one
// transaction, and returns once the job is recorded.
func (e *Enqueuer) EmptyArchive(ctx context.Context) error {
	return e.inTx(ctx, func(tx pgx.Tx) error {
		if err := agentstore.New(tx).MarkArchivedDeleting(ctx); err != nil {
			return err
		}

		if _, err := e.client.InsertTx(ctx, tx, internaljobs.EmptyArchiveArgs{}, nil); err != nil {
			return fmt.Errorf("failed to queue emptying the archive: %w", err)
		}

		return nil
	})
}

// DeleteWorktrees claims the archived agents the directories were cut for and queues one delete
// of the directories, in one transaction, and returns once the job is recorded.
func (e *Enqueuer) DeleteWorktrees(ctx context.Context, dirs []string) error {
	return e.inTx(ctx, func(tx pgx.Tx) error {
		if err := agentstore.New(tx).MarkArchivedDeletingByID(ctx, xslice.Map(dirs, filepath.Base)); err != nil {
			return err
		}

		if _, err := e.client.InsertTx(ctx, tx, internaljobs.DeleteWorktreesArgs{Dirs: dirs}, nil); err != nil {
			return fmt.Errorf("failed to queue deleting %d worktrees: %w", len(dirs), err)
		}

		return nil
	})
}

// ListClaimedDirs is every directory an unfinished worktree delete names.
func (e *Enqueuer) ListClaimedDirs(ctx context.Context) ([]string, error) {
	listed, err := e.client.JobList(ctx, river.NewJobListParams().
		Kinds(internaljobs.DeleteWorktreesArgs{}.Kind()).
		States(unfinishedStates...).
		First(maxListedJobs))
	if err != nil {
		return nil, fmt.Errorf("failed to list the worktree deletes in flight: %w", err)
	}

	var dirs []string
	for _, job := range listed.Jobs {
		var args internaljobs.DeleteWorktreesArgs
		if err := json.Unmarshal(job.EncodedArgs, &args); err != nil {
			return nil, fmt.Errorf("failed to read the arguments of job %d: %w", job.ID, err)
		}
		dirs = append(dirs, args.Dirs...)
	}

	return dirs, nil
}

func (e *Enqueuer) inTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := e.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
