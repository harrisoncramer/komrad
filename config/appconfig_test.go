package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func clearConnectionEnvironment(t *testing.T) {
	t.Helper()

	for _, name := range []string{
		"KOMRAD_DATABASE_URL",
		"KOMRAD_READONLY_DATABASE_URL",
		"KOMRAD_OWNER_DATABASE_URL",
		"KOMRAD_ADDR",
		"KOMRAD_TMUX_SOCKET",
		"KOMRAD_CACHE_DIR",
		enginesDirVariable,
		layerVariable,
	} {
		t.Setenv(name, "")
	}
}

// TestLoad pins what each shape of config file yields, a missing file falling back to the
// defaults included.
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
			name:      "what the file sets wins, and the rest still defaults",
			file:      "[harness.claude]\nflags = \"--model opus\"\n\n[keybindings.nav]\nup = [\"w\"]\n",
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

			cfg, err := LoadStack(Stack{Paths: paths})
			require.NoError(t, err)
			assert.Equal(t, tt.wantFlags, cfg.Harness.Claude.Flags)
			assert.Equal(t, DefaultHarness, cfg.DefaultHarness, "a launch goes through this, so it is never empty")
			assert.Equal(t, tt.wantNavUp, cfg.Keybindings.Nav.Up)
			assert.Equal(t, DefaultKeybindings().Nav.Down, cfg.Keybindings.Nav.Down, "unset bindings still default")
			assert.Equal(t, DefaultKeybindings().Agents.Open, cfg.Keybindings.Agents.Open, "an untouched panel still defaults")
		})
	}
}

// TestLoadLayout covers the sizes and sides the panel lays its panes out at, which tmux takes as
// either a share or a whole number of cells.
func TestLoadLayout(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		want    Layout
		wantErr bool
	}{
		{
			name:    "a negative width, which is not a width at all",
			file:    "[layout]\nsidebar_width = -20\n",
			wantErr: true,
		},

		{
			name:    "a height between a share and a row count",
			file:    "[layout]\ninfo_height = 2.5\n",
			wantErr: true,
		},
		{
			name:    "a side the list cannot sit on",
			file:    "[layout]\nsidebar_side = \"top\"\n",
			wantErr: true,
		},
		{
			name:    "a side the info pane cannot open on",
			file:    "[layout]\ninfo_side = \"right\"\n",
			wantErr: true,
		},
		{
			name: "a file that names no layout falls back to the default",
			file: "default_harness = \"claude\"\n",
			want: DefaultLayout(),
		},
		{
			name: "the width the user chose, with the rest left at the defaults",
			file: "[layout]\nsidebar_width = 40\n",
			want: Layout{
				SidebarSide:   DefaultSidebarSide,
				SidebarWidth:  40,
				InfoSide:      DefaultInfoSide,
				InfoHeight:    DefaultInfoHeight,
				DetailsHeight: DefaultDetailsHeight,
			},
		},
		{
			name: "the sides the user chose for the list and the info pane",
			file: "[layout]\nsidebar_side = \"right\"\ninfo_side = \"above\"\n",
			want: Layout{
				SidebarSide:   SidebarSideRight,
				SidebarWidth:  DefaultSidebarWidth,
				InfoSide:      InfoSideAbove,
				InfoHeight:    DefaultInfoHeight,
				DetailsHeight: DefaultDetailsHeight,
			},
		},
		{
			name: "the height the user chose for the info pane",
			file: "[layout]\ninfo_height = 0.6\n",
			want: Layout{
				SidebarSide:   DefaultSidebarSide,
				SidebarWidth:  DefaultSidebarWidth,
				InfoSide:      DefaultInfoSide,
				InfoHeight:    0.6,
				DetailsHeight: DefaultDetailsHeight,
			},
		},
		{
			name: "the height the user chose for the details under the list",
			file: "[layout]\ndetails_height = 12\n",
			want: Layout{
				SidebarSide:   DefaultSidebarSide,
				SidebarWidth:  DefaultSidebarWidth,
				InfoSide:      DefaultInfoSide,
				InfoHeight:    DefaultInfoHeight,
				DetailsHeight: 12,
			},
		},
		{
			name:    "a details height between a share and a row count",
			file:    "[layout]\ndetails_height = 2.5\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(path, []byte(tt.file), 0o644))

			cfg, err := LoadStack(Stack{Paths: []string{path}})
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.Layout)
		})
	}
}

