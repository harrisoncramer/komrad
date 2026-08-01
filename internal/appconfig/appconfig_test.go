package appconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithDefaults(t *testing.T) {
	tests := []struct {
		name string
		in   Keybindings
		want Keybindings
	}{
		{
			name: "an empty config becomes the full defaults",
			in:   Keybindings{},
			want: DefaultKeybindings(),
		},
		{
			name: "a set binding is kept while the rest fill in",
			in:   Keybindings{Nav: NavKeys{Up: []string{"w"}}},
			want: withNavUp(DefaultKeybindings(), "w"),
		},
		{
			name: "an override in one panel leaves the other panels alone",
			in:   Keybindings{Workflows: WorkflowsKeys{Archive: []string{"x"}}},
			want: withWorkflowArchive(DefaultKeybindings(), "x"),
		},
		{
			// A binding written out as an empty list is a binding nobody set, not a
			// request for a key that does nothing.
			name: "a binding set to no keys at all falls back like an absent one",
			in:   Keybindings{Nav: NavKeys{Up: []string{}}},
			want: DefaultKeybindings(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.in.withDefaults())
		})
	}
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name string
		// file is written to a temp path and passed to Load. Empty means no file is
		// written and Load falls back to the default location under a bare HOME.
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
			// The id every launch is resolved through, so a file that names none has to
			// end up on one rather than leaving creates to be refused.
			name:      "a file that names no harness still lands on the default one",
			file:      "[keybindings.nav]\nup = [\"w\"]\n",
			wantNavUp: []string{"w"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var path string
			if tt.file != "" {
				path = filepath.Join(t.TempDir(), "config.toml")
				require.NoError(t, os.WriteFile(path, []byte(tt.file), 0o644))
			} else {
				t.Setenv("HOME", t.TempDir())
			}

			cfg, err := Load(path)
			require.NoError(t, err)
			assert.Equal(t, tt.wantFlags, cfg.Harness.Claude.Flags)
			assert.Equal(t, DefaultHarness, cfg.DefaultHarness, "a launch is resolved through this, so it is never empty")
			assert.Equal(t, tt.wantNavUp, cfg.Keybindings.Nav.Up)
			assert.Equal(t, DefaultKeybindings().Nav.Down, cfg.Keybindings.Nav.Down, "unset bindings still default")
			assert.Equal(t, DefaultKeybindings().Agents.Open, cfg.Keybindings.Agents.Open, "an untouched panel still defaults")
		})
	}
}

// TestLoadMarks covers the marks list. A glyph goes into a rendered row, so it
// cannot carry anything a terminal would read as an instruction, and a colour has to
// be one lipgloss understands or the mark would quietly lose it.
func TestLoadMarks(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		want    []Mark
		wantErr bool
	}{
		{
			name:    "a glyph carrying a tab, which would break the row it is drawn on",
			file:    "marks = [{ glyph = \"a\\tb\" }]\n",
			wantErr: true,
		},
		{
			name:    "an empty glyph, which would draw as nothing at all",
			file:    "marks = [{ glyph = \"\" }]\n",
			wantErr: true,
		},
		{
			name:    "the same glyph twice, which would sit twice in the cycle",
			file:    "marks = [{ glyph = \"●\" }, { glyph = \"●\" }]\n",
			wantErr: true,
		},
		{
			name:    "a colour that is neither a name nor hex",
			file:    "marks = [{ glyph = \"●\", color = \"hot pink\" }]\n",
			wantErr: true,
		},
		{
			name:    "a colour missing its hash",
			file:    "marks = [{ glyph = \"●\", color = \"F932A1\" }]\n",
			wantErr: true,
		},
		{
			name:    "a colour named as an index rather than by name, which is the panel's own spelling and not a config's",
			file:    "marks = [{ glyph = \"●\", color = \"4\" }]\n",
			wantErr: true,
		},
		{
			name: "a file that names none falls back to the defaults",
			file: "default_harness = \"claude\"\n",
			want: DefaultMarks(),
		},
		{
			name: "the glyphs the user chose, in their order, with their colours",
			file: "marks = [{ glyph = \"◐\", color = \"#F932A1\" }, { glyph = \"✓\", color = \"#0f0\" }]\n",
			want: []Mark{
				{Glyph: "◐", Color: "#F932A1"},
				{Glyph: "✓", Color: "#0f0"},
			},
		},
		{
			name: "colours named from the sixteen, which follow the terminal's own scheme",
			file: "marks = [{ glyph = \"◐\", color = \"blue\" }, { glyph = \"✓\", color = \"brightmagenta\" }]\n",
			want: []Mark{
				{Glyph: "◐", Color: "blue"},
				{Glyph: "✓", Color: "brightmagenta"},
			},
		},
		{
			name: "a glyph with no colour of its own, which the panel draws in the default",
			file: "[[marks]]\nglyph = \"⚑\"\n",
			want: []Mark{{Glyph: "⚑"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(path, []byte(tt.file), 0o644))

			cfg, err := Load(path)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.Marks)
		})
	}
}

