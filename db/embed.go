// Package db embeds the Atlas migrations and applies them, so a deployment can
// bring its own schema up to date by running the binary rather than by having
// the atlas CLI on the box.
package db

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"time"

	"weave/internal/logging"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

//go:embed readonly_role.sql
var readOnlyRoleSQL string

// migrateLock is the advisory lock key migrations are applied under. Several
// processes share the database and more than one can start at once, so without
// it two of them can read the same empty schema_migrations and both try to
// create the same table. The number is arbitrary but must never change.
const migrateLock int64 = 7_240_115

// Migrate applies any migrations not yet recorded, in filename order. Each runs
// in its own transaction alongside the row recording it, so a migration either
// lands and is marked or does neither. The whole pass is serialized on an
// advisory lock, which makes a concurrent start wait rather than race.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	files, err := migrationFiles()
	if err != nil {
		return err
	}

	return migrate(ctx, pool, files)
}

// migrate applies the migrations in files, which is the embedded set on every
// real run and a written-out one under test, where how a pass behaves across
// several files is what is being checked.
func migrate(ctx context.Context, pool *pgxpool.Pool, files fs.FS) error {
	versions, err := migrationVersions(files)
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

	return apply(ctx, pool, migrationSet{files: files, versions: versions})
}

// migrationFiles opens the embedded migrations rooted at the directory holding
// them, so a version is the bare file name both here and under test.
func migrationFiles() (fs.FS, error) {
	files, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("failed to open the embedded migrations: %w", err)
	}

	return files, nil
}

// isPending reports whether any migration has yet to run. A missing
// schema_migrations table means none of them have.
func isPending(ctx context.Context, pool *pgxpool.Pool, versions []string) (bool, error) {
	var applied int
	err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version = ANY ($1)`, versions).Scan(&applied)

	if IsUndefinedTable(err) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to read applied migrations: %w", err)
	}

	return applied < len(versions), nil
}

// migrationSet is the migrations to apply and where to read them from.
type migrationSet struct {
	files    fs.FS
	versions []string
}

// apply runs the pending migrations under the advisory lock, re-reading the applied set
// inside it since another process may have finished the work while this one waited.
func apply(ctx context.Context, pool *pgxpool.Pool, set migrationSet) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire a connection to migrate: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrateLock); err != nil {
		return fmt.Errorf("failed to take the migration lock: %w", err)
	}
	defer releaseMigrateLock(ctx, conn)

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY)`); err != nil {
		return fmt.Errorf("failed to create schema_migrations: %w", err)
	}

	for _, version := range set.versions {
		var applied bool
		if err := conn.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&applied); err != nil {
			return fmt.Errorf("failed to check migration %q: %w", version, err)
		}
		if applied {
			continue
		}

		sqlText, err := fs.ReadFile(set.files, version)
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

// unlockTimeout bounds the unlock so it cannot hang on a connection whose peer is gone.
const unlockTimeout = 5 * time.Second

// releaseMigrateLock unlocks on a context of its own, since a cancelled caller context
// fails before reaching the server and pgxpool pools the connection with the lock held.
func releaseMigrateLock(ctx context.Context, conn *pgxpool.Conn) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), unlockTimeout)
	defer cancel()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", migrateLock); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to release the migration lock", logging.Err(err)...)
	}
}

// migrationVersions lists the embedded migration file names in the order they
// must run, which is filename order because Atlas prefixes each with the
// timestamp it was authored at.
func migrationVersions(files fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(files, ".")
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
