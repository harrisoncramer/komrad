package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
