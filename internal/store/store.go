package store

import (
	"context"
	"errors"
	"fmt"

	"weave/db"
	"weave/db/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNameTaken = errors.New("name already taken")

// ErrNotFound is what a single-row lookup returns instead of a nil row and a nil error, so a
// caller is told the row is absent rather than left to infer it.
var ErrNotFound = errors.New("not found")

var ErrNoReadOnlyURL = errors.New(
	"no read-only database url: set WEAVE_READONLY_DATABASE_URL, or a DATABASE_URL one can be derived from")

type DBTX = models.DBTX

type Pool struct {
	pool *pgxpool.Pool
}

// Open connects to the Postgres at url and brings its schema up to date.
func Open(ctx context.Context, url string) (*Pool, error) {
	cfg, err := parseConfig(url)
	if err != nil {
		return nil, err
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
	}

	// The ping turns an unreachable database into an error at startup rather than a panel
	// that renders empty on the first query.
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

// OpenReadOnly opens a pool for reading only, for surfaces such as the MCP endpoint that
// must never write.
func OpenReadOnly(ctx context.Context, url string) (*pgxpool.Pool, error) {
	// It takes a url of its own because a read-only transaction stops writes and nothing
	// else, so the role that url connects as decides everything a statement can read.
	if url == "" {
		return nil, ErrNoReadOnlyURL
	}

	cfg, err := parseConfig(url)
	if err != nil {
		return nil, err
	}
	// The guard rides on the connection rather than on each statement, so Postgres itself
	// refuses a write that slips past the checks above it.
	cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to open db read-only: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to reach db read-only: %w", err)
	}

	if err := refuseSuperuser(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}

	return pool, nil
}

// parseConfig reads url and names this process on every connection made from it, which is how
// a migration pass tells its own connections from another weave's on the same database.
func parseConfig(url string) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database url: %w", err)
	}
	cfg.ConnConfig.RuntimeParams["application_name"] = db.AppName()

	return cfg, nil
}

func refuseSuperuser(ctx context.Context, pool *pgxpool.Pool) error {
	var superuser bool
	if err := pool.QueryRow(ctx,
		`SELECT rolsuper FROM pg_roles WHERE rolname = current_user`).Scan(&superuser); err != nil {
		return fmt.Errorf("failed to check whether the read-only role is a superuser: %w", err)
	}
	if !superuser {
		return nil
	}

	return fmt.Errorf(
		"the read-only database role is a superuser, which would let a query tool read files on " +
			"the database host; run 'mise run db:readonly-role' and point " +
			"WEAVE_READONLY_DATABASE_URL at the role it creates")
}

// GetDB returns the handle to build a domain store over for ordinary use, outside
// any transaction.
func (p *Pool) GetDB() DBTX {
	return p.pool
}

// GetRawPool returns the underlying pgx pool for River, whose driver takes the concrete
// pool rather than an interface, and nothing else should reach for it.
func (p *Pool) GetRawPool() *pgxpool.Pool {
	return p.pool
}

// Close releases the connection pool.
func (p *Pool) Close() error {
	p.pool.Close()
	return nil
}

// InTx runs fn inside a transaction, handing it the handle to build the domain stores over
// so an operation spanning two domains stays atomic.
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

// IsID reports whether a string could be one of the ids these stores hand out, since the
// columns are uuid and Postgres fails a query on anything else rather than matching nothing.
func IsID(id string) bool {
	_, err := uuid.Parse(id)
	return err == nil
}

// NewID mints a row id the same way the columns' own uuidv7() default does, where v7
// matters because its leading bits carry the creation time that ordering by id relies on.
func NewID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("failed to mint an id: %w", err)
	}

	return id.String(), nil
}
