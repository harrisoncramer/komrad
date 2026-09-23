package store

import (
	"testing"

	"weave/db"
	"weave/db/models"
	"weave/db/testmodels"
	"weave/internal/dbtest"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEveryTableHasATenant fails for a table added later without a required tenant_id.
func TestEveryTableHasATenant(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := open(ctx, t)

	missing, err := testmodels.New(pool.GetRawPool()).ListTablesWithoutTenant(ctx)
	require.NoError(t, err)
	assert.Empty(t, missing, "these tables have no required tenant_id column")
}

// TestTenantStampedFromTheConnection pins an insert that never names tenant_id landing in the
// pool's tenant.
func TestTenantStampedFromTheConnection(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := open(ctx, t)

	config, err := models.New(pool.GetDB()).InsertWorkflowConfig(ctx, models.InsertWorkflowConfigParams{Harness: "claude"})
	require.NoError(t, err)

	assert.Equal(t, db.DefaultTenantID, config.TenantID)
}

// TestUnscopedInsertRefused pins a connection that never set its tenant being unable to write.
func TestUnscopedInsertRefused(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := pgx.Connect(ctx, dbtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(ctx) })

	_, err = models.New(conn).InsertWorkflowConfig(ctx, models.InsertWorkflowConfigParams{Harness: "claude"})

	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "23502", pgErr.Code)
	assert.Equal(t, "tenant_id", pgErr.ColumnName)
}
