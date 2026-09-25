// Package db embeds the Atlas migrations and applies them, so a deployment can
// bring its own schema up to date by running the binary rather than by having
// the atlas CLI on the box.
//
// It does this by embedding all of the migrations into the binary and then acquiring
// an advisory lock (so that two migrations cannot run migrations at the same time) and
// running them each, in sequential order, against the database.
package db

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"time"

	"komrad/internal/logging"
	"komrad/lib/xslice"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

//go:embed applied_migrations.sql
var appliedMigrationsSQL string

// migrateLock is the advisory lock key that stops two processes applying the same migration
// twice, and it must never change.
const migrateLock int64 = 7_240_115

// Migrate applies any migrations not yet recorded, in filename order, each in its own
// transaction alongside the row recording it.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	files, err := openMigrationFiles()
	if err != nil {
		return err
	}

	return migrate(ctx, pool, files)
}

// migrate applies the migrations in files.
func migrate(ctx context.Context, pool *pgxpool.Pool, files fs.FS) error {
	versions, err := listMigrationVersions(files)
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

// openMigrationFiles opens the embedded migrations rooted at the directory holding them, so a
// version is the bare file name.
func openMigrationFiles() (fs.FS, error) {
	files, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("failed to open the embedded migrations: %w", err)
	}

	return files, nil
}

// Pending lists the embedded migrations that have not run, in the order they would run in, and
// writes nothing.
func Pending(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	files, err := openMigrationFiles()
	if err != nil {
		return nil, err
	}

	versions, err := listMigrationVersions(files)
	if err != nil {
		return nil, err
	}

	return getMigrationsToRun(ctx, pool, versions)
}

// ErrSchemaBehind is returned when the database is missing migrations this build carries.
var ErrSchemaBehind = errors.New("the database is missing migrations this build carries; run komrad migrate as the database owner")

// ErrSchemaAhead is returned when the database holds migrations this build does not know.
var ErrSchemaAhead = errors.New("the database holds migrations this build does not know; run the build that applied them")

// CheckSchema refuses a database whose applied migrations are not exactly the ones this build carries.
func CheckSchema(ctx context.Context, pool *pgxpool.Pool) error {
	files, err := openMigrationFiles()
	if err != nil {
		return err
	}

	return checkSchema(ctx, pool, files)
}

func checkSchema(ctx context.Context, pool *pgxpool.Pool, files fs.FS) error {
	versions, err := listMigrationVersions(files)
	if err != nil {
		return err
	}

	applied, err := listAppliedMigrations(ctx, pool)
	if err != nil {
		return err
	}

	known := make(map[string]bool, len(versions))
	for _, version := range versions {
		known[version] = true
	}

	if pending := xslice.Filter(versions, func(version string) bool { return !applied[version] }); len(pending) > 0 {
		return fmt.Errorf("%w: %s", ErrSchemaBehind, strings.Join(pending, ", "))
	}

	var unknown []string
	for version := range applied {
		if !known[version] {
			unknown = append(unknown, version)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)

		return fmt.Errorf("%w: %s", ErrSchemaAhead, strings.Join(unknown, ", "))
	}

	return nil
}