// TestLoadUsage covers each role's limits and what an omitted key takes.
func TestLoadUsage(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		want    Usage
		wantErr string
	}{
		{
			name:    "a negative compaction point is refused",
			file:    "[usage.implementer]\nauto_compaction = -1\n",
			wantErr: "usage.implementer.auto_compaction",
		},
		{
			name:    "a negative child limit on an implementer is refused",
			file:    "[usage.implementer]\nmax_children = -1\n",
			wantErr: "usage.implementer.max_children",
		},
		{
			name:    "a negative child limit on an orchestrator is refused",
			file:    "[usage.orchestrator]\nmax_children = -2\n",
			wantErr: "usage.orchestrator.max_children",
		},
		{
			name: "a file naming none takes the built-in, with no orchestrator limit of its own",
			file: "default_harness = \"claude\"\n",
			want: NewUsage(),
		},
		{
			name: "zero hands a role's compaction back to the CLI and one role changed leaves the rest",
			file: "[usage.reviewer]\nauto_compaction = 120000\n[usage.orchestrator]\nauto_compaction = 0\nmax_children = 4\n",
			want: Usage{
				MaxAgents: DefaultMaxAgents,
				Implementer: ImplementerUsage{
					AutoCompaction: DefaultImplementerAutoCompaction,
					MaxChildren:    DefaultMaxChildren,
				},
				Reviewer:     ReviewerUsage{AutoCompaction: 120000},
				Orchestrator: OrchestratorUsage{MaxChildren: 4},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(path, []byte(tt.file), 0o644))

			cfg, err := LoadStack(Stack{Paths: []string{path}})
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.Usage)
		})
	}
}

// TestLoadMaxAgents covers the ceiling on how many agents may exist, which an omitted key
// takes the built-in for rather than reading as none.
func TestLoadMaxAgents(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		want    int
		wantErr bool
	}{
		{
			name: "a file naming none takes the built-in",
			file: "default_harness = \"claude\"\n",
			want: DefaultMaxAgents,
		},
		{
			name: "the ceiling the user chose",
			file: "[usage]\nmax_agents = 12\n",
			want: 12,
		},
		{
			// Zero reads the same as the key being absent, so it cannot mean no ceiling.
			name: "zero is an omitted key rather than a ceiling of none",
			file: "[usage]\nmax_agents = 0\n",
			want: DefaultMaxAgents,
		},
		{
			name:    "a negative ceiling is not a number of agents",
			file:    "[usage]\nmax_agents = -1\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(path, []byte(tt.file), 0o644))

			cfg, err := LoadStack(Stack{Paths: []string{path}})
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.Usage.MaxAgents)
		})
	}
}

// TestLoadRejectsUnknownKeys pins that a misspelled key is refused rather than quietly
// ignored, wherever in the file it sits.
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

			_, err := LoadStack(Stack{Paths: []string{path}})
			require.Error(t, err, "an unknown key is rejected rather than silently dropped")
			assert.Contains(t, err.Error(), "failed to parse config")
		})
	}
}

// TestLoadErrorNamesTheFile covers the path being carried in the error.
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

			_, err := LoadStack(Stack{Paths: []string{path}})

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

	_, err := LoadStack(Stack{Paths: []string{path}})

	require.Error(t, err, "a link pointing at nothing is not the same as having no config")
	assert.Contains(t, err.Error(), path, "the link to go and repoint is named")
	assert.Contains(t, err.Error(), target, "what it points at is named, since that is the part that is missing")
}

// TestLoadAbsentFileDefaults covers the case the dangling link is separated from, nothing at the
// default location.
func TestLoadAbsentFileDefaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg, err := LoadStack(Stack{})

	require.NoError(t, err, "an install with no config file comes up on the defaults")
	assert.Equal(t, DefaultKeybindings().Nav.Up, cfg.Keybindings.Nav.Up)
}

