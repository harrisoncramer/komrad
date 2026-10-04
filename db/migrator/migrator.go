// Package migrator applies the Atlas migrations and trigger units db embeds, so a deployment can
// bring its own schema up to date by running the binary rather than by having
// the atlas CLI on the box.
//
// It does this by acquiring an advisory lock (so that two migrations cannot run migrations at
// the same time) and running them each, in sequential order, against the database.
//
// The triggers are embedded alongside them as one versioned file per table, which a pass
// reapplies whenever the file's version moves past the one the database recorded.
package migrator

import (
	"cmp"
	"context"
	"crypto/sha256"
	_ "embed"
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

	"komrad/db"
	"komrad/db/isolation"
	"komrad/internal/logging"
	"komrad/lib/xslice"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed applied_migrations.sql
var appliedMigrationsSQL string

//go:embed applied_triggers.sql
var appliedTriggersSQL string

//go:embed drop_orphan_functions.sql
var dropOrphanFunctionsSQL string

//go:embed record_migration.sql
var recordMigrationSQL string

//go:embed record_trigger.sql
var recordTriggerSQL string

//go:embed create_ledgers.sql
var createLedgersSQL string

//go:embed lock.sql
var lockSQL string

//go:embed unlock.sql
var unlockSQL string

//go:embed other_komrads.sql
var otherKomradsSQL string

// migrateLock is the advisory lock key that stops two processes applying the same migration
// twice, and it must never change.
const migrateLock int64 = 7_240_115

// TriggerFunctionsUnit is the trigger unit holding the functions every other unit's triggers run.
const TriggerFunctionsUnit = "_functions"

// unlockTimeout bounds the unlock so it cannot hang on a connection whose peer is gone.
const unlockTimeout = 5 * time.Second

// versionLength is how much of the fingerprint is kept.
const versionLength = 12

var triggerFileName = regexp.MustCompile(`^(.+)\.v(\d+)\.sql$`)

// errSchemaBehind is returned when the database is missing migrations or triggers this build carries.
var errSchemaBehind = errors.New("the database is missing migrations this build carries; run komrad migrate as the database owner")

// errDatabaseInUse is returned rather than migrating a database another komrad is on.
var errDatabaseInUse = errors.New("another komrad is using this database")

// sources is where a pass reads its migrations and trigger units from.
type sources struct {
	migrations fs.FS
	triggers   fs.FS
	isolate    func(context.Context, pgx.Tx) error
}

// contents is a list of migrations and trigger units, in the order they run.
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

type appliedTrigger struct {
	Unit    string
	Version int
}

// queryer is the connection a walk of the ledger reads through, the pool or the locked
// connection.
type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// ParseTriggerFileName splits a trigger file's name into its unit and version.
func ParseTriggerFileName(name string) (string, int, error) {
	match := triggerFileName.FindStringSubmatch(name)
	if match == nil {
		return "", 0, fmt.Errorf("trigger file %s is not named unit.vN.sql", name)
	}

	version, err := strconv.Atoi(match[2])
	if err != nil || version < 1 {
		return "", 0, fmt.Errorf("trigger file %s has no version above zero", name)
	}

	return match[1], version, nil
}

// Migrate applies any migrations not yet recorded, in filename order, each in its own
// transaction alongside the row recording it, and then any trigger unit the database is behind on.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	src, err := openSources()
	if err != nil {
		return err
	}

	return migrate(ctx, pool, src)
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
	migrations, err := fs.Sub(db.Migrations, "migrations")
	if err != nil {
		return nil, fmt.Errorf("failed to open the embedded migrations: %w", err)
	}

	triggers, err := fs.Sub(db.Triggers, "triggers")
	if err != nil {
		return nil, fmt.Errorf("failed to open the embedded triggers: %w", err)
	}

	return &sources{
		migrations: migrations,
		triggers:   triggers,
		isolate:    isolation.Apply,
	}, nil
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

	plan, err := getPlan(ctx, pool, all)
	if err != nil {
		return nil, err
	}

	return plan.names(), nil
}

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

	plan, err := getPlan(ctx, pool, all)
	if err != nil {
		return err
	}
	if !plan.empty() {
		return fmt.Errorf("%w: %s", errSchemaBehind, strings.Join(plan.names(), ", "))
	}

	return nil
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

