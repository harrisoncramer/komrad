// Package db embeds the Atlas migrations and applies them, so a deployment can
// bring its own schema up to date by running the binary rather than by having
// the atlas CLI on the box.
package db

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrateLock is the advisory lock key migrations are applied under. Several
// processes share the database and more than one can start at once, so without
// it two of them can read the same empty schema_migrations and both try to
// create the same table. The number is arbitrary but must never change.
const migrateLock int64 = 7_240_115

// Migrate applies any migrations not yet recorded, in filename order. Each runs
// in its own transaction alongside the row recording it, so a migration either
// lands and is marked or does neither. The whole pass is serialized on an
// advisory lock, which makes a concurrent start wait rather than race.
//
// Every command opens the store, including the status line's, which tmux runs
// as a fresh process on each interval. So the common case, a database already up
// to date, is answered by a single read before any lock is taken.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	versions, err := migrationVersions()
	if err != nil {
		return err
	}

	pending, err := isPending(ctx, pool, versions)
	if err != nil {
		return err
	}
	if !pending {
		return nil
	}

	return apply(ctx, pool, versions)
}

// isPending reports whether any migration has yet to run. A missing
// schema_migrations table means none of them have.
func isPending(ctx context.Context, pool *pgxpool.Pool, versions []string) (bool, error) {
	var applied int
	err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version = ANY ($1)`, versions).Scan(&applied)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == undefinedTable {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to read applied migrations: %w", err)
	}

	return applied < len(versions), nil
}

// undefinedTable is the SQLSTATE Postgres raises for a query against a table
// that does not exist, which on the first run is schema_migrations itself.
const undefinedTable = "42P01"

// apply runs the pending migrations under the advisory lock. The applied set is
// re-read inside the lock, since another process may have finished the work
// between the check above and the lock being granted.
func apply(ctx context.Context, pool *pgxpool.Pool, versions []string) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire a connection to migrate: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrateLock); err != nil {
		return fmt.Errorf("failed to take the migration lock: %w", err)
	}
	defer func() { _, _ = conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", migrateLock) }()

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY)`); err != nil {
		return fmt.Errorf("failed to create schema_migrations: %w", err)
	}

	for _, version := range versions {
		var applied bool
		if err := conn.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&applied); err != nil {
			return fmt.Errorf("failed to check migration %q: %w", version, err)
		}
		if applied {
			continue
		}

		sqlText, err := migrationsFS.ReadFile("migrations/" + version)
		if err != nil {
			return fmt.Errorf("failed to read migration %q: %w", version, err)
		}

		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("failed to begin migration %q: %w", version, err)
		}

		if _, err := tx.Exec(ctx, string(sqlText)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("failed to apply migration %q: %w", version, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("failed to record migration %q: %w", version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("failed to commit migration %q: %w", version, err)
		}
	}

	return nil
}

// migrationVersions lists the embedded migration file names in the order they
// must run, which is filename order because Atlas prefixes each with the
// timestamp it was authored at.
func migrationVersions() ([]string, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("failed to read migrations: %w", err)
	}

	versions := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".sql") {
			versions = append(versions, entry.Name())
		}
	}
	sort.Strings(versions)

	return versions, nil
}
