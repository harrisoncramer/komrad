// Package dbtest hands a test a database with weave's schema already in it, copied from a
// template rather than migrated from empty.
package dbtest

import (
	"context"
	"testing"

	"weave/db"
	"weave/internal/pgtest"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// TenantID is the tenant every migrated test database already holds.
const TenantID = "00000000-0000-7000-8000-000000000001"

// URL is a migrated database of this test's own, dropped when the test ends.
func URL(t *testing.T) string {
	t.Helper()

	version, err := db.SchemaVersion()
	require.NoError(t, err)

	return pgtest.Migrated(t, pgtest.Schema{Version: version, Apply: migrate})
}

// migrate applies the embedded migrations to the database url names.
func migrate(ctx context.Context, url string) error {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return err
	}
	defer pool.Close()

	return db.Migrate(ctx, pool)
}

// Pool is a pool on a migrated database of this test's own, acting for the default tenant.
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	cfg, err := pgxpool.ParseConfig(URL(t))
	require.NoError(t, err)
	db.ScopeToTenant(cfg, TenantID)

	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	return pool
}
