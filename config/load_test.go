package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func clearConnectionEnvironment(t *testing.T) {
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

func TestLoad(t *testing.T) {
	tests := []struct {
		name string
		// file is written to a temp path and passed to Load, or when empty leaves Load to
		// fall back to the default path under a bare HOME.
		file      string
		wantFlags string
		wantNavUp []string
	}{
		{
			name:      "a missing file is not an error and yields the defaults",
			wantNavUp: DefaultKeybindings().Nav.Up,
		},
		{
			name: "what the file sets wins, and the rest still defaults",
			file: "[harness.claude]\nflags = \"--model opus\"\n\n[keybindings.nav]\nup = [\"w\"]\n",

			wantFlags: "--model opus",
			wantNavUp: []string{"w"},
		},
		{
			name:      "a file that sets nothing leaves every default in place",
			file:      "\n",
			wantNavUp: DefaultKeybindings().Nav.Up,
		},
		{
			// The id every launch goes through, so a file that names none has to
			// end up on one rather than leaving creates to be refused.
			name:      "a file that names no harness still lands on the default one",
			file:      "[keybindings.nav]\nup = [\"w\"]\n",
			wantNavUp: []string{"w"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var paths []string
			if tt.file != "" {
				path := filepath.Join(t.TempDir(), "config.toml")
				require.NoError(t, os.WriteFile(path, []byte(tt.file), 0o644))
				paths = []string{path}
			} else {
				t.Setenv("HOME", t.TempDir())
			}

			cfg, err := Load(paths)
			require.NoError(t, err)
			assert.Equal(t, tt.wantFlags, cfg.Harness.Claude.Flags)
			assert.Equal(t, DefaultHarness, cfg.DefaultHarness, "a launch goes through this, so it is never empty")
			assert.Equal(t, tt.wantNavUp, cfg.Keybindings.Nav.Up)
			assert.Equal(t, DefaultKeybindings().Nav.Down, cfg.Keybindings.Nav.Down, "unset bindings still default")
			assert.Equal(t, DefaultKeybindings().Agents.Open, cfg.Keybindings.Agents.Open, "an untouched panel still defaults")
		})
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	tests := []struct {
		name string
		file string
	}{
		{
			name: "a misspelled top-level key",
			file: "claude_flgas = \"--model opus\"\n",
		},
		{
			name: "a binding the panel does not have",
			file: "[keybindings.nav]\npreview_up = [\"ctrl+u\"]\n",
		},
		{
			name: "a section the panel does not have",
			file: "[keybindings.list]\nselect = [\"enter\"]\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(path, []byte(tt.file), 0o644))

			_, err := Load([]string{path})
			require.Error(t, err, "an unknown key is rejected rather than silently dropped")
			assert.Contains(t, err.Error(), "failed to parse config")
		})
	}
}

// TestLoadErrorNamesTheFile covers the path being carried in the error, since the panel
// shows that error and nothing else to say which file to open.
func TestLoadErrorNamesTheFile(t *testing.T) {
	tests := []struct {
		name string
		file string
		want string
	}{
		{
			name: "a file that is not toml at all",
			file: "keybindings = [\n",
			want: "failed to read config",
		},
		{
			name: "a file that parses but sets a key the panel does not have",
			file: "claude_flgas = \"--model opus\"\n",
			want: "failed to parse config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(path, []byte(tt.file), 0o644))

			_, err := Load([]string{path})

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			assert.Contains(t, err.Error(), path, "the file to go and edit is named")
		})
	}
}

// TestLoadDanglingSymlink covers a config path that is a symlink to nothing, where falling
// back quietly would look exactly like the config being honoured.
func TestLoadDanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	target := filepath.Join(dir, "missing", "config.toml")
	require.NoError(t, os.Symlink(target, path))

	_, err := Load([]string{path})

	require.Error(t, err, "a link pointing at nothing is not the same as having no config")
	assert.Contains(t, err.Error(), path, "the link to go and repoint is named")
	assert.Contains(t, err.Error(), target, "what it points at is named, since that is the part that is missing")
}

