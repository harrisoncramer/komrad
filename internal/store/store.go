// Package store owns the database connection and the transaction boundary the
// domain stores run inside.
//
// It holds no tables of its own. Each domain owns its own package beneath this
// one, for agents, workflows, and transcripts.
//
// A write that spans domains runs through InTx, which hands the same handle to
// each store so they commit together. That is why the stores are built over a
// DBTX rather than a pool. The same code path serves a plain query and a
// transaction, and no store has to reach into another's tables to stay atomic.
package store

import (
	"context"
	"errors"
	"fmt"

	"claude-agents/db"
	"claude-agents/db/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DBTX is what a domain store runs its queries against, either the pool or a
// transaction, which is what lets one be rebound onto the other.
type DBTX = models.DBTX

// Pool is the database handle the domain stores are built over.
type Pool struct {
	pool *pgxpool.Pool
}

// Open connects to the Postgres at url and brings its schema up to date. The
// ping is what turns an unreachable database into an error at startup rather
// than on the first query, which for a tool launched from a tmux keybinding is
// the difference between a message and a panel that renders empty.
func Open(ctx context.Context, url string) (*Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to reach db: %w", err)
	}

	if err := db.Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to migrate db: %w", err)
	}

	return &Pool{pool: pool}, nil
}

// OpenReadOnly opens a pool for reading only, for surfaces that hand the database
// to something that must never write, such as the MCP endpoint. The guard rides on
// the connection rather than on each statement. Every session the pool hands out
// defaults to read-only transactions, so a write is refused by Postgres itself
// even if it slips past the statement checks above it.
func OpenReadOnly(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database url: %w", err)
	}
	cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to open db read-only: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to reach db read-only: %w", err)
	}

	return pool, nil
}

// DB returns the handle to build a domain store over for ordinary use, outside
// any transaction.
func (p *Pool) DB() DBTX {
	return p.pool
}

// Close releases the connection pool.
func (p *Pool) Close() error {
	p.pool.Close()
	return nil
}

// InTx runs fn inside a transaction, handing it the handle to build the domain
// stores over. Everything those stores do lands together or not at all, which is
// how an operation spanning two domains stays atomic without either of them
// knowing about the other's tables. Returning an error rolls the whole thing
// back.
func (p *Pool) InTx(ctx context.Context, fn func(DBTX) error) error {
	tx, err := p.pool.Begin(ctx)
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

// IsID reports whether a string could be one of the ids these stores hand out.
//
// Ids reach the stores from outside, as tool arguments or a name typed into the
// panel. The columns are uuid, so a value that is not one is refused by Postgres
// with a parse error rather than matching nothing, which turns a caller's typo
// into a failure instead of a miss. Checking first keeps the answer the caller
// expects, that an id which cannot exist is one that is not there.
func IsID(id string) bool {
	_, err := uuid.Parse(id)
	return err == nil
}

// NoRows reports whether an error is Postgres finding nothing, which every
// domain store turns into its own empty answer rather than an error.
func NoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
