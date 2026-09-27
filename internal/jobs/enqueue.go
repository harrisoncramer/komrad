package jobs

import (
	"context"
	"fmt"

	"komrad/lib/xslice"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// maxListedJobs is how many unfinished jobs of one kind a listing reads.
const maxListedJobs = 1000

// unfinishedStates are the states a job is in while its work is still owed.
var unfinishedStates = []rivertype.JobState{
	rivertype.JobStateAvailable,
	rivertype.JobStatePending,
	rivertype.JobStateRetryable,
	rivertype.JobStateRunning,
	rivertype.JobStateScheduled,
}

// waitingStates are the unfinished states short of running.
var waitingStates = []rivertype.JobState{
	rivertype.JobStateAvailable,
	rivertype.JobStatePending,
	rivertype.JobStateRetryable,
	rivertype.JobStateScheduled,
}

type txBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type Enqueuer struct {
	client *river.Client[pgx.Tx]
	db     txBeginner
}

// NewEnqueuer wraps the River client for the callers that only queue work.
func NewEnqueuer(client *river.Client[pgx.Tx], db txBeginner) *Enqueuer {
	return &Enqueuer{client: client, db: db}
}

// InsertTx records a job in one transaction with whatever before writes, so the two land or
// fail together, and returns once the job is recorded.
func (e *Enqueuer) InsertTx(ctx context.Context, args river.JobArgs, before func(tx pgx.Tx) error) error {
	return e.InsertManyTx(ctx, func(tx pgx.Tx) ([]river.JobArgs, error) {
		if before != nil {
			if err := before(tx); err != nil {
				return nil, err
			}
		}

		return []river.JobArgs{args}, nil
	})
}

// InsertManyTx records the jobs build asks for in the transaction build writes in, so the
// writes and the jobs land or fail together.
func (e *Enqueuer) InsertManyTx(ctx context.Context, build func(tx pgx.Tx) ([]river.JobArgs, error)) error {
	tx, err := e.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	jobs, err := build(tx)
	if err != nil {
		return err
	}

	if len(jobs) > 0 {
		params := xslice.Map(jobs, func(args river.JobArgs) river.InsertManyParams { return river.InsertManyParams{Args: args} })
		if _, err := e.client.InsertManyTx(ctx, tx, params); err != nil {
			return fmt.Errorf("failed to queue %d jobs: %w", len(jobs), err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// ListUnfinished is every job of one kind whose work is still owed.
func (e *Enqueuer) ListUnfinished(ctx context.Context, kind string) ([]*rivertype.JobRow, error) {
	return e.listInStates(ctx, kind, unfinishedStates)
}

// ListWaiting is every job of one kind whose work is owed and not yet taken by a worker.
func (e *Enqueuer) ListWaiting(ctx context.Context, kind string) ([]*rivertype.JobRow, error) {
	return e.listInStates(ctx, kind, waitingStates)
}

func (e *Enqueuer) listInStates(ctx context.Context, kind string, states []rivertype.JobState) ([]*rivertype.JobRow, error) {
	listed, err := e.client.JobList(ctx, river.NewJobListParams().
		Kinds(kind).
		States(states...).
		First(maxListedJobs))
	if err != nil {
		return nil, fmt.Errorf("failed to list the %s jobs in flight: %w", kind, err)
	}

	return listed.Jobs, nil
}