// TestLoadFrameHistoryRefusesToBeUnbounded covers frame_history at or below zero taking the default.
func TestLoadFrameHistoryRefusesToBeUnbounded(t *testing.T) {
	tests := []struct {
		name    string
		written string
		want    int
	}{
		{
			name:    "nothing written",
			written: "",
			want:    DefaultFrameHistory,
		},
		{
			name:    "zero",
			written: "[debug]\nframe_history = 0\n",
			want:    DefaultFrameHistory,
		},
		{
			name:    "negative",
			written: "[debug]\nframe_history = -1\n",
			want:    DefaultFrameHistory,
		},
		{
			name:    "a number somebody chose",
			written: "[debug]\nframe_history = 20\n",
			want:    20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(path, []byte(tt.written), 0o600))

			cfg, err := LoadStack(Stack{Paths: []string{path}})
			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.Debug.FrameHistory)
		})
	}
}

// TestLoadLogLevel covers the level the service logs at, including a word slog has no answer
// for.
func TestLoadLogLevel(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		want    string
		wantErr bool
	}{
		{
			name: "a file that says nothing about logging",
			want: DefaultLogLevel,
		},
		{
			name: "a level spelled as slog spells it",
			file: "[log]\nlevel = \"debug\"\n",
			want: "debug",
		},
		{
			name: "a level in capitals, since a config is written by hand",
			file: "[log]\nlevel = \"ERROR\"\n",
			want: "ERROR",
		},
		{
			name:    "a level that is not one",
			file:    "[log]\nlevel = \"verbose\"\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(path, []byte(tt.file), 0o644))

			cfg, err := LoadStack(Stack{Paths: []string{path}})
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.Log.Level)
		})
	}
}

// TestContextFile covers the default context path being filled in at read time.
func TestContextFile(t *testing.T) {
	tests := []struct {
		name string
		file string
		// want is the exact path when the config names one, and wantSuffix is what the
		// filled-in default has to end in.
		want       string
		wantSuffix string
	}{
		{
			name:       "no config at all falls back beside the config file",
			file:       "",
			wantSuffix: filepath.Join(".config", "komrad", "context.md"),
		},
		{
			name:       "a blank entry is the same as naming none",
			file:       "[context]\nfile = \"   \"\n",
			wantSuffix: filepath.Join(".config", "komrad", "context.md"),
		},
		{
			name: "a file the config names is taken as given",
			file: "[context]\nfile = \"/work/notes/standing.md\"\n",
			want: "/work/notes/standing.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(path, []byte(tt.file), 0o644))

			cfg, err := LoadStack(Stack{Paths: []string{path}})
			require.NoError(t, err)

			got, err := cfg.GetContextFile()
			require.NoError(t, err)

			if tt.want != "" {
				assert.Equal(t, tt.want, got)

				return
			}

			assert.True(t, strings.HasSuffix(got, tt.wantSuffix), "got %q", got)
		})
	}
}

// TestLoadNamedFileMustExist covers a --config pointing at nothing, where falling back to
// defaults would look exactly like a config that was read and ignored.
func TestLoadNamedFileMustExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	_, err := LoadStack(Stack{Paths: []string{path}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), path, "the error names the file that is not there")
}

