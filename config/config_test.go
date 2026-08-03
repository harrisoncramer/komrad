package config

import (
	"os"
	"path/filepath"
	"testing"

	"weave/internal/appconfig"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadReadOnlyDatabaseURL covers the two logins landing on one database. The panel
// writes through the first and the MCP query tools read through the second, so a pair
// pointing at two servers answers an agent's question out of a database nothing writes to.
// Both halves connect, so nothing else in weave objects.
func TestLoadReadOnlyDatabaseURL(t *testing.T) {
	t.Run("derived from WEAVE_DATABASE_URL when it is not set", func(t *testing.T) {
		t.Setenv("WEAVE_READONLY_DATABASE_URL", "")
		t.Setenv("WEAVE_DATABASE_URL", "postgres://weave:secret@db.internal:5432/panel?sslmode=require")

		cfg, err := Load(nil)
		require.NoError(t, err)

		assert.Equal(t,
			"postgres://weave_readonly:weave_readonly@db.internal:5432/panel?sslmode=require",
			cfg.ReadOnlyDatabaseURL,
			"the host, port, database, and options are the writable url's, and only the login differs")
	})

	t.Run("an override is taken as it stands", func(t *testing.T) {
		t.Setenv("WEAVE_DATABASE_URL", "postgres://weave:secret@db.internal:5432/panel?sslmode=require")
		t.Setenv("WEAVE_READONLY_DATABASE_URL", "postgres://reader:other@pooler.internal:6432/panel?sslmode=require")

		cfg, err := Load(nil)
		require.NoError(t, err)

		assert.Equal(t, "postgres://reader:other@pooler.internal:6432/panel?sslmode=require", cfg.ReadOnlyDatabaseURL)
	})

	t.Run("a database url that cannot be rewritten is left for doctor to report", func(t *testing.T) {
		t.Setenv("WEAVE_READONLY_DATABASE_URL", "")
		t.Setenv("WEAVE_DATABASE_URL", "host=db.internal port=5432 dbname=panel")

		cfg, err := Load(nil)
		require.NoError(t, err,
			"weave doctor is the command a bad database url is diagnosed with, so loading has to survive one")

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

		cfg, err := Load(nil)
		require.NoError(t, err)

		cache, err := os.UserCacheDir()
		require.NoError(t, err)

		assert.Equal(t, filepath.Join(cache, "weave"), cfg.CacheDir)
		assert.Equal(t, filepath.Join(cache, "weave", "weave.log"), cfg.LogPath())
	})

	t.Run("an override is taken as it stands", func(t *testing.T) {
		t.Setenv("WEAVE_CACHE_DIR", "/var/tmp/weave-elsewhere")

		cfg, err := Load(nil)
		require.NoError(t, err)

		assert.Equal(t, "/var/tmp/weave-elsewhere", cfg.CacheDir)
		assert.Equal(t, "/var/tmp/weave-elsewhere/weave.log", cfg.LogPath())
	})

	t.Run("a cache directory that cannot be resolved is refused", func(t *testing.T) {
		t.Setenv("WEAVE_CACHE_DIR", "")
		t.Setenv("XDG_CACHE_HOME", "")
		t.Setenv("HOME", "")

		_, err := Load(nil)
		require.Error(t, err, "a service started without a home has nowhere to cache, and the working directory is not an answer")
		assert.Contains(t, err.Error(), "WEAVE_CACHE_DIR", "the refusal names what to set")
	})
}

// clearConnectionEnv unsets every variable Load resolves, so a test says what it means rather
// than inheriting whatever the shell running the suite happens to export.
func clearConnectionEnv(t *testing.T) {
	t.Helper()

	for _, name := range []string{
		"WEAVE_DATABASE_URL",
		"WEAVE_READONLY_DATABASE_URL",
		"WEAVE_ADDR",
		"WEAVE_TMUX_SOCKET",
		"WEAVE_CACHE_DIR",
	} {
		t.Setenv(name, "")
	}
}

// TestLoadPrecedence covers the whole chain: the built-in default underneath, then the config
// file, then the environment. The environment has to keep winning because the throwaway
// instance's isolation is those variables and mise cannot write a file, so a scheme where a
// config file in a home directory could beat them would quietly move that instance onto the
// development database.
func TestLoadPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		variable string
		want     string
		wantFrom Source
	}{
		{
			name:     "nothing set anywhere is the built-in default",
			want:     appconfig.DefaultAddr,
			wantFrom: SourceDefault,
		},
		{
			name:     "the file is used when the variable is not set",
			file:     "1.1.1.1:1",
			want:     "1.1.1.1:1",
			wantFrom: SourceFile,
		},
		{
			name:     "the variable beats the file",
			file:     "1.1.1.1:1",
			variable: "2.2.2.2:2",
			want:     "2.2.2.2:2",
			wantFrom: SourceEnvironment,
		},
		{
			name:     "a variable exported empty counts as unset and falls through to the file",
			file:     "1.1.1.1:1",
			variable: "",
			want:     "1.1.1.1:1",
			wantFrom: SourceFile,
		},
		{
			name:     "the variable is used when the file says nothing",
			variable: "2.2.2.2:2",
			want:     "2.2.2.2:2",
			wantFrom: SourceEnvironment,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearConnectionEnv(t)

			if tt.variable != "" {
				t.Setenv("WEAVE_ADDR", tt.variable)
			}

			file := appconfig.Defaults()
			file.Connection.Addr = tt.file

			cfg, err := Load(file)
			require.NoError(t, err)

			assert.Equal(t, tt.want, cfg.Addr)
			assert.Equal(t, tt.wantFrom, cfg.Sources["addr"], "doctor reports this, so it has to be right")
		})
	}
}

