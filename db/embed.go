// Package db embeds the Atlas migrations and applies them, so a deployment can
// bring its own schema up to date by running the binary rather than by having
// the atlas CLI on the box.
//
// It does this by embedding all of the migrations into the binary and then acquiring
// an advisory lock (so that two migrations cannot run migrations at the same time) and
// running them each, in sequential order, against the database.
//
// The triggers are embedded alongside them as one versioned file per table, which a pass
// reapplies whenever the file's version moves past the one the database recorded.
package db

import (
	"cmp"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"komrad/internal/logging"
	"komrad/lib/xslice"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

//go:embed triggers/*.sql
var triggersFS embed.FS

//go:embed applied_migrations.sql
var appliedMigrationsSQL string

//go:embed applied_triggers.sql
var appliedTriggersSQL string

// migrateLock is the advisory lock key that stops two processes applying the same migration
// twice, and it must never change.
const migrateLock int64 = 7_240_115

// functionsUnit is the trigger unit holding the functions every other unit's triggers run.
const functionsUnit = "_functions"

var unitFileName = regexp.MustCompile(`^(.+)\.v(\d+)\.sql$`)

// dropOrphanFunctionsSQL drops every trigger function no trigger runs any longer.
const dropOrphanFunctionsSQL = `
DO $$
DECLARE
    orphan regprocedure;
BEGIN
    FOR orphan IN
        SELECT p.oid::regprocedure FROM pg_proc p
        WHERE p.pronamespace = 'public'::regnamespace
          AND p.prorettype = 'trigger'::regtype
          AND NOT EXISTS (SELECT 1 FROM pg_trigger t WHERE t.tgfoid = p.oid)
    LOOP
        EXECUTE FORMAT('DROP FUNCTION %s', orphan);
    END LOOP;
END;
$$;`

// recordMigrationSQL records a migration and forgets every unit this build carries, so each stays pending until reapplied.
const recordMigrationSQL = `
WITH recorded AS (INSERT INTO schema_migrations (version) VALUES ($1))
DELETE FROM schema_triggers WHERE unit = ANY ($2)`

// Migrate applies any migrations not yet recorded, in filename order, each in its own
// transaction alongside the row recording it, and then any trigger unit the database is behind on.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	src, err := openSources()
	if err != nil {
		return err
	}

	return migrate(ctx, pool, src)
}

// sources is where a pass reads its migrations and trigger units from.
type sources struct {
	migrations fs.FS
	triggers   fs.FS
}

// migrate applies the migrations and trigger units in src.
func migrate(ctx context.Context, pool *pgxpool.Pool, src *sources) error {
	contents, err := src.list()
	if err != nil {
		return err
	}

	pending, err := isPending(ctx, pool, contents)
	if err != nil {
		return err
	}
	if !pending {
		return nil
	}

	return apply(ctx, pool, src, contents)
}

// openSources opens the embedded migrations and trigger units, each rooted at the directory
// holding them, so a version is the bare file name.
func openSources() (*sources, error) {
	migrations, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("failed to open the embedded migrations: %w", err)
	}

	triggers, err := fs.Sub(triggersFS, "triggers")
	if err != nil {
		return nil, fmt.Errorf("failed to open the embedded triggers: %w", err)
	}

	return &sources{migrations: migrations, triggers: triggers}, nil
}

// contents is every migration and trigger unit a set of sources holds, in the order they run.
type contents struct {
	migrations []string
	units      []unit
}

// unit is one trigger file, the unit it replaces and the version it carries.
type unit struct {
	name    string
	version int
	file    string
}

func (s *sources) list() (*contents, error) {
	migrations, err := listMigrationVersions(s.migrations)
	if err != nil {
		return nil, err
	}

	units, err := listUnits(s.triggers)
	if err != nil {
		return nil, err
	}

	return &contents{migrations: migrations, units: units}, nil
}

