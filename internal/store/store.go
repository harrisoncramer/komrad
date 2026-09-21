// Package store owns the connection to Postgres and the transaction the domain stores run in,
// with the queries themselves in a package beneath it, one per domain.
//
// A write that touches more than one domain is handed a transaction through InTx and the domain
// stores take it as their DBTX, so archiving an agent and settling the mail addressed to it
// either both happen or neither does. Opening a pool also migrates the schema, so a checkout
// carrying a migration of its own must not be pointed at the panel's database.
package store

import (
	"context"
	"fmt"
	"strings"

	"weave/db"
	"weave/db/models"
	"weave/internal/fail"
	"weave/lib/xslice"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNameTaken = fail.AlreadyExists("name already taken")

// ErrNotFound is what a single-row lookup returns when the row is absent.
var ErrNotFound = fail.NotFound("not found")

// ErrNoTransaction says the connection a store was built over cannot open a transaction.
var ErrNoTransaction = fail.Internal("this connection cannot open a transaction")

var ErrNoReadOnlyURL = fail.Internal(
	"no read-only database url: set WEAVE_READONLY_DATABASE_URL, or a DATABASE_URL one can be derived from")

// Publisher is told which rows a statement changed once the change can be read.
type Publisher interface {
	TouchAgents(ids ...string)
	TouchWorkflows(ids ...string)
	TouchSchedules(ids ...string)
}

// DBTX is what a domain store runs its statements on and tells which rows they changed.
type DBTX interface {
	models.DBTX
	Publisher
}

// Discard is the publisher that tells nobody.
var Discard Publisher = discard{}

type discard struct{}

func (discard) TouchAgents(...string) {}

func (discard) TouchWorkflows(...string) {}

func (discard) TouchSchedules(...string) {}

// OpenParams is what a pool is opened with.
type OpenParams struct {
	URL string
	// Publisher is told which rows every store over the pool changes, nobody when nil.
	Publisher Publisher
}

type Pool struct {
	pool      *pgxpool.Pool
	publisher Publisher
}

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

// Open connects to the Postgres at the url and brings its schema up to date.
func Open(ctx context.Context, params OpenParams) (*Pool, error) {
	cfg, err := parseConfig(params.URL)
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

	publisher := params.Publisher
	if publisher == nil {
		publisher = Discard
	}

	return &Pool{pool: pool, publisher: publisher}, nil
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
			"the database host; run 'mise run db:readonly-role' and point " +
			"WEAVE_READONLY_DATABASE_URL at the role it creates")
}

// GetDB returns the handle to build a domain store over for ordinary use, outside any
// transaction, which tells the publisher about a change as soon as the statement returns.
func (p *Pool) GetDB() DBTX {
	return conn{DBTX: p.pool, Publisher: p.publisher}
}

// Unwatched is a handle over a connection whose changes are told to nobody.
func Unwatched(db models.DBTX) DBTX {
	return conn{DBTX: db, Publisher: Discard}
}

// conn runs statements straight on a connection and passes every touch on at once.
type conn struct {
	models.DBTX
	Publisher
}

// Begin opens a transaction whose touches wait for its commit.
func (c conn) Begin(ctx context.Context) (*Tx, error) {
	beginner, ok := c.DBTX.(txBeginner)
	if !ok {
		return nil, ErrNoTransaction
	}

	return begin(ctx, beginner, c.Publisher)
}

// GetRawPool returns the underlying pgx pool, which only River's driver should reach for.
func (p *Pool) GetRawPool() *pgxpool.Pool {
	return p.pool
}

// Close releases the connection pool.
func (p *Pool) Close() error {
	p.pool.Close()
	return nil
}

// Begin opens a transaction whose touches wait for its commit.
func (p *Pool) Begin(ctx context.Context) (*Tx, error) {
	return begin(ctx, p.pool, p.publisher)
}

// InTx runs fn inside a transaction, handing it the handle to build the domain stores over.
func (p *Pool) InTx(ctx context.Context, fn func(DBTX) error) error {
	tx, err := p.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(tx); err != nil {
		return err
	}

	return tx.Commit(ctx)
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
