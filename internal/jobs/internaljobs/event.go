package internaljobs

import (
	"context"
	"fmt"

	"komrad/internal/events"
	"komrad/internal/store"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

type EventRouter interface {
	Route(ctx context.Context, tx store.DBTX, event events.Event) ([]river.JobArgs, error)
}

type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type EventWorker struct {
	river.WorkerDefaults[events.Event]
	db     TxBeginner
	router EventRouter
}

// NewEventWorker builds the worker that routes each event in the transaction that completes it.
func NewEventWorker(db TxBeginner, router EventRouter) *EventWorker {
	return &EventWorker{db: db, router: router}
}

// Work writes the mail an event calls for and marks the job done in one transaction.
func (w *EventWorker) Work(ctx context.Context, job *river.Job[events.Event]) error {
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	followUps, err := w.router.Route(ctx, tx, job.Args)
	if err != nil {
		return err
	}

	if len(followUps) > 0 {
		client, err := river.ClientFromContextSafely[pgx.Tx](ctx)
		if err != nil {
			return fmt.Errorf("failed to find the client to record follow-up jobs with: %w", err)
		}
		for _, args := range followUps {
			if _, err := client.InsertTx(ctx, tx, args, nil); err != nil {
				return fmt.Errorf("failed to record a %s follow-up job: %w", args.Kind(), err)
			}
		}
	}

	if _, err := river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, job); err != nil {
		return fmt.Errorf("failed to mark event job %d done: %w", job.ID, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
