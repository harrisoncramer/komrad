package migrator

import (
	"context"
	_ "embed"
	"fmt"
	"slices"

	"komrad/db"
	"komrad/lib/xslice"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed applied_migrations.sql
var appliedMigrationsSQL string

//go:embed applied_triggers.sql
var appliedTriggersSQL string

// contents is a list of migrations and trigger units, in the order they run.
type contents struct {
	migrations []string
	units      []unit
}

type appliedTrigger struct {
	Unit    string
	Version int
}

// queryer is the connection a walk of the ledger reads through, the pool or the locked
// connection.
type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// listAppliedMigrations reads the ledger, which a database that has never been migrated does not have yet.
func listAppliedMigrations(ctx context.Context, q queryer) (map[string]bool, error) {
	rows, err := q.Query(ctx, appliedMigrationsSQL)
	if db.IsUndefinedTable(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read applied migrations: %w", err)
	}

	versions, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if db.IsUndefinedTable(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read applied migrations: %w", err)
	}

	applied := make(map[string]bool, len(versions))
	for _, version := range versions {
		applied[version] = true
	}

	return applied, nil
}

// listAppliedTriggers reads the version each trigger unit was last applied at, which a database
// that has never been migrated has no ledger for yet.
func listAppliedTriggers(ctx context.Context, q queryer) (map[string]int, error) {
	rows, err := q.Query(ctx, appliedTriggersSQL)
	if db.IsUndefinedTable(err) {
		return map[string]int{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read applied triggers: %w", err)
	}

	units, err := pgx.CollectRows(rows, pgx.RowToStructByName[appliedTrigger])
	if db.IsUndefinedTable(err) {
		return map[string]int{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read applied triggers: %w", err)
	}

	applied := make(map[string]int, len(units))
	for _, u := range units {
		applied[u.Unit] = u.Version
	}

	return applied, nil
}

// isPending reports whether any migration or trigger unit has yet to run.
func isPending(ctx context.Context, pool *pgxpool.Pool, all *contents) (bool, error) {
	plan, err := getPlan(ctx, pool, all)
	if err != nil {
		return false, err
	}

	return !plan.empty(), nil
}

func (c *contents) empty() bool {
	return len(c.migrations) == 0 && len(c.units) == 0
}

func (c *contents) names() []string {
	return append(slices.Clone(c.migrations), xslice.Map(c.units, func(u unit) string { return u.file })...)
}

// getPlan lists the migrations the database has not recorded and the trigger units it is behind on.
func getPlan(ctx context.Context, q queryer, all *contents) (*contents, error) {
	appliedMigrations, err := listAppliedMigrations(ctx, q)
	if err != nil {
		return nil, err
	}

	appliedTriggers, err := listAppliedTriggers(ctx, q)
	if err != nil {
		return nil, err
	}

	migrations := xslice.Filter(all.migrations, func(version string) bool { return !appliedMigrations[version] })
	units := xslice.Filter(all.units, func(u unit) bool {
		return len(migrations) > 0 || appliedTriggers[u.name] < u.version
	})

	return &contents{migrations: migrations, units: units}, nil
}
