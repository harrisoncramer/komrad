package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// UnscopedTables are the public tables that hold no engine's rows and so carry no engine_id.
var UnscopedTables = []string{"engines", "schema_migrations", "schema_triggers"}

// UnisolatedTables are the tables that carry engine_id yet are read across engines.
var UnisolatedTables = []string{"engine_logins"}

// ReaderlessTables are the engine tables the reader role is not granted.
var ReaderlessTables = []string{"forge_review_comments", "forge_review_threads"}

// StampEngine names the trigger, and the function it runs, that stamps each new row with the writing engine.
const StampEngine = "stamp_engine"

const engineIsolationPolicy = "engine_isolation"

const listTablesWithoutEngineSQL = `
SELECT c.relname::text
FROM pg_class c
WHERE c.relnamespace = 'public'::regnamespace
  AND c.relkind = 'r'
  AND c.relname <> ALL ($1::text[])
  AND NOT EXISTS (
      SELECT 1 FROM pg_attribute a
      WHERE a.attrelid = c.oid
        AND a.attname = 'engine_id'
        AND a.attnotnull
        AND NOT a.attisdropped)
ORDER BY c.relname`

const listEngineTablesSQL = `
SELECT c.relname::text AS name, c.relname <> ALL ($1::text[]) AS isolated
FROM pg_class c
JOIN pg_attribute a ON a.attrelid = c.oid
WHERE c.relnamespace = 'public'::regnamespace
  AND c.relkind = 'r'
  AND a.attname = 'engine_id'
  AND NOT a.attisdropped
ORDER BY c.relname`

const stampEngineFunctionSQL = `
CREATE OR REPLACE FUNCTION stamp_engine()
    RETURNS TRIGGER
    AS $$
BEGIN
    NEW.engine_id := COALESCE(current_engine(), NEW.engine_id);
    RETURN NEW;
END;
$$
LANGUAGE plpgsql;`

type engineTable struct {
	Name     string
	Isolated bool
}

func isolateEngines(ctx context.Context, tx pgx.Tx) error {
	rows, err := tx.Query(ctx, listTablesWithoutEngineSQL, UnscopedTables)
	if err != nil {
		return fmt.Errorf("failed to list tables without engine_id: %w", err)
	}

	unscoped, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("failed to list tables without engine_id: %w", err)
	}
	if len(unscoped) > 0 {
		return fmt.Errorf("these tables have no required engine_id and are not listed in db.UnscopedTables: %s", strings.Join(unscoped, ", "))
	}

	rows, err = tx.Query(ctx, listEngineTablesSQL, UnisolatedTables)
	if err != nil {
		return fmt.Errorf("failed to list engine tables: %w", err)
	}

	tables, err := pgx.CollectRows(rows, pgx.RowToStructByName[engineTable])
	if err != nil {
		return fmt.Errorf("failed to list engine tables: %w", err)
	}

	statements := []string{stampEngineFunctionSQL}
	for _, table := range tables {
		name := pgx.Identifier{table.Name}.Sanitize()
		if !table.Isolated {
			statements = append(statements,
				fmt.Sprintf("DROP POLICY IF EXISTS %s ON %s;", engineIsolationPolicy, name),
				fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON %s;", StampEngine, name),
				fmt.Sprintf("ALTER TABLE %s DISABLE ROW LEVEL SECURITY;", name),
			)

			continue
		}

		statements = append(statements,
			fmt.Sprintf("ALTER TABLE %s ENABLE ROW LEVEL SECURITY;", name),
			fmt.Sprintf("DROP POLICY IF EXISTS %s ON %s;", engineIsolationPolicy, name),
			fmt.Sprintf("CREATE POLICY %s ON %s USING (engine_id = (SELECT current_engine())) WITH CHECK (engine_id = (SELECT current_engine()));", engineIsolationPolicy, name),
			fmt.Sprintf("CREATE OR REPLACE TRIGGER %s BEFORE INSERT ON %s FOR EACH ROW EXECUTE FUNCTION %s();", StampEngine, name, StampEngine),
		)
	}

	if _, err := tx.Exec(ctx, strings.Join(statements, "\n")); err != nil {
		return fmt.Errorf("failed to isolate engine tables: %w", err)
	}

	return nil
}