// TestConfiguredPathsExpandHome covers the ~ a user writes a path with, which nothing below
// expands and which silently does nothing when it is left as it was typed.
func TestConfiguredPathsExpandHome(t *testing.T) {
	tests := []struct {
		name string
		file string
		// want is what the getter has to return, given the home the test set.
		want func(home string) string
		read func(Config) (string, error)
	}{
		{
			name: "a path that only begins with a tilde is not a home path",
			file: "[context]\nfile = \"~notahome/context.md\"\n",
			want: func(string) string { return "~notahome/context.md" },
			read: Config.GetContextFile,
		},
		{
			name: "a hooks directory under ~ resolves to the home directory",
			file: "[hooks]\ndir = \"~/dotfiles/komrad/hooks\"\n",
			want: func(home string) string { return filepath.Join(home, "dotfiles", "komrad", "hooks") },
			read: Config.GetHooksDir,
		},
		{
			name: "a bare ~ is the home directory itself",
			file: "[hooks]\ndir = \"~\"\n",
			want: func(home string) string { return home },
			read: Config.GetHooksDir,
		},
		{
			name: "a context file under ~ resolves to the home directory",
			file: "[context]\nfile = \"~/dotfiles/komrad/context.md\"\n",
			want: func(home string) string { return filepath.Join(home, "dotfiles", "komrad", "context.md") },
			read: Config.GetContextFile,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)

			path := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(path, []byte(tt.file), 0o644))

			cfg, err := LoadStack(Stack{Paths: []string{path}})
			require.NoError(t, err)

			got, err := tt.read(*cfg)
			require.NoError(t, err)

			assert.Equal(t, tt.want(home), got)
		})
	}
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
			files: []string{"[usage]\nmax_agents = 10\n", "[usage]\nmax_agents = 99\n"},
			check: func(t *testing.T, cfg *Config) {
				assert.Equal(t, 99, cfg.Usage.MaxAgents)
			},
		},
		{
			name:  "a scalar only the first layer sets survives",
			files: []string{"default_harness = \"codex\"\n", "[usage]\nmax_agents = 99\n"},
			check: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "codex", cfg.DefaultHarness)
				assert.Equal(t, 99, cfg.Usage.MaxAgents)
			},
		},
		{
			name:  "a table merges key by key",
			files: []string{"[debug]\nframes = true\nframe_history = 5\n", "[debug]\nframe_history = 9\n"},
			check: func(t *testing.T, cfg *Config) {
				assert.Equal(t, 9, cfg.Debug.FrameHistory, "the later layer wins the key it set")
				assert.True(t, cfg.Debug.Frames, "and leaves alone the key it did not mention")
			},
		},
		{
			name: "a list is replaced whole rather than appended to",
			files: []string{
				"[keybindings.nav]\ndown = [\"j\", \"x\"]\n",
				"[keybindings.nav]\ndown = [\"n\"]\n",
			},
			check: func(t *testing.T, cfg *Config) {
				assert.Equal(t, []string{"n"}, cfg.Keybindings.Nav.Down, "appending would leave no way to drop a key a base file set")
			},
		},
		{
			name: "a list the later layer does not mention is not reset",
			files: []string{
				"[keybindings.nav]\ndown = [\"x\"]\n",
				"[usage]\nmax_agents = 5\n",
			},
			check: func(t *testing.T, cfg *Config) {
				assert.Equal(t, []string{"x"}, cfg.Keybindings.Nav.Down)
			},
		},
		{
			name: "each keybinding is replaced on its own",
			files: []string{
				"[keybindings.menu]\nnew_agent = [\"x\"]\nlogs = [\"y\"]\n",
				"[keybindings.menu]\nnew_agent = [\"b\"]\n",
			},
			check: func(t *testing.T, cfg *Config) {
				assert.Equal(t, []string{"b"}, cfg.Keybindings.Menu.NewAgent)
				assert.Equal(t, []string{"y"}, cfg.Keybindings.Menu.Logs,
					"a binding the later layer did not mention keeps the earlier one's")
				assert.Equal(t, []string{"e"}, cfg.Keybindings.Menu.Edit,
					"and one no layer mentioned keeps its built-in default")
			},
		},
		{
			name: "three layers resolve each key to the last that set it",
			files: []string{
				"[connection]\ndatabase_url = \"postgres://one/db\"\naddr = \"127.0.0.1:1\"\n",
				"[connection]\naddr = \"127.0.0.2:2\"\n",
				"[connection]\ntmux_socket = \"three\"\n",
			},
			check: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "postgres://one/db", cfg.Connection.DatabaseURL)
				assert.Equal(t, "127.0.0.2:2", cfg.Connection.Addr)
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

			cfg, err := LoadStack(Stack{Paths: paths})
			require.NoError(t, err)

			tt.check(t, cfg)
		})
	}
}