// TestLoadAbsentFileDefaults covers the case the dangling link is separated from, nothing
// at the default location, which is every install that never wrote a config and has to
// come up on the built-in bindings.
func TestLoadAbsentFileDefaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg, err := Load(nil)

	require.NoError(t, err, "an install with no config file comes up on the defaults")
	assert.Equal(t, DefaultKeybindings().Nav.Up, cfg.Keybindings.Nav.Up)
}

// TestLoadNamedFileMustExist covers a --config pointing at nothing, where falling back to
// defaults would look exactly like a config that was read and ignored.
func TestLoadNamedFileMustExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	_, err := Load([]string{path})

	require.Error(t, err)
	assert.Contains(t, err.Error(), path, "the error names the file that is not there")
}

// TestLoadMergesLayers covers tables merging key by key and lists being replaced whole,
// which is the whole of what layering means to somebody writing two files.
func TestLoadMergesLayers(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		check func(t *testing.T, cfg *Config)
	}{
		{
			name:  "a later layer wins a scalar",
			files: []string{"max_agents = 10\n", "max_agents = 99\n"},
			check: func(t *testing.T, cfg *Config) {
				assert.Equal(t, 99, cfg.MaxAgents)
			},
		},
		{
			name:  "a scalar only the first layer sets survives",
			files: []string{"default_harness = \"codex\"\n", "max_agents = 99\n"},
			check: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "codex", cfg.DefaultHarness)
				assert.Equal(t, 99, cfg.MaxAgents)
			},
		},
		{
			name:  "a table merges key by key",
			files: []string{"[log]\nlevel = \"warn\"\nviewer = \"less\"\n", "[log]\nlevel = \"debug\"\n"},
			check: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "debug", cfg.Log.Level, "the later layer wins the key it set")
				assert.Equal(t, "less", cfg.Log.Viewer, "and leaves alone the key it did not mention")
			},
		},
		{
			name:  "a list is replaced whole rather than appended to",
			files: []string{"tags = [{ glyph = \"a\" }, { glyph = \"b\" }]\n", "tags = [{ glyph = \"c\" }]\n"},
			check: func(t *testing.T, cfg *Config) {
				require.Len(t, cfg.Tags, 1, "appending would leave no way to drop a tag a base file set")
				assert.Equal(t, "c", cfg.Tags[0].Glyph)
			},
		},
		{
			name: "a list the later layer does not mention is not reset",
			files: []string{
				"tags = [{ glyph = \"a\" }]\n",
				"max_agents = 5\n",
			},
			check: func(t *testing.T, cfg *Config) {
				require.Len(t, cfg.Tags, 1)
				assert.Equal(t, "a", cfg.Tags[0].Glyph)
			},
		},
		{
			name: "each keybinding is replaced on its own",
			files: []string{
				"[keybindings.agents]\nnew = [\"a\"]\nrename = [\"b\"]\n",
				"[keybindings.agents]\nnew = [\"c\"]\n",
			},
			check: func(t *testing.T, cfg *Config) {
				assert.Equal(t, []string{"c"}, cfg.Keybindings.Agents.New)
				assert.Equal(t, []string{"b"}, cfg.Keybindings.Agents.Rename,
					"a binding the later layer did not mention keeps the earlier one's")
				assert.Equal(t, []string{"z"}, cfg.Keybindings.Agents.Archive,
					"and one no layer mentioned keeps its built-in default")
			},
		},
		{
			name: "three layers resolve each key to the last that set it",
			files: []string{
				"[connection]\ndatabase_url = \"postgres://one/db\"\naddr = \"1.1.1.1:1\"\n",
				"[connection]\naddr = \"2.2.2.2:2\"\n",
				"[connection]\ntmux_socket = \"three\"\n",
			},
			check: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "postgres://one/db", cfg.Connection.DatabaseURL)
				assert.Equal(t, "2.2.2.2:2", cfg.Connection.Addr)
				assert.Equal(t, "three", cfg.Connection.TmuxSocket)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearConnectionEnvironment(t)
			dir := t.TempDir()

			paths := make([]string, 0, len(tt.files))
			for i, body := range tt.files {
				path := filepath.Join(dir, fmt.Sprintf("layer%d.toml", i))
				require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
				paths = append(paths, path)
			}

			cfg, err := Load(paths)
			require.NoError(t, err)

			tt.check(t, cfg)
		})
	}
}

