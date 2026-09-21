package jobs

import (
	"context"
	"fmt"

	"weave/internal/store"

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

type txBeginner interface {
	Begin(ctx context.Context) (*store.Tx, error)
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
func (e *Enqueuer) InsertTx(ctx context.Context, args river.JobArgs, before func(tx store.DBTX) error) error {
	tx, err := e.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if before != nil {
		if err := before(tx); err != nil {
			return err
		}
	}

	if _, err := e.client.InsertTx(ctx, tx.Unwrap(), args, nil); err != nil {
		return fmt.Errorf("failed to queue %s: %w", args.Kind(), err)
	}

	return tx.Commit(ctx)
}

// ListUnfinished is every job of one kind whose work is still owed.
func (e *Enqueuer) ListUnfinished(ctx context.Context, kind string) ([]*rivertype.JobRow, error) {
	listed, err := e.client.JobList(ctx, river.NewJobListParams().
		Kinds(kind).
		States(unfinishedStates...).
		First(maxListedJobs))
	if err != nil {
		return nil, fmt.Errorf("failed to list the %s jobs in flight: %w", kind, err)
	}

	return listed.Jobs, nil
}
