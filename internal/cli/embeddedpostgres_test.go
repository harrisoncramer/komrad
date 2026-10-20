package cli

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"komrad/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRequireEmbeddedPostgres covers which commands refuse to run while the embedded Postgres is down.
func TestRequireEmbeddedPostgres(t *testing.T) {
	tests := []struct {
		name             string
		command          string
		embeddedPostgres bool
		externalOwner    bool
		running          bool
		wantErr          bool
	}{
		{
			name:             "engine ensure with the embedded Postgres down",
			command:          "engine ensure",
			embeddedPostgres: true,
			wantErr:          true,
		},
		{
			name:             "engine grant with the embedded Postgres down",
			command:          "engine grant",
			embeddedPostgres: true,
			wantErr:          true,
		},
		{
			name:             "engine revoke with the embedded Postgres down",
			command:          "engine revoke",
			embeddedPostgres: true,
			wantErr:          true,
		},
		{
			name:             "engine ensure with the embedded Postgres up",
			command:          "engine ensure",
			embeddedPostgres: true,
			running:          true,
		},
		{
			name:             "engine ensure with the embedded Postgres down and the owner on another Postgres",
			command:          "engine ensure",
			embeddedPostgres: true,
			externalOwner:    true,
		},
		{
			name:    "engine ensure against a Postgres the person runs",
			command: "engine ensure",
		},
		{
			name:             "migrate, which creates or starts the embedded Postgres",
			command:          "migrate",
			embeddedPostgres: true,
		},
		{
			name:             "serve, which starts the embedded Postgres and keeps it running",
			command:          "serve",
			embeddedPostgres: true,
		},
		{
			name:             "start, which brings the embedded Postgres up",
			command:          "start",
			embeddedPostgres: true,
		},
		{
			name:             "stop, which needs no database",
			command:          "stop",
			embeddedPostgres: true,
		},
		{
			name:             "doctor, which reports the embedded Postgres down itself",
			command:          "doctor",
			embeddedPostgres: true,
		},
		{
			name:             "panel, which starts the server the way start does",
			command:          "panel",
			embeddedPostgres: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateDir, err := os.MkdirTemp("/tmp", "komrad") //nolint:usetesting // a socket path under the test directory runs past the length unix allows
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, os.RemoveAll(stateDir)) })

			if tt.running {
				dir := config.GetEmbeddedPostgresDir(stateDir)
				require.NoError(t, os.Mkdir(dir, 0o700))
				listener, err := net.Listen("unix", filepath.Join(dir, ".s.PGSQL.5432"))
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, listener.Close()) })
			}

			cmd, _, err := NewRootCommand().Find(strings.Fields(tt.command))
			require.NoError(t, err)

			err = requireEmbeddedPostgres(t.Context(), cmd, &config.Environment{
				EmbeddedPostgres:      tt.embeddedPostgres,
				OwnerEmbeddedPostgres: tt.embeddedPostgres && !tt.externalOwner,
				StateDir:              stateDir,
			})
			if !tt.wantErr {
				assert.NoError(t, err)

				return
			}

			require.Error(t, err)
			assert.ErrorContains(t, err, "run komrad start first")
		})
	}
}
