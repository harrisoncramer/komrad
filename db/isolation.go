package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// StampEngineTrigger is the trigger and function that stamp each new row with the writing engine.
const StampEngineTrigger = "stamp_engine"

const engineIsolationPolicy = "engine_isolation"

// UnscopedTables are the public tables that hold no engine's rows and so carry no engine_id.
var UnscopedTables = []string{"engines", "schema_migrations", "schema_triggers"}

// UnisolatedTables are the tables that carry engine_id yet are read across engines.
var UnisolatedTables = []string{"engine_logins"}

// ReaderlessTables are the engine tables the reader role is not granted.
var ReaderlessTables = []string{"forge_review_comments", "forge_review_threads"}

const listTablesWithoutEngineSQL = `
SELECT c.relname::text
FROM pg_class c
WHERE c.relnamespace = 'public'::regnamespace
  AND c.relkind IN ('r', 'p')
  AND c.relname <> ALL ($1::text[])
  AND NOT EXISTS (
      SELECT 1 FROM pg_attribute a
      WHERE a.attrelid = c.oid
        AND a.attname = 'engine_id'
        AND a.attnotnull
        AND NOT a.attisdropped)
ORDER BY c.relname`

const listEngineTablesSQL = `
SELECT
    c.relname::text AS name,
    c.relname <> ALL ($1::text[]) AS should_isolate,
    c.relname <> ALL ($2::text[]) AS should_reader_read,
    c.relrowsecurity AS secured,
    EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid AND p.polname = $3) AS policed,
    EXISTS (SELECT 1 FROM pg_trigger t WHERE t.tgrelid = c.oid AND t.tgname = $4 AND NOT t.tgisinternal) AS stamped,
    has_table_privilege('komrad_engine', c.oid, 'SELECT')
        AND has_table_privilege('komrad_engine', c.oid, 'INSERT')
        AND has_table_privilege('komrad_engine', c.oid, 'UPDATE')
        AND has_table_privilege('komrad_engine', c.oid, 'DELETE') AS engine_writes,
    has_table_privilege('komrad_engine', c.oid, 'SELECT, INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER') AS engine_has_any,
    has_table_privilege('komrad_reader', c.oid, 'SELECT') AS reader_reads,
    has_table_privilege('komrad_reader', c.oid, 'SELECT, INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER') AS reader_has_any
FROM pg_class c
JOIN pg_attribute a ON a.attrelid = c.oid
WHERE c.relnamespace = 'public'::regnamespace
  AND c.relkind IN ('r', 'p')
  AND a.attname = 'engine_id'
  AND NOT a.attisdropped
ORDER BY c.relname`

const stampEngineFunctionSQL = `
CREATE OR REPLACE FUNCTION ` + StampEngineTrigger + `()
    RETURNS TRIGGER
    AS $$
BEGIN
    NEW.engine_id := COALESCE(current_engine(), NEW.engine_id);
    RETURN NEW;
END;
$$
LANGUAGE plpgsql;`

type engineTable struct {
	Name             string
	ShouldIsolate    bool
	ShouldReaderRead bool
	Secured          bool
	Policed          bool
	Stamped          bool
	EngineWrites     bool
	EngineHasAny     bool
	ReaderReads      bool
	ReaderHasAny     bool
}

func (t engineTable) isSettled() bool {
	if !t.ShouldIsolate {
		return !t.Secured && !t.Policed && !t.Stamped && !t.EngineHasAny && !t.ReaderHasAny
	}

	return t.Secured && t.Policed && t.Stamped && t.EngineWrites && t.ReaderReads == t.ShouldReaderRead
}

func (t engineTable) buildStatements() []string {
	name := pgx.Identifier{t.Name}.Sanitize()
	if !t.ShouldIsolate {
		return []string{
			fmt.Sprintf("DROP POLICY IF EXISTS %s ON %s;", engineIsolationPolicy, name),
			fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON %s;", StampEngineTrigger, name),
			fmt.Sprintf("ALTER TABLE %s DISABLE ROW LEVEL SECURITY;", name),
			fmt.Sprintf("REVOKE ALL ON %s FROM komrad_engine, komrad_reader;", name),
		}
	}

	reader := fmt.Sprintf("GRANT SELECT ON %s TO komrad_reader;", name)
	if !t.ShouldReaderRead {
		reader = fmt.Sprintf("REVOKE ALL ON %s FROM komrad_reader;", name)
	}

	return []string{
		fmt.Sprintf("ALTER TABLE %s ENABLE ROW LEVEL SECURITY;", name),
		fmt.Sprintf("DROP POLICY IF EXISTS %s ON %s;", engineIsolationPolicy, name),
		fmt.Sprintf("CREATE POLICY %s ON %s USING (engine_id = (SELECT current_engine())) WITH CHECK (engine_id = (SELECT current_engine()));", engineIsolationPolicy, name),
		fmt.Sprintf("CREATE OR REPLACE TRIGGER %s BEFORE INSERT ON %s FOR EACH ROW EXECUTE FUNCTION %s();", StampEngineTrigger, name, StampEngineTrigger),
		fmt.Sprintf("GRANT SELECT, INSERT, UPDATE, DELETE ON %s TO komrad_engine;", name),
		reader,
	}
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

	rows, err = tx.Query(ctx, listEngineTablesSQL, UnisolatedTables, ReaderlessTables, engineIsolationPolicy, StampEngineTrigger)
	if err != nil {
		return fmt.Errorf("failed to list engine tables: %w", err)
	}

	tables, err := pgx.CollectRows(rows, pgx.RowToStructByName[engineTable])
	if err != nil {
		return fmt.Errorf("failed to list engine tables: %w", err)
	}

	statements := []string{"SET LOCAL lock_timeout = '5s';", stampEngineFunctionSQL}
	for _, table := range tables {
		if table.isSettled() {
			continue
		}
		statements = append(statements, table.buildStatements()...)
	}

	if _, err := tx.Exec(ctx, strings.Join(statements, "\n")); err != nil {
		return fmt.Errorf("failed to isolate engine tables: %w", err)
	}

	return nil
}
