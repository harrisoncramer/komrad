package store

import (
	"context"
	"fmt"

	"weave/db/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type txBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Tx is one transaction, which holds every touch made through it until it has committed and
// drops them all if it rolls back.
type Tx struct {
	tx        pgx.Tx
	publisher Publisher
	agents    []string
	workflows []string
	schedules []string
}

func begin(ctx context.Context, db txBeginner, publisher Publisher) (*Tx, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	return &Tx{tx: tx, publisher: publisher}, nil
}

// Exec runs a statement in the transaction.
func (t *Tx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return t.tx.Exec(ctx, sql, args...)
}

// Query runs a query in the transaction.
func (t *Tx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return t.tx.Query(ctx, sql, args...)
}

// QueryRow runs a query for one row in the transaction.
func (t *Tx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return t.tx.QueryRow(ctx, sql, args...)
}

// CopyFrom bulk-loads rows in the transaction.
func (t *Tx) CopyFrom(ctx context.Context, table pgx.Identifier, columns []string, rows pgx.CopyFromSource) (int64, error) {
	return t.tx.CopyFrom(ctx, table, columns, rows)
}

// TouchAgents holds the agent ids for the commit.
func (t *Tx) TouchAgents(ids ...string) {
	t.agents = append(t.agents, ids...)
}

// TouchWorkflows holds the workflow ids for the commit.
func (t *Tx) TouchWorkflows(ids ...string) {
	t.workflows = append(t.workflows, ids...)
}

// TouchSchedules holds the schedule ids for the commit.
func (t *Tx) TouchSchedules(ids ...string) {
	t.schedules = append(t.schedules, ids...)
}

// Begin opens a nested transaction whose touches are handed to this one when it commits.
func (t *Tx) Begin(ctx context.Context) (*Tx, error) {
	return begin(ctx, t.tx, t)
}

// Unwrap is the transaction as pgx holds it, for a queue that writes its own rows into it.
func (t *Tx) Unwrap() pgx.Tx {
	return t.tx
}

// Commit commits the transaction and then tells the publisher every row touched through it.
func (t *Tx) Commit(ctx context.Context) error {
	if err := t.tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	if len(t.agents) > 0 {
		t.publisher.TouchAgents(t.agents...)
	}
	if len(t.workflows) > 0 {
		t.publisher.TouchWorkflows(t.workflows...)
	}
	if len(t.schedules) > 0 {
		t.publisher.TouchSchedules(t.schedules...)
	}

	return nil
}

// Rollback abandons the transaction and everything touched through it.
func (t *Tx) Rollback(ctx context.Context) error {
	return t.tx.Rollback(ctx)
}

var _ DBTX = (*Tx)(nil)

var _ models.DBTX = conn{}
