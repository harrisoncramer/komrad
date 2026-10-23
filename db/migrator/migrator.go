package migrator

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"komrad/db"
	"komrad/db/isolation"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// errSchemaBehind reports a database that lacks migrations or triggers this build carries.
var errSchemaBehind = errors.New("the database is missing migrations this build carries; run komrad migrate as the database owner")

// Migrator brings a database's schema up to date with the migrations and trigger units it reads.
type Migrator struct {
	pool       *pgxpool.Pool
	migrations fs.FS
	triggers   fs.FS
	isolator   isolator
}

// isolator settles engine isolation inside each migration's transaction.
type isolator interface {
	Apply(ctx context.Context, tx pgx.Tx) error
}

// NewMigrator builds a Migrator over the embedded migrations and trigger units, each rooted at
// the directory holding them so a version is the bare file name, applying them through pool.
func NewMigrator(pool *pgxpool.Pool) (*Migrator, error) {
	migrations, err := fs.Sub(db.Migrations, "migrations")
	if err != nil {
		return nil, fmt.Errorf("failed to open the embedded migrations: %w", err)
	}

	triggers, err := fs.Sub(db.Triggers, "triggers")
	if err != nil {
		return nil, fmt.Errorf("failed to open the embedded triggers: %w", err)
	}

	return &Migrator{
		pool:       pool,
		migrations: migrations,
		triggers:   triggers,
		isolator:   isolation.NewIsolator(),
	}, nil
}

// Migrate applies any migrations not yet recorded, in filename order, each in its own
// transaction alongside the row recording it, and then any trigger unit the database is behind on.
func (m *Migrator) Migrate(ctx context.Context) error {
	all, err := m.list()
	if err != nil {
		return err
	}

	pending, err := m.isPending(ctx, all)
	if err != nil {
		return err
	}
	if !pending {
		return nil
	}

	return m.apply(ctx, all)
}

// Pending lists the migrations and trigger files that have not run, in the order they
// would run in, and writes nothing.
func (m *Migrator) Pending(ctx context.Context) ([]string, error) {
	all, err := m.list()
	if err != nil {
		return nil, err
	}

	plan, err := getPlan(ctx, m.pool, all)
	if err != nil {
		return nil, err
	}

	return plan.names(), nil
}

// CheckSchema refuses a database missing any migration or trigger version this build carries, and accepts one holding newer ones.
func (m *Migrator) CheckSchema(ctx context.Context) error {
	all, err := m.list()
	if err != nil {
		return err
	}

	plan, err := getPlan(ctx, m.pool, all)
	if err != nil {
		return err
	}
	if !plan.empty() {
		return fmt.Errorf("%w: %s", errSchemaBehind, strings.Join(plan.names(), ", "))
	}

	return nil
}

// list reads the migrations and trigger units in the order they run.
func (m *Migrator) list() (*contents, error) {
	migrations, err := listMigrationVersions(m.migrations)
	if err != nil {
		return nil, err
	}

	units, err := listUnits(m.triggers)
	if err != nil {
		return nil, err
	}

	return &contents{migrations: migrations, units: units}, nil
}
