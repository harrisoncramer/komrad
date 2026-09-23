// Package store owns the connection to Postgres and the transaction the domain stores run in,
// with the queries themselves in a package beneath it, one per domain.
//
// A write that touches more than one domain is handed a transaction through InTx and the domain
// stores take it as their DBTX, so archiving an agent and settling the mail addressed to it
// either both happen or neither does. A pool connects as one engine's login, which is what
// scopes every row it reads or writes to that engine, and it never migrates: the database
// owner does that through Migrate.
package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"weave/db"
	"weave/db/models"
	"weave/internal/fail"
	"weave/lib/xslice"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNameTaken = fail.AlreadyExists("name already taken")

// ErrNotFound is what a single-row lookup returns when the row is absent.
var ErrNotFound = fail.NotFound("not found")

var ErrNoReadOnlyURL = fail.Internal(
	"no read-only database url: set WEAVE_READONLY_DATABASE_URL, or a DATABASE_URL one can be derived from")

type DBTX = models.DBTX

type Pool struct {
	pool   *pgxpool.Pool
	engine Engine
}

// Engine is the engine a pool's login belongs to.
type Engine struct {
	ID   string
	Name string
}

// ErrNotAnEngine is what opening a pool returns for a login that belongs to no engine.
var ErrNotAnEngine = fail.Precondition(
	"the database login belongs to no engine; run weave engine ensure as the database owner and connect with the login it prints")

// IDCursor pages a listing that resumes from one row's id.
type IDCursor struct {
	ID string `json:"i"`
}

// Validate refuses a cursor holding anything but an id.
func (c IDCursor) Validate() error {
	if !IsID(c.ID) {
		return fmt.Errorf("cursor id %q is not an id", c.ID)
	}

	return nil
}

// Open connects to the Postgres at url as an engine's login, refusing a schema this build does not match.
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

	if err := db.CheckSchema(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}

	engine, err := models.New(pool).GetCurrentEngine(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		pool.Close()
		return nil, ErrNotAnEngine
	}
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to read which engine the login belongs to: %w", err)
	}

	return &Pool{pool: pool, engine: Engine{ID: engine.ID, Name: engine.Name}}, nil
}

// Migrate brings the schema at url up to date, connecting as the database's owner.
func Migrate(ctx context.Context, url string) error {
	pool, err := openOwner(ctx, url)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		return fmt.Errorf("failed to migrate db: %w", err)
	}

	return nil
}

// EnsureEngine creates an engine or brings its logins up to date, connecting as the database's owner.
func EnsureEngine(ctx context.Context, url string, params db.EnsureEngineParams) error {
	pool, err := openOwner(ctx, url)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := db.CheckSchema(ctx, pool); err != nil {
		return err
	}

	return db.EnsureEngine(ctx, pool, params)
}

func openOwner(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := parseConfig(url)
	if err != nil {
		return nil, err
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to open db as its owner: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to reach db as its owner: %w", err)
	}

	return pool, nil
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

// parseConfig reads url and names this process on every connection made from it.
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
			"the database host; run 'weave engine ensure NAME' and point " +
			"WEAVE_READONLY_DATABASE_URL at the role it creates")
}

// GetDB returns the handle to build a domain store over for ordinary use, outside
// any transaction.
func (p *Pool) GetDB() DBTX {
	return p.pool
}

// GetEngine returns the engine the pool's login belongs to.
func (p *Pool) GetEngine() Engine {
	return p.engine
}

// GetRiverSchema returns the schema the engine keeps its job queue in.
func (p *Pool) GetRiverSchema() string {
	return db.EngineRiverSchema(p.engine.Name)
}

// GetRawPool returns the underlying pgx pool, which only River's driver and the watch's
// listener reach for.
func (p *Pool) GetRawPool() *pgxpool.Pool {
	return p.pool
}

// Close releases the connection pool.
func (p *Pool) Close() error {
	p.pool.Close()
	return nil
}

// InTx runs fn inside a transaction, handing it the handle to build the domain stores over.
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
func IsID(id string) bool {
	_, err := uuid.Parse(id)
	return err == nil
}

// KeepIDs drops every string that could not be one of the ids these stores hand out.
func KeepIDs(ids []string) []string {
	return xslice.Filter(ids, IsID)
}

// NewID mints a row id the same way the columns' own uuidv7() default does.
func NewID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("failed to mint an id: %w", err)
	}

	return id.String(), nil
}

const maxFailureReasonLen = 480

// FailureReason is a cause as a text column can hold it, cut and scrubbed of what Postgres
// refuses.
func FailureReason(cause error) string {
	if cause == nil {
		return ""
	}

	text := strings.ToValidUTF8(strings.ReplaceAll(cause.Error(), "\x00", ""), "")
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > maxFailureReasonLen {
		text = strings.ToValidUTF8(text[:maxFailureReasonLen], "")
	}

	return text
}