// Pending lists the embedded migrations and trigger files that have not run, in the order they
// would run in, and writes nothing.
func Pending(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	src, err := openSources()
	if err != nil {
		return nil, err
	}

	all, err := src.list()
	if err != nil {
		return nil, err
	}

	p, err := getPlan(ctx, pool, all)
	if err != nil {
		return nil, err
	}

	return p.names(), nil
}

// ErrSchemaBehind is returned when the database is missing migrations or triggers this build carries.
var ErrSchemaBehind = errors.New("the database is missing migrations this build carries; run komrad migrate as the database owner")

// CheckSchema refuses a database missing any migration or trigger version this build carries, and accepts one holding newer ones.
func CheckSchema(ctx context.Context, pool *pgxpool.Pool) error {
	src, err := openSources()
	if err != nil {
		return err
	}

	return checkSchema(ctx, pool, src)
}

func checkSchema(ctx context.Context, pool *pgxpool.Pool, src *sources) error {
	all, err := src.list()
	if err != nil {
		return err
	}

	appliedMigrations, err := listAppliedMigrations(ctx, pool)
	if err != nil {
		return err
	}

	appliedTriggers, err := listAppliedTriggers(ctx, pool)
	if err != nil {
		return err
	}

	behind := xslice.Filter(all.migrations, func(version string) bool { return !appliedMigrations[version] })
	for _, u := range all.units {
		if appliedTriggers[u.name] < u.version {
			behind = append(behind, u.file)
		}
	}
	if len(behind) > 0 {
		return fmt.Errorf("%w: %s", ErrSchemaBehind, strings.Join(behind, ", "))
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

type appliedTrigger struct {
	Unit    string
	Version int
}

// listAppliedTriggers reads the version each trigger unit was last applied at, which a database
// that has never been migrated has no ledger for yet.
func listAppliedTriggers(ctx context.Context, q queryer) (map[string]int, error) {
	rows, err := q.Query(ctx, appliedTriggersSQL)
	if isUndefinedTable(err) {
		return map[string]int{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read applied triggers: %w", err)
	}

	units, err := pgx.CollectRows(rows, pgx.RowToStructByName[appliedTrigger])
	if isUndefinedTable(err) {
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
	p, err := getPlan(ctx, pool, all)
	if err != nil {
		return false, err
	}

	return !p.empty(), nil
}

// queryer is the connection a walk of the ledger reads through, the pool or the locked
// connection.
type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// plan is what a pass has left to run.
type plan struct {
	migrations []string
	units      []unit
}

func (p *plan) empty() bool {
	return len(p.migrations) == 0 && len(p.units) == 0
}

func (p *plan) names() []string {
	return append(slices.Clone(p.migrations), xslice.Map(p.units, func(u unit) string { return u.file })...)
}

// getPlan is which migrations the database has no record of, and which trigger units it is
// behind on, every unit being reapplied when a migration runs.
func getPlan(ctx context.Context, q queryer, all *contents) (*plan, error) {
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

	return &plan{migrations: migrations, units: units}, nil
}

// apply runs the pending migrations and trigger units under the advisory lock, re-reading the
// ledgers inside it since another process may have finished the work while this one waited.
func apply(ctx context.Context, pool *pgxpool.Pool, src *sources, all *contents) error {
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

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_triggers (unit text PRIMARY KEY, version integer NOT NULL)`); err != nil {
		return fmt.Errorf("failed to create schema_triggers: %w", err)
	}

	p, err := getPlan(ctx, conn, all)
	if err != nil {
		return err
	}
	if p.empty() {
		return nil
	}

	if err := refuseSharedDatabase(ctx, conn, p.names()); err != nil {
		return err
	}

	unitNames := xslice.Map(all.units, func(u unit) string { return u.name })
	for _, version := range p.migrations {
		if err := applyFile(ctx, conn, step{
			files:  src.migrations,
			name:   version,
			record: recordMigrationSQL,
			args:   []any{version, unitNames},
		}); err != nil {
			return err
		}
	}

	for _, u := range p.units {
		if err := applyFile(ctx, conn, step{
			files:  src.triggers,
			name:   u.file,
			record: `INSERT INTO schema_triggers (unit, version) VALUES ($1, $2) ON CONFLICT (unit) DO UPDATE SET version = EXCLUDED.version`,
			args:   []any{u.name, u.version},
		}); err != nil {
			return err
		}
	}

	if len(p.units) > 0 {
		if _, err := conn.Exec(ctx, dropOrphanFunctionsSQL); err != nil {
			return fmt.Errorf("failed to drop trigger functions no trigger runs: %w", err)
		}
	}

	if _, err := conn.Exec(ctx, `DELETE FROM schema_triggers WHERE unit <> ALL ($1)`, unitNames); err != nil {
		return fmt.Errorf("failed to forget trigger units this build no longer carries: %w", err)
	}

	return nil
}

type step struct {
	files  fs.FS
	name   string
	record string
	args   []any
}

// applyFile runs one file in its own transaction alongside the statement recording it.
func applyFile(ctx context.Context, conn *pgxpool.Conn, s step) error {
	name := s.name
	sqlText, err := fs.ReadFile(s.files, name)
	if err != nil {
		return fmt.Errorf("failed to read %q: %w", name, err)
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin %q: %w", name, err)
	}

	if _, err := tx.Exec(ctx, string(sqlText)); err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("failed to apply %q: %w", name, err)
	}

	if _, err := tx.Exec(ctx, s.record, s.args...); err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("failed to record %q: %w", name, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit %q: %w", name, err)
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

	return fmt.Errorf("%w: %s is connected to it, and this build holds %d schema change(s) it has not seen (%s). "+
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

// SchemaVersion fingerprints the embedded migrations and triggers, and changes whenever one is added or edited.
func SchemaVersion() (string, error) {
	src, err := openSources()
	if err != nil {
		return "", err
	}

	return schemaVersion(src)
}

// schemaVersion hashes each migration's and trigger file's name alongside its contents.
func schemaVersion(src *sources) (string, error) {
	all, err := src.list()
	if err != nil {
		return "", err
	}

	var content []byte
	add := func(files fs.FS, name, label string) error {
		sqlText, err := fs.ReadFile(files, name)
		if err != nil {
			return fmt.Errorf("failed to read %q: %w", name, err)
		}

		content = append(content, label...)
		content = append(content, 0)
		content = append(content, sqlText...)
		content = append(content, 0)

		return nil
	}

	for _, version := range all.migrations {
		if err := add(src.migrations, version, version); err != nil {
			return "", err
		}
	}
	for _, u := range all.units {
		if err := add(src.triggers, u.file, "triggers/"+u.file); err != nil {
			return "", err
		}
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
	slices.Sort(versions)

	return versions, nil
}

// listUnits lists the trigger files, the functions first and then each table by name.
func listUnits(files fs.FS) ([]unit, error) {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil, fmt.Errorf("failed to read triggers: %w", err)
	}

	byName := map[string]unit{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		match := unitFileName.FindStringSubmatch(entry.Name())
		if match == nil {
			return nil, fmt.Errorf("trigger file %s is not named unit.vN.sql", entry.Name())
		}
		version, err := strconv.Atoi(match[2])
		if err != nil || version < 1 {
			return nil, fmt.Errorf("trigger file %s has no version above zero", entry.Name())
		}
		if other, ok := byName[match[1]]; ok {
			return nil, fmt.Errorf("trigger unit %s has two files, %s and %s", match[1], other.file, entry.Name())
		}

		byName[match[1]] = unit{name: match[1], version: version, file: entry.Name()}
	}

	units := slices.Collect(maps.Values(byName))
	slices.SortFunc(units, func(a, b unit) int {
		switch {
		case a.name == functionsUnit:
			return -1
		case b.name == functionsUnit:
			return 1
		}

		return cmp.Compare(a.name, b.name)
	})

	return units, nil
}
