package migrator

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"time"

	"komrad/db"
	"komrad/internal/logging"
	"komrad/lib/xslice"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

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

// unlockTimeout bounds the unlock so it cannot hang on a connection whose peer is gone.
const unlockTimeout = 5 * time.Second

// errDatabaseInUse is returned rather than migrating a database another komrad is on.
var errDatabaseInUse = errors.New("another komrad is using this database")

// apply runs the pending migrations and trigger units under the advisory lock, re-reading the
// ledgers inside it since another process may have finished the work while this one waited.
func (m *Migrator) apply(ctx context.Context, all *contents) error {
	conn, err := m.pool.Acquire(ctx)
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
			files:    m.migrations,
			name:     version,
			record:   recordMigrationSQL,
			args:     []any{version, unitNames},
			isolator: m.isolator,
		}); err != nil {
			return err
		}
	}

	for _, u := range plan.units {
		if err := applyFile(ctx, conn, applyFileParams{
			files:  m.triggers,
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
	files    fs.FS
	name     string
	record   string
	args     []any
	isolator isolator
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

	if params.isolator != nil {
		if err := params.isolator.Apply(ctx, tx); err != nil {
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
