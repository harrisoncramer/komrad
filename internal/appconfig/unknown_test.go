package appconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadNamesWhatTheTableTakes covers the error a key nothing answers to produces. The
// panel shows that error in place of itself and nothing else, so it is the whole of what
// the reader has to work out which key to change and what to change it to.
func TestLoadNamesWhatTheTableTakes(t *testing.T) {
	tests := []struct {
		name string
		file string
		want []string
	}{
		{
			name: "a misspelled top-level key",
			file: "default_harnes = \"codex\"\n",
			want: []string{"default_harnes is not a setting; the top level takes default_harness, max_agents"},
		},
		{
			name: "a misspelled key inside a table",
			file: "[layout]\nsplitt = 0.3\n",
			want: []string{"layout.splitt is not a setting; [layout] takes split, secondary_split"},
		},
		{
			name: "a table the panel does not have",
			file: "[keybindings.list]\nselect = [\"enter\"]\n",
			want: []string{"keybindings.list is not a setting; [keybindings] takes actions, agents, archive"},
		},
		{
			name: "a harness this binary cannot run",
			file: "[harness.gemini]\nflags = \"--fast\"\n",
			want: []string{"harness.gemini is not a setting; [harness] takes claude, codex"},
		},
		{
			name: "a misspelled key inside one of the marks",
			file: "marks = [{ glyph = \"x\", colour = \"red\" }]\n",
			want: []string{"marks[0].colour is not a setting; an entry in marks takes glyph, color"},
		},
		{
			name: "several keys at once",
			file: "default_harnes = \"codex\"\n[layout]\nsplitt = 0.3\n",
			want: []string{
				"default_harnes is not a setting",
				"layout.splitt is not a setting; [layout] takes split, secondary_split",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(path, []byte(tt.file), 0o644))

			_, err := Load([]string{path})

			require.Error(t, err, "an unknown key is rejected rather than silently dropped")
			assert.Contains(t, err.Error(), path, "the file to go and edit is named")
			for _, want := range tt.want {
				assert.Contains(t, err.Error(), want)
			}
		})
	}
}

// TestLoadStillRejectsAValueItCannotRead covers the check the unknown-key reporting sits
// beside. The decoder is no longer told to fail on a key it did not use, and a value of the
// wrong type is refused by the same call, so it is worth holding that it still is.
func TestLoadStillRejectsAValueItCannotRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("max_agents = \"lots\"\n"), 0o644))

	_, err := Load([]string{path})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_agents")
}

// TestReportUnknownKeysSortsWhatItNames covers the order the keys are reported in. The
// decoder reports them in whatever order it walked the file, so without sorting the same
// broken file would name them differently from one run to the next.
func TestReportUnknownKeysSortsWhatItNames(t *testing.T) {
	err := reportUnknownKeys([]string{"layout.splitt", "default_harnes"})

	require.Error(t, err)

	first := strings.Index(err.Error(), "default_harnes")
	second := strings.Index(err.Error(), "layout.splitt")

	require.GreaterOrEqual(t, first, 0, "both keys are named")
	assert.Less(t, first, second, "the keys are named in sorted order rather than as decoded")
}

// TestReportUnknownKeysWithNothingUnused covers the ordinary case, where every key in the
// file was used and there is nothing to report.
func TestReportUnknownKeysWithNothingUnused(t *testing.T) {
	assert.NoError(t, reportUnknownKeys(nil))
}

// TestListKnownKeys covers resolving a dotted table to what it holds, which is what decides
// whether a key gets a list beside it or is reported on its own.
func TestListKnownKeys(t *testing.T) {
	tests := []struct {
		name  string
		table string
		want  []string
	}{
		{
			name:  "a table nothing answers to",
			table: "keybindings.list",
			want:  nil,
		},
		{
			name:  "a value rather than a table",
			table: "max_agents",
			want:  nil,
		},
		{
			name:  "a table one level down",
			table: "log",
			want:  []string{"level", "viewer"},
		},
		{
			name:  "a table two levels down",
			table: "keybindings.nav",
			want:  []string{"bottom", "down", "top", "up"},
		},
		{
			name:  "an entry in a list",
			table: "marks[0]",
			want:  []string{"glyph", "color"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, listKnownKeys(tt.table))
		})
	}
}