// TestLoadConnectionPrecedence pins the order a connection setting is taken from, and that
// the report says which layer won.
func TestLoadConnectionPrecedence(t *testing.T) {
	clearConnectionEnvironment(t)
	dir := t.TempDir()
	base := filepath.Join(dir, "base.toml")
	local := filepath.Join(dir, "local.toml")
	require.NoError(t, os.WriteFile(base, []byte("[connection]\naddr = \"127.0.0.1:1\"\n"), 0o644))
	require.NoError(t, os.WriteFile(local, []byte("[connection]\naddr = \"127.0.0.2:2\"\n"), 0o644))
	t.Setenv("KOMRAD_ADDR", "127.0.0.3:3")

	cfg, report, err := LoadWithReport(Stack{Paths: []string{base, local}})
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.3:3", cfg.Connection.Addr)
	assert.Equal(t, SourceEnvironment, report.Sources["addr"])
}

// TestLoadDerivesConnectionValuesAfterPrecedence pins that a derived value is worked out from
// the setting that won rather than from the one it replaced.
func TestLoadDerivesConnectionValuesAfterPrecedence(t *testing.T) {
	clearConnectionEnvironment(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(
		"[connection]\ndatabase_url = \"postgres://komrad:secret@db.internal:5432/panel?sslmode=require\"\n",
	), 0o644))
	t.Setenv("KOMRAD_DATABASE_URL", "")
	t.Setenv("KOMRAD_READONLY_DATABASE_URL", "")
	t.Setenv("KOMRAD_CACHE_DIR", "")

	cfg, report, err := LoadWithReport(Stack{Paths: []string{path}})
	require.NoError(t, err)

	assert.Equal(t,
		"postgres://komrad_reader:secret@db.internal:5432/panel?sslmode=require",
		cfg.Connection.ReadOnlyDatabaseURL,
	)
	assert.Equal(t, SourceDerived, report.Sources["readonly_database_url"])
	assert.Equal(t, "postgres://komrad:secret@db.internal:5432/panel?sslmode=require", cfg.Connection.OwnerDatabaseURL)
	assert.Equal(t, SourceDerived, report.Sources["owner_database_url"])
	assert.NotEmpty(t, cfg.Connection.CacheDir)
	assert.Equal(t, SourceDerived, report.Sources["cache_dir"])
}

// TestLoadTakesTheLocalOwnerWithTheDefaultEngine pins komrad migrate working on a checkout that sets nothing.
func TestLoadTakesTheLocalOwnerWithTheDefaultEngine(t *testing.T) {
	clearConnectionEnvironment(t)

	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, nil, 0o644))

	cfg, err := LoadStack(Stack{Paths: []string{path}})
	require.NoError(t, err)

	assert.Equal(t, DefaultDatabaseURL, cfg.Connection.DatabaseURL)
	assert.Equal(t, DefaultOwnerDatabaseURL, cfg.Connection.OwnerDatabaseURL)
}

// TestLoadRefusesAnEmptyBinding pins the one collection an empty list cannot clear, a binding
// naming no key being an action drawn in the legend that no press reaches.
func TestLoadRefusesAnEmptyBinding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("[keybindings.nav]\nup = []\n"), 0o644))

	_, err := LoadStack(Stack{Paths: []string{path}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "nav.up")
}

// TestResolveLayers covers naming a config file replacing the default rather than adding to it.
func TestResolveLayers(t *testing.T) {
	t.Run("no paths is the one default file", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		layers, err := ResolveLayers(Stack{})
		require.NoError(t, err)

		require.Len(t, layers, 1, "komrad looks for one file and never goes hunting for a second")
		assert.Equal(t, filepath.Join(home, ".config", "komrad", "config.toml"), layers[0].Path)
		assert.False(t, layers[0].Named, "an absent default is the ordinary install rather than an error")
	})

	t.Run("named paths replace the default entirely", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())

		layers, err := ResolveLayers(Stack{Paths: []string{"/one.toml", "/two.toml"}})
		require.NoError(t, err)

		require.Len(t, layers, 2)
		assert.Equal(t, "/one.toml", layers[0].Path)
		assert.Equal(t, "/two.toml", layers[1].Path)
		assert.True(t, layers[0].Named, "a path someone typed has to be there")
	})

	t.Run("an empty path is a mistake rather than a request for the defaults", func(t *testing.T) {
		_, err := ResolveLayers(Stack{Paths: []string{""}})
		require.Error(t, err)
	})
}

