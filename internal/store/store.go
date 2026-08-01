// Package store owns the database connection and the transaction boundary the domain
// stores run inside. It holds no tables of its own; each domain owns a package beneath
// this one.
//
// A write that spans domains runs through InTx, which hands the same handle to each store
// so they commit together. That is why the stores are built over a DBTX rather than a
// pool: the same code path serves a plain query and a transaction, and no store has to
// reach into another's tables to stay atomic.
package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"weave/db"
	"weave/db/models"
	"weave/internal/logging"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DBTX is what a domain store runs its queries against, either the pool or a
// transaction, which is what lets one be rebound onto the other.
type DBTX = models.DBTX

// Pool is the database handle the domain stores are built over.
type Pool struct {
	pool *pgxpool.Pool
}

// Open connects to the Postgres at url and brings its schema up to date. The ping turns
// an unreachable database into an error at startup rather than on the first query, which
// for a tool launched from a tmux keybinding is the difference between a message and a
// panel that renders empty.
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

// OpenReadOnly opens a pool for reading only, for surfaces that hand the database to
// something that must never write, such as the MCP endpoint. The guard rides on the
// connection rather than on each statement, so a write is refused by Postgres itself even
// if it slips past the statement checks above it.
//
// What the url names matters as much as the flag on it. The read-only transaction stops
// writes and only writes, so the privileges of the role it connects as are what decide
// everything a statement can read. That is why this takes its own url rather than the
// writable one.
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

	warnIfSuperuser(ctx, pool)

	return pool, nil
}

// warnIfSuperuser writes a line when the read-only pool has landed on a superuser, which
// is the one case where nothing above the database holds. A superuser is exempt from the
// privilege checks on the catalogs and on the file-reading functions, and both are reached
// through a plain SELECT that writes nothing, so neither the read-only transaction nor any
// amount of reading the statement will refuse them.
//
// It warns rather than refuses because reaching here means someone pointed this at a
// superuser on purpose, and a panel that will not open says less about why than a line
// naming what it costs. A role that was never created fails earlier, on the ping.
func warnIfSuperuser(ctx context.Context, pool *pgxpool.Pool) {
	var superuser bool
	if err := pool.QueryRow(ctx,
		`SELECT rolsuper FROM pg_roles WHERE rolname = current_user`).Scan(&superuser); err != nil {
		slog.LogAttrs(ctx, slog.LevelWarn, "could not tell whether the read-only role is a superuser",
			logging.Err(err)...)

		return
	}
	if !superuser {
		return
	}

	slog.LogAttrs(ctx, slog.LevelWarn,
		"the read-only pool is connected as a superuser, so a query tool can read files on the database host; "+
			"run 'mise run db:readonly-role' and set WEAVE_READONLY_DATABASE_URL")
}

// DB returns the handle to build a domain store over for ordinary use, outside
// any transaction.
func (p *Pool) DB() DBTX {
	return p.pool
}

// Raw returns the underlying pgx pool. It exists for River, whose driver takes the
// concrete pool rather than an interface. Nothing else should reach for it, since a
// domain store takes DBTX so a transaction can be swapped in underneath it.
func (p *Pool) Raw() *pgxpool.Pool {
	return p.pool
}

// Close releases the connection pool.
func (p *Pool) Close() error {
	p.pool.Close()
	return nil
}

// InTx runs fn inside a transaction, handing it the handle to build the domain stores
// over, so an operation spanning two domains stays atomic without either of them knowing
// about the other's tables. Returning an error rolls the whole thing back.
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

// IsID reports whether a string could be one of the ids these stores hand out. The
// columns are uuid, so a value that is not one is refused by Postgres with a parse error
// rather than matching nothing, which would turn a caller's typo into a failure instead of
// a miss.
func IsID(id string) bool {
	_, err := uuid.Parse(id)
	return err == nil
}

// NewID mints a row id the same way the columns' own uuidv7() default does, for a caller
// that needs the id before the row exists. The version matters rather than just the
// uniqueness: a v7 carries its creation time in its leading bits, so the keyset pages and
// the search tie-breaks that order by id read it as the order rows were written.
func NewID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("failed to mint an id: %w", err)
	}

	return id.String(), nil
}

// NoRows reports whether an error is Postgres finding nothing, which every
// domain store turns into its own empty answer rather than an error.
func NoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

// ErrNameTaken reports that a write collided with a name already held. Every
// unique name in this schema is scoped to live rows, so this only ever means a
// record currently listed answers to that name, never one sitting in the archive.
var ErrNameTaken = errors.New("name already taken")

// uniqueViolation is the SQLSTATE Postgres raises for a broken unique constraint.
const uniqueViolation = "23505"

// NameTaken reports whether an error is a unique index refusing a write. It reads the
// driver's code rather than its message text, so the check does not depend on wording that
// can change under it.
func NameTaken(err error) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}
