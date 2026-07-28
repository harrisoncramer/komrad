package appconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFillKeys(t *testing.T) {
	tests := []struct {
		name string
		val  []string
		def  []string
		want []string
	}{
		{
			name: "an empty value falls back to the default",
			val:  nil,
			def:  []string{"a", "b"},
			want: []string{"a", "b"},
		},
		{
			name: "a set value is kept",
			val:  []string{"x"},
			def:  []string{"a"},
			want: []string{"x"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, fillKeys(tt.val, tt.def))
		})
	}
}

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
			in:   Keybindings{Workflows: WorkflowsKeys{Delete: []string{"x"}}},
			want: withWorkflowDelete(DefaultKeybindings(), "x"),
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
			file: "claude_flags = \"--model opus\"\n\n[keybindings.nav]\nup = [\"w\"]\n",

			wantFlags: "--model opus",
			wantNavUp: []string{"w"},
		},
		{
			name:      "a file that sets nothing leaves every default in place",
			file:      "\n",
			wantNavUp: DefaultKeybindings().Nav.Up,
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
			assert.Equal(t, tt.wantFlags, cfg.ClaudeFlags)
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
			name:    "a colour that is not hex at all",
			file:    "marks = [{ glyph = \"●\", color = \"hot pink\" }]\n",
			wantErr: true,
		},
		{
			name:    "a colour missing its hash",
			file:    "marks = [{ glyph = \"●\", color = \"F932A1\" }]\n",
			wantErr: true,
		},
		{
			name: "a file that names none falls back to the defaults",
			file: "claude_flags = \"\"\n",
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
			name: "an action the panel does not have",
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

// withNavUp is the defaults with one navigation binding replaced, which is what
// a config that overrides only that binding should come out as.
func withNavUp(k Keybindings, keys ...string) Keybindings {
	k.Nav.Up = keys

	return k
}

// withWorkflowDelete is the same idea for a binding in another panel, so a case
// can show that overriding one panel does not disturb the others.
func withWorkflowDelete(k Keybindings, keys ...string) Keybindings {
	k.Workflows.Delete = keys

	return k
}
