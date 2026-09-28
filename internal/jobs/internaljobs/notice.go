package internaljobs

import (
	"context"
	"fmt"

	"komrad/internal/notices"
	"komrad/internal/store"
	"komrad/lib/xslice"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

type NoticeRouter interface {
	Route(ctx context.Context, tx store.DBTX, event notices.Event) ([]river.JobArgs, error)
}

type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type NoticeWorker struct {
	river.WorkerDefaults[notices.Event]
	db     TxBeginner
	router NoticeRouter
}

// NewNoticeWorker builds the worker that routes each event in the transaction that completes it.
func NewNoticeWorker(db TxBeginner, router NoticeRouter) *NoticeWorker {
	return &NoticeWorker{db: db, router: router}
}

// Work writes the notices an event calls for and marks the job done in one transaction.
func (w *NoticeWorker) Work(ctx context.Context, job *river.Job[notices.Event]) error {
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
		inserts := xslice.Map(followUps, func(args river.JobArgs) river.InsertManyParams { return river.InsertManyParams{Args: args} })
		client, err := river.ClientFromContextSafely[pgx.Tx](ctx)
		if err != nil {
			return fmt.Errorf("failed to find the client to record follow-up jobs with: %w", err)
		}
		if _, err := client.InsertManyTx(ctx, tx, inserts); err != nil {
			return fmt.Errorf("failed to record %d follow-up jobs: %w", len(inserts), err)
		}
	}

	if _, err := river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, job); err != nil {
		return fmt.Errorf("failed to mark notice job %d done: %w", job.ID, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