func TestLoadConnectionPrecedence(t *testing.T) {
	clearConnectionEnvironment(t)
	dir := t.TempDir()
	base := filepath.Join(dir, "base.toml")
	local := filepath.Join(dir, "local.toml")
	require.NoError(t, os.WriteFile(base, []byte("[connection]\naddr = \"1.1.1.1:1\"\n"), 0o644))
	require.NoError(t, os.WriteFile(local, []byte("[connection]\naddr = \"2.2.2.2:2\"\n"), 0o644))
	t.Setenv("WEAVE_ADDR", "3.3.3.3:3")

	cfg, report, err := LoadWithReport([]string{base, local})
	require.NoError(t, err)

	assert.Equal(t, "3.3.3.3:3", cfg.Connection.Addr)
	assert.Equal(t, SourceEnvironment, report.Sources["addr"])
}

func TestLoadDerivesConnectionValuesAfterPrecedence(t *testing.T) {
	clearConnectionEnvironment(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(
		"[connection]\ndatabase_url = \"postgres://weave:secret@db.internal:5432/panel?sslmode=require\"\n",
	), 0o644))
	t.Setenv("WEAVE_DATABASE_URL", "")
	t.Setenv("WEAVE_READONLY_DATABASE_URL", "")
	t.Setenv("WEAVE_CACHE_DIR", "")

	cfg, report, err := LoadWithReport([]string{path})
	require.NoError(t, err)

	assert.Equal(t,
		"postgres://weave_readonly:weave_readonly@db.internal:5432/panel?sslmode=require",
		cfg.Connection.ReadOnlyDatabaseURL,
	)
	assert.Equal(t, SourceDerived, report.Sources["readonly_database_url"])
	assert.NotEmpty(t, cfg.Connection.CacheDir)
	assert.Equal(t, SourceDerived, report.Sources["cache_dir"])
}

func TestLoadExplicitEmptyCollectionReplacesDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("tags = []\n[keybindings.nav]\nup = []\n"), 0o644))

	cfg, err := Load([]string{path})
	require.NoError(t, err)

	assert.Empty(t, cfg.Tags)
	assert.Empty(t, cfg.Keybindings.Nav.Up)
	assert.Equal(t, DefaultKeybindings().Nav.Down, cfg.Keybindings.Nav.Down)
}

// TestResolveLayers covers naming a config file replacing the default rather than adding to
// it, which is what lets a second instance be isolated from the first one's config.
func TestResolveLayers(t *testing.T) {
	t.Run("no paths is the one default file", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		layers, err := ResolveLayers(nil)
		require.NoError(t, err)

		require.Len(t, layers, 1, "weave looks for one file and never goes hunting for a second")
		assert.Equal(t, filepath.Join(home, ".config", "weave", "config.toml"), layers[0].Path)
		assert.False(t, layers[0].Named, "an absent default is the ordinary install rather than an error")
	})

	t.Run("named paths replace the default entirely", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())

		layers, err := ResolveLayers([]string{"/one.toml", "/two.toml"})
		require.NoError(t, err)

		require.Len(t, layers, 2)
		assert.Equal(t, "/one.toml", layers[0].Path)
		assert.Equal(t, "/two.toml", layers[1].Path)
		assert.True(t, layers[0].Named, "a path someone typed has to be there")
	})

	t.Run("an empty path is a mistake rather than a request for the defaults", func(t *testing.T) {
		_, err := ResolveLayers([]string{""})
		require.Error(t, err)
	})
}