// TestEveryBindingShipsAKey walks the keybindings the way DefaultKeybindings does, catching a
// field added without a default tag before it panics the panel at startup.
func TestEveryBindingShipsAKey(t *testing.T) {
	groups := reflect.ValueOf(DefaultKeybindings())

	for i := range groups.NumField() {
		group := groups.Field(i)
		for j := range group.NumField() {
			name := groups.Type().Field(i).Name + "." + group.Type().Field(j).Name

			bound, ok := group.Field(j).Interface().([]string)
			require.True(t, ok, "%s is not a key list", name)
			assert.NotEmpty(t, bound, "%s ships no key", name)

			for _, key := range bound {
				assert.NotEmpty(t, key, "%s ships an empty key", name)
			}
		}
	}
}

// TestCacheDirExpandsHome covers a cache directory written the way every other directory in the
// config is, where a literal tilde makes komrad write into a directory of that name instead.
func TestCacheDirExpandsHome(t *testing.T) {
	t.Setenv("KOMRAD_CACHE_DIR", "")

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("[connection]\ncache_dir = \"~/.cache/komrad-remote\"\n"), 0o600))

	cfg, err := LoadStack(Stack{Paths: []string{path}})
	require.NoError(t, err)

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(home, ".cache", "komrad-remote"), cfg.Connection.CacheDir)
	assert.NotContains(t, cfg.Connection.CacheDir, "~")
}

// loadStrict writes one config file and returns what Load made of it, with the connection
// environment cleared so the file is what is being read rather than the pane komrad started.
func loadStrict(t *testing.T, file string) (*Config, error) {
	t.Helper()

	clearConnectionEnvironment(t)

	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(file), 0o600))

	return LoadStack(Stack{Paths: []string{path}})
}

