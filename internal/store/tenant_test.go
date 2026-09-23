package store

import (
	"testing"

	"weave/db"
	"weave/internal/dbtest"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const unowned = `
SELECT
    c.relname
FROM
    pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE
    n.nspname = 'public'
    AND c.relkind = 'r'
    AND c.relname NOT IN ('tenants', 'schema_migrations')
    AND NOT EXISTS (
        SELECT
            1
        FROM
            pg_attribute a
        WHERE
            a.attrelid = c.oid
            AND a.attname = 'tenant_id'
            AND a.attnotnull
            AND NOT a.attisdropped)
ORDER BY
    c.relname`

// TestEveryTableHasATenant fails for a table added later without a required tenant_id.
func TestEveryTableHasATenant(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := open(ctx, t)

	assert.Empty(t, tableNames(ctx, t, pool, unowned),
		"these tables have no required tenant_id column")
}

// TestTenantStampedFromTheConnection pins an insert that never names tenant_id landing in the
// pool's tenant.
func TestTenantStampedFromTheConnection(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := open(ctx, t)

	var tenantID string
	require.NoError(t, pool.GetRawPool().QueryRow(ctx, `
		INSERT INTO workflow_configs (harness)
		VALUES ('claude')
		RETURNING tenant_id`).Scan(&tenantID))

	assert.Equal(t, db.DefaultTenantID, tenantID)
}

// TestUnscopedInsertRefused pins a connection that never set its tenant being unable to write.
func TestUnscopedInsertRefused(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := pgx.Connect(ctx, dbtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(ctx) })

	_, err = conn.Exec(ctx, `INSERT INTO workflow_configs (harness) VALUES ('claude')`)

	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "23502", pgErr.Code)
	assert.Equal(t, "tenant_id", pgErr.ColumnName)
}
