package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadReadOnlyDatabaseURL covers the two logins landing on one database. The panel
// writes through the first and the MCP query tools read through the second, so a pair
// pointing at two servers answers an agent's question out of a database nothing writes to.
// Both halves connect, so nothing else in weave objects.
func TestLoadReadOnlyDatabaseURL(t *testing.T) {
	t.Run("derived from DATABASE_URL when it is not set", func(t *testing.T) {
		t.Setenv("WEAVE_READONLY_DATABASE_URL", "")
		t.Setenv("DATABASE_URL", "postgres://weave:secret@db.internal:5432/panel?sslmode=require")

		cfg, err := Load()
		require.NoError(t, err)

		assert.Equal(t,
			"postgres://weave_readonly:weave_readonly@db.internal:5432/panel?sslmode=require",
			cfg.ReadOnlyDatabaseURL,
			"the host, port, database, and options are the writable url's, and only the login differs")
	})

	t.Run("an override is taken as it stands", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://weave:secret@db.internal:5432/panel?sslmode=require")
		t.Setenv("WEAVE_READONLY_DATABASE_URL", "postgres://reader:other@pooler.internal:6432/panel?sslmode=require")

		cfg, err := Load()
		require.NoError(t, err)

		assert.Equal(t, "postgres://reader:other@pooler.internal:6432/panel?sslmode=require", cfg.ReadOnlyDatabaseURL)
	})

	t.Run("a DATABASE_URL that cannot be rewritten is left for doctor to report", func(t *testing.T) {
		t.Setenv("WEAVE_READONLY_DATABASE_URL", "")
		t.Setenv("DATABASE_URL", "host=db.internal port=5432 dbname=panel")

		cfg, err := Load()
		require.NoError(t, err,
			"weave doctor is the command a bad DATABASE_URL is diagnosed with, so loading has to survive one")

		assert.Empty(t, cfg.ReadOnlyDatabaseURL,
			"no derivation is possible and the old default would silently read another database")
	})
}

// TestLoadCacheDir covers where weave writes what it generates. The service, the panel that
// pages its log, and the launch that hands an agent a prompt file all get this from the
// same place, so a value one of them does not see may as well not be set.
func TestLoadCacheDir(t *testing.T) {
	t.Run("defaults under the user's cache directory", func(t *testing.T) {
		t.Setenv("WEAVE_CACHE_DIR", "")

		cfg, err := Load()
		require.NoError(t, err)

		cache, err := os.UserCacheDir()
		require.NoError(t, err)

		assert.Equal(t, filepath.Join(cache, "weave"), cfg.CacheDir)
		assert.Equal(t, filepath.Join(cache, "weave", "weave.log"), cfg.LogPath())
	})

	t.Run("an override is taken as it stands", func(t *testing.T) {
		t.Setenv("WEAVE_CACHE_DIR", "/var/tmp/weave-elsewhere")

		cfg, err := Load()
		require.NoError(t, err)

		assert.Equal(t, "/var/tmp/weave-elsewhere", cfg.CacheDir)
		assert.Equal(t, "/var/tmp/weave-elsewhere/weave.log", cfg.LogPath())
	})

	t.Run("a cache directory that cannot be resolved is refused", func(t *testing.T) {
		t.Setenv("WEAVE_CACHE_DIR", "")
		t.Setenv("XDG_CACHE_HOME", "")
		t.Setenv("HOME", "")

		_, err := Load()
		require.Error(t, err, "a service started without a home has nowhere to cache, and the working directory is not an answer")
		assert.Contains(t, err.Error(), "WEAVE_CACHE_DIR", "the refusal names what to set")
	})
}