// TestLoadRejectsMailTheServiceCouldNotSendOn covers the two mail settings the service reads at
// send time, where a value it cannot read stops every send rather than the config being read.
func TestLoadRejectsMailTheServiceCouldNotSendOn(t *testing.T) {
	tests := []struct {
		name string
		file string
		want string
	}{
		{
			name: "a deadline in words rather than a duration",
			file: "[mail]\ndelivery_deadline = \"3 hours\"\n",
			want: "mail.delivery_deadline",
		},
		{
			name: "a deadline that has already passed",
			file: "[mail]\ndelivery_deadline = \"-5m\"\n",
			want: "mail.delivery_deadline",
		},
		{
			name: "an exchange limit below none",
			file: "[mail]\nmax_exchange = -3\n",
			want: "mail.max_exchange",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadStrict(t, tt.file)

			require.Error(t, err, "the service reads this per send, so a bad value stops mail rather than startup")
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

// TestLoadRejectsAnAddressTheApiShouldNotAnswerOn covers the address the service listens on,
// which has no auth and whose writes spawn agents.
func TestLoadRejectsAnAddressTheApiShouldNotAnswerOn(t *testing.T) {
	tests := []struct {
		name string
		file string
	}{
		{
			name: "a word rather than a host and port",
			file: "[connection]\naddr = \"nonsense\"\n",
		},
		{
			name: "a port that is not a number",
			file: "[connection]\naddr = \"127.0.0.1:http\"\n",
		},
		{
			name: "every interface, written as no host at all",
			file: "[connection]\naddr = \":8787\"\n",
		},
		{
			name: "every interface, written out",
			file: "[connection]\naddr = \"0.0.0.0:8787\"\n",
		},
		{
			name: "a host on the network",
			file: "[connection]\naddr = \"192.168.1.20:8787\"\n",
		},
		{
			name: "a name rather than an address",
			file: "[connection]\naddr = \"my-laptop.local:8787\"\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadStrict(t, tt.file)

			require.Error(t, err, "the api has no auth, so it answers on this machine only")
			assert.Contains(t, err.Error(), "connection.addr")
		})
	}
}

// TestLoadRejectsAPortOutsideTheRange covers the numbers Atoi takes and a listener does not,
// which is the class the port check exists for.
func TestLoadRejectsAPortOutsideTheRange(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:99999", "127.0.0.1:-1", "127.0.0.1:0"} {
		t.Run(addr, func(t *testing.T) {
			_, err := loadStrict(t, "[connection]\naddr = \""+addr+"\"\n")

			require.Error(t, err, "a listener refuses this, so the file should too")
			assert.Contains(t, err.Error(), "connection.addr")
		})
	}
}

// TestLoadTakesTheDefaultForAnEmptyConnection covers the settings an empty value used to survive
// as, where an empty address listens on every interface on a port nothing else can predict.
func TestLoadTakesTheDefaultForAnEmptyConnection(t *testing.T) {
	cfg, err := loadStrict(t, "[connection]\naddr = \"\"\ndatabase_url = \"\"\ntmux_socket = \"\"\n")

	require.NoError(t, err, "an empty value is how an unset setting reads, so it takes the default")
	assert.Equal(t, DefaultAddr, cfg.Connection.Addr)
	assert.Equal(t, DefaultDatabaseURL, cfg.Connection.DatabaseURL)
	assert.Equal(t, DefaultTmuxSocket, cfg.Connection.TmuxSocket)
}

// TestValidateAddrRefusesNothing covers the address arriving as a flag rather than a setting,
// where there is no default underneath it.
func TestValidateAddrRefusesNothing(t *testing.T) {
	require.Error(t, ValidateAddr(""), "an empty listen address answers on every interface")
	require.Error(t, ValidateAddr("0.0.0.0:8787"))
	require.NoError(t, ValidateAddr("127.0.0.1:8787"))
}

// TestLoadTakesEveryLoopbackAddress covers the spellings that reach only this machine.
func TestLoadTakesEveryLoopbackAddress(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:8787", "127.0.0.2:8790", "localhost:8787", "[::1]:8787"} {
		t.Run(addr, func(t *testing.T) {
			cfg, err := loadStrict(t, "[connection]\naddr = \""+addr+"\"\n")

			require.NoError(t, err)
			assert.Equal(t, addr, cfg.Connection.Addr)
		})
	}
}

// TestLoadRejectsADatabaseUrlPostgresWouldNotAnswer covers the url the read-only login is derived
// from, which comes out empty for anything else and fails a check that names something else.
func TestLoadRejectsADatabaseUrlPostgresWouldNotAnswer(t *testing.T) {
	tests := []struct {
		name string
		file string
	}{
		{
			name: "another database's scheme",
			file: "[connection]\ndatabase_url = \"mysql://root@localhost/komrad\"\n",
		},
		{
			name: "a string that is not a url",
			file: "[connection]\ndatabase_url = \"::::\"\n",
		},
		{
			name: "a path with no scheme",
			file: "[connection]\ndatabase_url = \"/var/run/postgres\"\n",
		},
		{
			name: "a postgres url naming no server",
			file: "[connection]\ndatabase_url = \"postgres://\"\n",
		},
		{
			name: "a postgres url that is only a scheme",
			file: "[connection]\ndatabase_url = \"postgres:\"\n",
		},
		{
			name: "a postgres url carrying only a query",
			file: "[connection]\ndatabase_url = \"postgres://?sslmode=disable\"\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadStrict(t, tt.file)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "connection.database_url")
		})
	}
}

// TestLoadTakesEitherPostgresScheme covers both spellings Postgres answers to.
func TestLoadTakesEitherPostgresScheme(t *testing.T) {
	for _, databaseURL := range []string{
		"postgres://komrad:komrad@localhost:5460/komrad?sslmode=disable",
		"postgresql://komrad@localhost/komrad",
	} {
		t.Run(databaseURL, func(t *testing.T) {
			cfg, err := loadStrict(t, "[connection]\ndatabase_url = \""+databaseURL+"\"\n")

			require.NoError(t, err)
			assert.NotEmpty(t, cfg.Connection.ReadOnlyDatabaseURL, "a read-only login is derived from it")
		})
	}
}