// listAppliedMigrations reads the ledger, which a database that has never been migrated does not have yet.
func listAppliedMigrations(ctx context.Context, q queryer) (map[string]bool, error) {
	rows, err := q.Query(ctx, appliedMigrationsSQL)
	if isUndefinedTable(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read applied migrations: %w", err)
	}

	versions, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if isUndefinedTable(err) {
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

// isPending reports whether any migration has yet to run.
func isPending(ctx context.Context, pool *pgxpool.Pool, versions []string) (bool, error) {
	pending, err := getMigrationsToRun(ctx, pool, versions)
	if err != nil {
		return false, err
	}

	return len(pending) > 0, nil
}

// queryer is the connection a walk of the ledger reads through, the pool or the locked
// connection.
type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// getMigrationsToRun is which of versions the database has no record of yet.
func getMigrationsToRun(ctx context.Context, q queryer, versions []string) ([]string, error) {
	applied, err := listAppliedMigrations(ctx, q)
	if err != nil {
		return nil, err
	}

	return xslice.Filter(versions, func(version string) bool { return !applied[version] }), nil
}

// migrationSet is the migrations to apply and where to read them from.
type migrationSet struct {
	files    fs.FS
	versions []string
}

// apply runs the pending migrations under the advisory lock, re-reading the applied set inside
// it since another process may have finished the work while this one waited.
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

	pendingMigrations, err := getMigrationsToRun(ctx, conn, set.versions)
	if err != nil {
		return err
	}
	if len(pendingMigrations) == 0 {
		return nil
	}

	if err := refuseSharedDatabase(ctx, conn, pendingMigrations); err != nil {
		return err
	}

	for _, version := range pendingMigrations {
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

// ErrDatabaseInUse is returned rather than migrating a database another komrad is on.
var ErrDatabaseInUse = errors.New("another komrad is using this database")

// refuseSharedDatabase stops a pass that would change the schema under a komrad already
// running against the same database.
func refuseSharedDatabase(ctx context.Context, conn *pgxpool.Conn, pending []string) error {
	rows, err := conn.Query(ctx, `
		SELECT DISTINCT application_name
		FROM pg_stat_activity
		WHERE datname = current_database()
		  AND pid <> pg_backend_pid()
		  AND application_name LIKE $1
		  AND application_name <> $2
		ORDER BY application_name`, appNamePrefix+"/%", AppName())
	if err != nil {
		return fmt.Errorf("failed to read who else is using the database: %w", err)
	}

	others, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("failed to read who else is using the database: %w", err)
	}
	if len(others) == 0 {
		return nil
	}

	return fmt.Errorf("%w: %s is connected to it, and this build holds %d migration(s) it has not seen (%s). "+
		"Stop that komrad, or point KOMRAD_DATABASE_URL at a database of this checkout's own",
		ErrDatabaseInUse, strings.Join(others, ", "), len(pending), strings.Join(pending, ", "))
}

// unlockTimeout bounds the unlock so it cannot hang on a connection whose peer is gone.
const unlockTimeout = 5 * time.Second

// releaseMigrateLock unlocks on a context of its own.
func releaseMigrateLock(ctx context.Context, conn *pgxpool.Conn) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), unlockTimeout)
	defer cancel()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", migrateLock); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to release the migration lock", logging.Err(err)...)
	}
}

// versionLength is how much of the fingerprint is kept.
const versionLength = 12

// SchemaVersion fingerprints the embedded migrations, and changes whenever one is added or edited.
func SchemaVersion() (string, error) {
	files, err := openMigrationFiles()
	if err != nil {
		return "", err
	}

	return schemaVersion(files)
}

// schemaVersion hashes each migration's name alongside its contents.
func schemaVersion(files fs.FS) (string, error) {
	versions, err := listMigrationVersions(files)
	if err != nil {
		return "", err
	}

	var content []byte
	for _, version := range versions {
		sqlText, err := fs.ReadFile(files, version)
		if err != nil {
			return "", fmt.Errorf("failed to read migration %q: %w", version, err)
		}

		content = append(content, version...)
		content = append(content, 0)
		content = append(content, sqlText...)
		content = append(content, 0)
	}

	sum := sha256.Sum256(content)

	return hex.EncodeToString(sum[:])[:versionLength], nil
}

// listMigrationVersions lists the embedded migration file names in the order they must run,
// which is filename order because Atlas prefixes each with a timestamp.
func listMigrationVersions(files fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil, fmt.Errorf("failed to read migrations: %w", err)
	}

	sqlFiles := xslice.Filter(entries, func(entry fs.DirEntry) bool { return strings.HasSuffix(entry.Name(), ".sql") })
	versions := xslice.Map(sqlFiles, func(entry fs.DirEntry) string { return entry.Name() })
	sort.Strings(versions)

	return versions, nil
}
