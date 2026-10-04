// Package isolation scopes every engine table to the engine writing it, deriving the policy,
// trigger and grants from the schema rather than from each migration.
package isolation

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// StampEngineTrigger is the trigger and function that stamp each new row with the writing engine.
const StampEngineTrigger = "stamp_engine"

// UnscopedTables are the public tables that hold no engine's rows and so carry no engine_id.
var UnscopedTables = []string{"engines", "schema_migrations", "schema_triggers"}

// UnisolatedTables are the tables that carry engine_id yet are read across engines.
var UnisolatedTables = []string{"engine_logins"}

// ReaderlessTables are the engine tables the reader role is not granted.
var ReaderlessTables = []string{"forge_review_comments", "forge_review_threads"}

//go:embed tables_without_engine.sql
var tablesWithoutEngineSQL string

//go:embed isolation_settings.sql
var isolationSettingsSQL string

//go:embed isolation.sql
var isolationSQL string

// Apply settles row level security, the isolation policy, the stamp trigger and the grants on every engine table.
func Apply(ctx context.Context, tx pgx.Tx) error {
	tablesWithoutEngineRows, err := tx.Query(ctx, tablesWithoutEngineSQL, UnscopedTables)
	if err != nil {
		return fmt.Errorf("failed to list tables without engine_id: %w", err)
	}

	tablesWithoutEngine, err := pgx.CollectRows(tablesWithoutEngineRows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("failed to read tables without engine_id: %w", err)
	}
	if len(tablesWithoutEngine) > 0 {
		return fmt.Errorf("these tables have no required engine_id and are not listed in isolation.UnscopedTables: %s", strings.Join(tablesWithoutEngine, ", "))
	}

	if _, err := tx.Exec(ctx, isolationSettingsSQL, UnisolatedTables, ReaderlessTables); err != nil {
		return fmt.Errorf("failed to pass the excepted tables to engine isolation: %w", err)
	}

	if _, err := tx.Exec(ctx, isolationSQL); err != nil {
		return fmt.Errorf("failed to isolate engine tables: %w", err)
	}

	return nil
}