// TestLoadLayout covers the split the panel lays its two panes out at. A share of
// the whole width or more is the case worth watching, since tmux would be asked to
// leave the list nothing.
func TestLoadLayout(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		want    Layout
		wantErr bool
	}{
		{
			name:    "the whole width, which leaves the list none of it",
			file:    "[layout]\nsplit = 1\n",
			wantErr: true,
		},
		{
			name:    "more than the width there is",
			file:    "[layout]\nsplit = 1.5\n",
			wantErr: true,
		},
		{
			name:    "a negative share, which is not a share at all",
			file:    "[layout]\nsplit = -0.2\n",
			wantErr: true,
		},
		{
			name: "a file that names no layout falls back to the default",
			file: "default_harness = \"claude\"\n",
			want: DefaultLayout(),
		},
		{
			name: "the share the user chose",
			file: "[layout]\nsplit = 0.45\n",
			want: Layout{Split: 0.45},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(path, []byte(tt.file), 0o644))

			cfg, err := Load(path)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.Layout)
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
			file: "max_agents = 12\n",
			want: 12,
		},
		{
			// Zero reads the same as the key being absent, so it cannot mean no ceiling.
			name: "zero is an omitted key rather than a ceiling of none",
			file: "max_agents = 0\n",
			want: DefaultMaxAgents,
		},
		{
			name:    "a negative ceiling is not a number of agents",
			file:    "max_agents = -1\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(path, []byte(tt.file), 0o644))

			cfg, err := Load(path)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.MaxAgents)
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

			_, err := Load(path)
			require.Error(t, err, "an unknown key is rejected rather than silently dropped")
			assert.Contains(t, err.Error(), "failed to parse config")
		})
	}
}

// TestLoadErrorNamesTheFile covers the path being carried in the error. The panel
// shows that error and nothing else, so an error saying only that the config would
// not parse leaves the reader with no file to go and open.
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

			_, err := Load(path)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			assert.Contains(t, err.Error(), path, "the file to go and edit is named")
		})
	}
}

// TestLoadDanglingSymlink covers a config path that is a symlink to nothing, which
// reading fails at in the same way as a path with no file at it. A machine that was
// never configured is meant to fall back quietly, but a broken link is a config the
// user believes is in force, and defaulting on it silently is indistinguishable from
// the config being honoured with those same values.
func TestLoadDanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	target := filepath.Join(dir, "missing", "config.toml")
	require.NoError(t, os.Symlink(target, path))

	_, err := Load(path)

	require.Error(t, err, "a link resolving to nothing is not the same as having no config")
	assert.Contains(t, err.Error(), path, "the link to go and repoint is named")
	assert.Contains(t, err.Error(), target, "what it points at is named, since that is the part that is missing")
}

// TestLoadAbsentFileDefaults covers the case the dangling link is separated from: a
// path with nothing at it at all, which is every install that never wrote a config
// and has to come up on the built-in bindings.
func TestLoadAbsentFileDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	cfg, err := Load(path)

	require.NoError(t, err, "an install with no config file comes up on the defaults")
	assert.Equal(t, DefaultKeybindings().Nav.Up, cfg.Keybindings.Nav.Up)
}

// withNavUp is the defaults with one navigation binding replaced, which is what
// a config that overrides only that binding should come out as.
func withNavUp(k Keybindings, keys ...string) Keybindings {
	k.Nav.Up = keys

	return k
}

// withWorkflowArchive is the same idea for a binding in another panel, so a case
// can show that overriding one panel does not disturb the others.
func withWorkflowArchive(k Keybindings, keys ...string) Keybindings {
	k.Workflows.Archive = keys

	return k
}

// TestLoadLogLevel covers the level the service logs at. A word slog has no answer for
// would decode as debug and turn the log all the way up, so it is refused here rather
// than left to surprise somebody reading a file four times the size they expected.
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

			cfg, err := Load(path)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.Log.Level)
		})
	}
}