// apply runs the pending migrations and trigger units under the advisory lock, re-reading the
// ledgers inside it since another process may have finished the work while this one waited.
func apply(ctx context.Context, pool *pgxpool.Pool, src *sources, all *contents) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire a connection to migrate: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, lockSQL, migrateLock); err != nil {
		return fmt.Errorf("failed to take the migration lock: %w", err)
	}
	defer releaseMigrateLock(ctx, conn)

	if _, err := conn.Exec(ctx, createLedgersSQL); err != nil {
		return fmt.Errorf("failed to create the migration ledgers: %w", err)
	}

	plan, err := getPlan(ctx, conn, all)
	if err != nil {
		return err
	}
	if plan.empty() {
		return nil
	}

	if err := refuseSharedDatabase(ctx, conn, plan.names()); err != nil {
		return err
	}

	unitNames := xslice.Map(all.units, func(u unit) string { return u.name })
	for _, version := range plan.migrations {
		if err := applyFile(ctx, conn, applyFileParams{
			files:   src.migrations,
			name:    version,
			record:  recordMigrationSQL,
			args:    []any{version, unitNames},
			isolate: src.isolate,
		}); err != nil {
			return err
		}
	}

	for _, u := range plan.units {
		if err := applyFile(ctx, conn, applyFileParams{
			files:  src.triggers,
			name:   u.file,
			record: recordTriggerSQL,
			args:   []any{u.name, u.version},
		}); err != nil {
			return err
		}
	}

	if len(plan.units) > 0 {
		if _, err := conn.Exec(ctx, dropOrphanFunctionsSQL); err != nil {
			return fmt.Errorf("failed to drop trigger functions no trigger runs: %w", err)
		}
	}

	return nil
}

type applyFileParams struct {
	files   fs.FS
	name    string
	record  string
	args    []any
	isolate func(context.Context, pgx.Tx) error
}

// applyFile runs one file in its own transaction alongside the statement recording it.
func applyFile(ctx context.Context, conn *pgxpool.Conn, params applyFileParams) error {
	sqlText, err := fs.ReadFile(params.files, params.name)
	if err != nil {
		return fmt.Errorf("failed to read %q: %w", params.name, err)
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin %q: %w", params.name, err)
	}

	if _, err := tx.Exec(ctx, string(sqlText)); err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("failed to apply %q: %w", params.name, err)
	}

	if params.isolate != nil {
		if err := params.isolate(ctx, tx); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("failed to isolate engines after %q: %w", params.name, err)
		}
	}

	if _, err := tx.Exec(ctx, params.record, params.args...); err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("failed to record %q: %w", params.name, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit %q: %w", params.name, err)
	}

	return nil
}

// refuseSharedDatabase stops a pass that would change the schema under a komrad already
// running against the same database.
func refuseSharedDatabase(ctx context.Context, conn *pgxpool.Conn, pending []string) error {
	rows, err := conn.Query(ctx, otherKomradsSQL, db.AppNamePrefix+"/%", db.AppName())
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
		errDatabaseInUse, strings.Join(others, ", "), len(pending), strings.Join(pending, ", "))
}

// releaseMigrateLock unlocks on a context of its own.
func releaseMigrateLock(ctx context.Context, conn *pgxpool.Conn) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), unlockTimeout)
	defer cancel()

	if _, err := conn.Exec(ctx, unlockSQL, migrateLock); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to release the migration lock", logging.Err(err)...)
	}
}

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

		name, version, err := ParseTriggerFileName(entry.Name())
		if err != nil {
			return nil, err
		}
		if other, ok := byName[name]; ok {
			return nil, fmt.Errorf("trigger unit %s has two files, %s and %s", name, other.file, entry.Name())
		}

		byName[name] = unit{
			name:    name,
			version: version,
			file:    entry.Name(),
		}
	}

	units := slices.Collect(maps.Values(byName))
	slices.SortFunc(units, func(a, b unit) int {
		switch {
		case a.name == TriggerFunctionsUnit:
			return -1
		case b.name == TriggerFunctionsUnit:
			return 1
		}

		return cmp.Compare(a.name, b.name)
	})

	return units, nil
}
