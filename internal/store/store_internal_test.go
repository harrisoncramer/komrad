package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"komrad/db/testmodels"
	"komrad/internal/dbtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const untriggered = `
SELECT
    c.relname
FROM
    pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    JOIN pg_attribute a ON a.attrelid = c.oid
WHERE
    n.nspname = 'public'
    AND c.relkind = 'r'
    AND a.attname = 'updated_at'
    AND NOT a.attisdropped
    AND NOT EXISTS (
        SELECT
            1
        FROM
            pg_trigger t
        WHERE
            t.tgrelid = c.oid
            AND NOT t.tgisinternal
            AND t.tgfoid = 'set_updated_at'::regproc
            AND (t.tgtype & 1) <> 0
            AND (t.tgtype & 2) <> 0
            AND (t.tgtype & 16) <> 0)
ORDER BY
    c.relname`

const stamped = `
SELECT
    c.relname
FROM
    pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    JOIN pg_attribute a ON a.attrelid = c.oid
WHERE
    n.nspname = 'public'
    AND c.relkind = 'r'
    AND a.attname = 'updated_at'
    AND NOT a.attisdropped`

// TestUpdatedAtTriggers is the guarantee the migration makes, checked against the schema rather
// than against a list written here: a table added later that carries the column and is not covered
// fails this.
func TestUpdatedAtTriggers(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := open(ctx, t)

	covered := tableNames(ctx, t, pool, stamped)
	require.NotEmpty(t, covered, "no table carries updated_at, so this test proves nothing")

	missing := tableNames(ctx, t, pool, untriggered)
	assert.Empty(t, missing,
		"these tables have an updated_at column and nothing stamping it; "+
			"end your migration with SELECT attach_updated_at_triggers()")
}

// TestUpdatedAtStamped pins the trigger stamping a write that names neither updated_at nor
// the trigger, using workflow_configs since it has no foreign key to satisfy first.
func TestUpdatedAtStamped(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := open(ctx, t)
	db := pool.GetRawPool()

	var id string
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO workflow_configs (harness, created_at, updated_at)
		VALUES ('claude', now() - interval '1 hour', now() - interval '1 hour')
		RETURNING id`).Scan(&id))

	_, err := db.Exec(ctx, `UPDATE workflow_configs SET harness = 'codex' WHERE id = $1`, id)
	require.NoError(t, err)

	var createdAt, updatedAt time.Time
	require.NoError(t, db.QueryRow(ctx,
		`SELECT created_at, updated_at FROM workflow_configs WHERE id = $1`, id).
		Scan(&createdAt, &updatedAt))

	assert.True(t, updatedAt.After(createdAt),
		"updated_at %s did not move past created_at %s", updatedAt, createdAt)
}

func open(ctx context.Context, t *testing.T) *Pool {
	t.Helper()

	pool, err := Open(ctx, dbtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	return pool
}

func tableNames(ctx context.Context, t *testing.T, pool *Pool, query string) []string {
	t.Helper()

	rows, err := pool.GetRawPool().Query(ctx, query)
	require.NoError(t, err)
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		names = append(names, name)
	}
	require.NoError(t, rows.Err())

	return names
}

// TestFailureReasonIsSomethingARowCanHold covers the cut and the scrub, with Postgres refusing
// a text column that holds a NUL or half a rune.
func TestFailureReasonIsSomethingARowCanHold(t *testing.T) {
	tests := []struct {
		name     string
		cause    error
		contains string
	}{
		{
			name:  "nothing at all",
			cause: nil,
		},
		{
			name:     "a reason short enough to keep whole",
			cause:    errors.New("a hook failed: install-deps: ✓ done, then exit 3"),
			contains: "✓ done",
		},
		{
			name:  "a cut that would land inside a rune",
			cause: errors.New(strings.Repeat("a", maxFailureReasonLen-2) + "✓✓"),
		},
		{
			name:     "output carrying a NUL",
			cause:    errors.New("a hook failed:\x00 install-deps"),
			contains: "install-deps",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := FailureReason(tt.cause)

			assert.True(t, utf8.ValidString(reason), "Postgres refuses a text column that is not valid UTF-8")
			assert.NotContains(t, reason, "\x00", "and refuses one holding a NUL")
			assert.LessOrEqual(t, len(reason), maxFailureReasonLen)
			if tt.contains != "" {
				assert.Contains(t, reason, tt.contains)
			}
		})
	}
}

// TestEngineIsolationHoldsForEveryTable fails for a table or key added later that one engine could reach across.
func TestEngineIsolationHoldsForEveryTable(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	queries := testmodels.New(open(ctx, t).GetRawPool())

	tests := []struct {
		name  string
		list  func(context.Context) ([]string, error)
		cause string
	}{
		{
			name:  "engine",
			list:  queries.ListTablesWithoutEngine,
			cause: "these tables have no required engine_id",
		},
		{
			name:  "isolation",
			list:  queries.ListTablesWithoutEngineIsolation,
			cause: "these tables lack row security, the engine_isolation policy or the stamp_engine trigger",
		},
		{
			name:  "foreign keys",
			list:  queries.ListForeignKeysWithoutEngine,
			cause: "these foreign keys can point at another engine's row",
		},
		{
			name:  "set null",
			list:  queries.ListSetNullKeysClearingEngine,
			cause: "these foreign keys clear engine_id on delete; name the column in ON DELETE SET NULL (...)",
		},
		{
			name:  "unique keys",
			list:  queries.ListUniqueKeysAcrossEngines,
			cause: "these unique keys collide across engines; lead them with engine_id",
		},
		{
			name:  "grants",
			list:  queries.ListTablesEnginesCannotReach,
			cause: "komrad_engine or komrad_reader cannot reach these tables",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			offending, err := tt.list(ctx)
			require.NoError(t, err)
			assert.Empty(t, offending, tt.cause)
		})
	}
}
