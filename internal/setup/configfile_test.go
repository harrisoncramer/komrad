package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfigFileWrite covers the file setup writes from its starter template as answers arrive.
func TestConfigFileWrite(t *testing.T) {
	tests := []struct {
		name    string
		answers []func(*answers)
		after   string
	}{
		{
			name:    "a harness alone",
			answers: []func(*answers){func(a *answers) { a.DefaultHarness = "codex" }},
			after:   "default_harness = \"codex\"\n",
		},
		{
			name: "every answer",
			answers: []func(*answers){
				func(a *answers) {
					a.RunPostgres = true
					a.OwnerDatabaseURL = "postgres://owner"
					a.DatabaseURL = "postgres://engine"
				},
				func(a *answers) { a.DefaultHarness = "codex" },
				func(a *answers) { a.AutoSetup = true },
			},
			after: "default_harness = \"codex\"\nauto_setup = true\n\n[connection]\nrun_postgres = true\nowner_database_url = \"postgres://owner\"\ndatabase_url = \"postgres://engine\"\n",
		},
		{
			name: "a Postgres the person runs",
			answers: []func(*answers){func(a *answers) {
				a.OwnerDatabaseURL = "postgres://owner"
				a.DatabaseURL = "postgres://engine"
			}},
			after: "[connection]\nowner_database_url = \"postgres://owner\"\ndatabase_url = \"postgres://engine\"\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := &ConfigFile{Path: filepath.Join(t.TempDir(), "komrad", "config.toml")}

			for _, answer := range tt.answers {
				require.NoError(t, file.write(answer))
			}

			written, err := os.ReadFile(file.Path)
			require.NoError(t, err)
			assert.Equal(t, tt.after, string(written))

			info, err := os.Stat(file.Path)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		})
	}
}

// TestConfigFileWriteRefusesAFileAlreadyThere covers that setup leaves a config it did not make as it was.
func TestConfigFileWriteRefusesAFileAlreadyThere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("# mine\n"), 0o600))
	file := &ConfigFile{Path: path}

	assert.False(t, file.writable())
	require.ErrorIs(t, file.write(func(a *answers) { a.DefaultHarness = "codex" }), errConfigExists)

	written, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "# mine\n", string(written))
}
