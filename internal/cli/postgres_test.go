package cli

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"komrad/config"
	"komrad/internal/postgres"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRequirePostgres covers which commands refuse to run while komrad's own Postgres is down.
func TestRequirePostgres(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		runPostgres bool
		running     bool
		wantErr     bool
	}{
		{
			name:        "migrate with Postgres down",
			command:     "migrate",
			runPostgres: true,
			wantErr:     true,
		},
		{
			name:        "engine ensure with Postgres down",
			command:     "engine ensure",
			runPostgres: true,
			wantErr:     true,
		},
		{
			name:        "engine grant with Postgres down",
			command:     "engine grant",
			runPostgres: true,
			wantErr:     true,
		},
		{
			name:        "engine revoke with Postgres down",
			command:     "engine revoke",
			runPostgres: true,
			wantErr:     true,
		},
		{
			name:        "serve with Postgres down",
			command:     "serve",
			runPostgres: true,
			wantErr:     true,
		},
		{
			name:        "migrate with Postgres up",
			command:     "migrate",
			runPostgres: true,
			running:     true,
		},
		{name: "migrate against a Postgres of the person's", command: "migrate"},
		{
			name:        "start, which brings Postgres up",
			command:     "start",
			runPostgres: true,
		},
		{
			name:        "stop, which needs no database",
			command:     "stop",
			runPostgres: true,
		},
		{
			name:        "doctor, which reports Postgres down itself",
			command:     "doctor",
			runPostgres: true,
		},
		{
			name:        "panel, which starts the server the way start does",
			command:     "panel",
			runPostgres: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateDir, err := os.MkdirTemp("/tmp", "komrad") //nolint:usetesting // a socket path under the test directory runs past the length unix allows
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, os.RemoveAll(stateDir)) })

			if tt.running {
				dir := postgres.Dir(stateDir)
				require.NoError(t, os.Mkdir(dir, 0o700))
				listener, err := net.Listen("unix", filepath.Join(dir, ".s.PGSQL.5432"))
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, listener.Close()) })
			}

			cmd, _, err := NewRootCommand().Find(strings.Fields(tt.command))
			require.NoError(t, err)

			err = requirePostgres(t.Context(), cmd, &config.Environment{RunPostgres: tt.runPostgres, StateDir: stateDir})
			if !tt.wantErr {
				assert.NoError(t, err)

				return
			}

			require.Error(t, err)
			assert.ErrorContains(t, err, "run komrad start first")
		})
	}
}
