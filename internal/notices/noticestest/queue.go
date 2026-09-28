// Package noticestest routes notice events as they are recorded.
package noticestest

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"komrad/config"
	"komrad/internal/mail"
	"komrad/internal/notices"
	"komrad/internal/storetest"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

// Queue commits what a producer writes and routes each notice event it records straight after.
type Queue struct {
	stores    *storetest.Stores
	router    *notices.Router
	mu        sync.Mutex
	events    []notices.Event
	followUps []river.JobArgs
	errs      []error
}

// NewQueue builds a queue that routes through the handlers and a real mail service over the test's stores.
func NewQueue(t *testing.T, stores *storetest.Stores, handlers ...notices.Handler) *Queue {
	q := &Queue{
		stores: stores,
		router: notices.NewRouter(notices.NewRouterParams{
			Mailer: mail.NewMailService(mail.NewMailServiceParams{
				MailReadWriter: stores.Mail,
				Agents:         stores.Agents,
				Workflows:      stores.Workflows,
				Config:         config.Mail{},
			}),
			Handlers: handlers,
		}),
	}
	t.Cleanup(func() {
		for _, err := range q.TakeErrors() {
			t.Errorf("routing failed: %v", err)
		}
	})

	return q
}

// InsertTx records one job alongside what before writes.
func (q *Queue) InsertTx(ctx context.Context, args river.JobArgs, before func(tx pgx.Tx) error) error {
	return q.InsertManyTx(ctx, func(tx pgx.Tx) ([]river.JobArgs, error) {
		if before != nil {
			if err := before(tx); err != nil {
				return nil, err
			}
		}

		return []river.JobArgs{args}, nil
	})
}

// InsertManyTx commits what build writes and then routes every notice event it returned.
func (q *Queue) InsertManyTx(ctx context.Context, build func(tx pgx.Tx) ([]river.JobArgs, error)) error {
	tx, err := q.stores.Pool.GetRawPool().Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	jobs, err := build(tx)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	for _, job := range jobs {
		event, ok := job.(notices.Event)
		if !ok {
			continue
		}
		followUps, err := q.route(ctx, event)
		q.mu.Lock()
		q.events = append(q.events, event)
		q.followUps = append(q.followUps, followUps...)
		if err != nil {
			q.errs = append(q.errs, err)
		}
		q.mu.Unlock()
	}

	return nil
}

// ListEvents returns every notice event recorded so far.
func (q *Queue) ListEvents() []notices.Event {
	q.mu.Lock()
	defer q.mu.Unlock()

	return append([]notices.Event(nil), q.events...)
}

// ListFollowUps returns every job the handlers asked to record so far.
func (q *Queue) ListFollowUps() []river.JobArgs {
	q.mu.Lock()
	defer q.mu.Unlock()

	return append([]river.JobArgs(nil), q.followUps...)
}

// TakeErrors returns the routing errors so far and forgets them.
func (q *Queue) TakeErrors() []error {
	q.mu.Lock()
	defer q.mu.Unlock()

	errs := q.errs
	q.errs = nil

	return errs
}

func (q *Queue) route(ctx context.Context, event notices.Event) ([]river.JobArgs, error) {
	tx, err := q.stores.Pool.GetRawPool().Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	followUps, err := q.router.Route(ctx, tx, event)
	if err != nil {
		return nil, fmt.Errorf("failed to route a %s event: %w", event.Topic, err)
	}

	return followUps, tx.Commit(ctx)
}
