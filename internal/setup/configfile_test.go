package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetTopLevelKey covers that the key lands above the first table and every other line survives.
func TestSetTopLevelKey(t *testing.T) {
	tests := []struct {
		name   string
		before string
		after  string
	}{
		{
			name:   "no file",
			before: "",
			after:  "default_harness = \"codex\"\n",
		},
		{
			name:   "replaces the key and keeps comments",
			before: "# mine\ndefault_harness = \"claude\" # old\n\n[layout]\nsidebar_width = 30\n",
			after:  "# mine\ndefault_harness = \"codex\"\n\n[layout]\nsidebar_width = 30\n",
		},
		{
			name:   "goes above the first table",
			before: "# mine\n[layout]\nsidebar_width = 30\n",
			after:  "# mine\ndefault_harness = \"codex\"\n\n[layout]\nsidebar_width = 30\n",
		},
		{
			name:   "a key of that name inside a table is not the top-level one",
			before: "[harness.claude]\ndefault_harness = \"x\"\n",
			after:  "default_harness = \"codex\"\n\n[harness.claude]\ndefault_harness = \"x\"\n",
		},
		{
			name:   "appended when there are no tables",
			before: "log_level = \"debug\"\n",
			after:  "log_level = \"debug\"\ndefault_harness = \"codex\"\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "komrad", "config.toml")
			if tt.before != "" {
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
				require.NoError(t, os.WriteFile(path, []byte(tt.before), 0o644))
			}

			require.NoError(t, setTopLevelKey(path, "default_harness", "codex"))

			written, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, tt.after, string(written))

			info, err := os.Stat(path)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		})
	}
}

// TestSetTopLevelKeyRefusesAFileThatWillNotParse covers that a broken file is left as it was.
func TestSetTopLevelKeyRefusesAFileThatWillNotParse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("[layout\n"), 0o600))

	require.Error(t, setTopLevelKey(path, "default_harness", "codex"))

	written, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "[layout\n", string(written), "a file setup cannot read is left as it was")
}

// TestSetTableKeys covers that each key lands inside its table and every other line survives.
func TestSetTableKeys(t *testing.T) {
	tests := []struct {
		name   string
		before string
		after  string
	}{
		{
			name:   "no file",
			before: "",
			after:  "[connection]\nrun_postgres = true\ndatabase_url = \"postgres://new\"\n",
		},
		{
			name:   "replaces a key and keeps the rest of the table",
			before: "[connection]\n# mine\ndatabase_url = \"postgres://old\" # old\naddr = \"localhost:8787\"\n",
			after:  "[connection]\n# mine\ndatabase_url = \"postgres://new\"\naddr = \"localhost:8787\"\nrun_postgres = true\n",
		},
		{
			name:   "goes at the end of the table, before the next one",
			before: "[connection]\naddr = \"localhost:8787\"\n\n[layout]\nsidebar_width = 30\n",
			after:  "[connection]\naddr = \"localhost:8787\"\nrun_postgres = true\ndatabase_url = \"postgres://new\"\n\n[layout]\nsidebar_width = 30\n",
		},
		{
			name:   "a key of that name in another table is not the one",
			before: "[layout]\ndatabase_url = \"x\"\n",
			after:  "[layout]\ndatabase_url = \"x\"\n\n[connection]\nrun_postgres = true\ndatabase_url = \"postgres://new\"\n",
		},
		{
			name:   "the table is added after the top-level keys",
			before: "default_harness = \"codex\"\n",
			after:  "default_harness = \"codex\"\n\n[connection]\nrun_postgres = true\ndatabase_url = \"postgres://new\"\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "komrad", "config.toml")
			if tt.before != "" {
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
				require.NoError(t, os.WriteFile(path, []byte(tt.before), 0o644))
			}

			require.NoError(t, setTableKeys(path, "connection", []tableKey{
				{Key: "run_postgres", Literal: "true"},
				{Key: "database_url", Literal: `"postgres://new"`},
			}))

			written, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, tt.after, string(written))
		})
	}
}
