package tenant

import (
	"testing"

	"weave/config"
	"weave/db"
	"weave/internal/dbtest"
	"weave/internal/store"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListOtherHostsNamesAnotherMachine pins a weave on another host being seen, and this host never.
func TestListOtherHostsNamesAnotherMachine(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	url := dbtest.URL(t)

	pool, err := store.Open(ctx, url, config.DefaultTenantID)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	tenants := NewTenantStore(pool.GetDB())

	alone, err := tenants.ListOtherHosts(ctx)
	require.NoError(t, err)
	assert.Empty(t, alone, "this host's own connections are not another machine")

	cfg, err := pgx.ParseConfig(url)
	require.NoError(t, err)
	cfg.RuntimeParams["application_name"] = "weave/elsewhere/1"
	other, err := pgx.ConnectConfig(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = other.Close(ctx) })

	hosts, err := tenants.ListOtherHosts(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"elsewhere"}, hosts)
	assert.NotContains(t, hosts, db.AppHost())
}
