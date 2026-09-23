package store_test

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"weave/db"
	"weave/internal/dbtest"
	"weave/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAPasswordCommandCarriesARotationToARunningPool pins a new password reaching new connections without a restart.
func TestAPasswordCommandCarriesARotationToARunningPool(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	database := dbtest.New(t)
	name := dbtest.EngineName(t, database)
	require.NoError(t, store.EnsureEngine(ctx, database.OwnerURL, db.EnsureEngineParams{Name: name, Password: "first"}))

	passwordFile := filepath.Join(t.TempDir(), "password")
	require.NoError(t, os.WriteFile(passwordFile, []byte("first\n"), 0o600))

	bare, err := url.Parse(database.EngineURL(t, name, "unused"))
	require.NoError(t, err)
	bare.User = url.User(db.EngineLogin(name))

	pool, err := store.Open(ctx, bare.String(), store.WithPasswordCommand("cat "+passwordFile))
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	require.NoError(t, store.EnsureEngine(ctx, database.OwnerURL, db.EnsureEngineParams{Name: name, Password: "second"}))
	require.NoError(t, os.WriteFile(passwordFile, []byte("second\n"), 0o600))
	pool.GetRawPool().Reset()

	require.NoError(t, pool.GetRawPool().Ping(ctx), "a fresh connection took the rotated password")
}

// TestAFailingPasswordCommandSaysSo covers the command's own failure reaching whoever opened the pool.
func TestAFailingPasswordCommandSaysSo(t *testing.T) {
	t.Parallel()

	_, err := store.Open(t.Context(), dbtest.URL(t), store.WithPasswordCommand("echo no vault >&2; exit 3"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no vault")
}
