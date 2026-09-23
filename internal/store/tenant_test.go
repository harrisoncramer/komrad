package store

import (
	"testing"

	"weave/config"
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

	created, err := models.New(pool.GetDB()).InsertWorkflowConfig(ctx, models.InsertWorkflowConfigParams{Harness: "claude"})
	require.NoError(t, err)

	assert.Equal(t, config.DefaultTenantID, created.TenantID)
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

// TestOpenRecordsItsTenant pins an engine with a tenant of its own being able to write on first start.
func TestOpenRecordsItsTenant(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	tenantID, err := NewID()
	require.NoError(t, err)

	pool, err := Open(ctx, dbtest.URL(t), tenantID)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	created, err := models.New(pool.GetDB()).InsertWorkflowConfig(ctx, models.InsertWorkflowConfigParams{Harness: "claude"})
	require.NoError(t, err)

	assert.Equal(t, tenantID, created.TenantID)
}

// TestOpenRefusesATenantThatIsNotAnID pins a malformed tenant failing before any connection is made.
func TestOpenRefusesATenantThatIsNotAnID(t *testing.T) {
	t.Parallel()

	_, err := Open(t.Context(), dbtest.URL(t), "acme")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant id")
}