// TestLoadDerivesAfterTheFile covers the ordering that matters most. The read-only login is
// derived from whichever database url won, and deriving before the file had its say would
// hand the MCP query tools a login on the built-in default database while the panel wrote to
// the one the file names.
func TestLoadDerivesAfterTheFile(t *testing.T) {
	clearConnectionEnv(t)

	file := appconfig.Defaults()
	file.Connection.DatabaseURL = "postgres://weave:secret@db.internal:5432/panel?sslmode=require"

	cfg, err := Load(file)
	require.NoError(t, err)

	assert.Equal(t,
		"postgres://weave_readonly:weave_readonly@db.internal:5432/panel?sslmode=require",
		cfg.ReadOnlyDatabaseURL,
		"the read-only login follows the database the file named, not the built-in default")
	assert.Equal(t, SourceDerived, cfg.Sources["readonly_database_url"])
}

// TestLoadIgnoresBareDatabaseURL covers the rename. DATABASE_URL is exported on a developer's
// machine by half the tools they use, and weave reading it would let an unrelated shell decide
// which database weave writes to.
func TestLoadIgnoresBareDatabaseURL(t *testing.T) {
	clearConnectionEnv(t)
	t.Setenv("DATABASE_URL", "postgres://someone:else@elsewhere:5432/other")

	cfg, err := Load(nil)
	require.NoError(t, err)

	assert.Equal(t, appconfig.DefaultDatabaseURL, cfg.DatabaseURL,
		"only WEAVE_DATABASE_URL speaks for weave")
}

// TestLoadWithoutAFileUsesTheDefaults covers the call the tests and the commands that do not
// care about the user's config make, since a test run must not depend on what happens to be
// in the developer's home directory.
func TestLoadWithoutAFileUsesTheDefaults(t *testing.T) {
	clearConnectionEnv(t)

	cfg, err := Load(nil)
	require.NoError(t, err)

	assert.Equal(t, appconfig.DefaultDatabaseURL, cfg.DatabaseURL)
	assert.Equal(t, appconfig.DefaultAddr, cfg.Addr)
	assert.Equal(t, appconfig.DefaultTmuxSocket, cfg.TmuxSocket)
}
