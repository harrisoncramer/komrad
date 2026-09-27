package shells_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"komrad/internal/agentdirs"
	"komrad/internal/pty"
	"komrad/internal/shells"
	agentstore "komrad/internal/store/agent"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/storetest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOpen covers a shell refused outside the agents' directories and started where its
// directory resolves to.
func TestOpen(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	live := filepath.Join(root, "tree")
	link := filepath.Join(root, "link")
	require.NoError(t, os.MkdirAll(live, 0o755))
	require.NoError(t, os.Symlink(live, link))

	stores := storetest.Open(t)
	workflowID := stores.SeedWorkflow(t, workflowstore.Config{Name: "flow"}).ID
	stores.SeedAgent(t, agentstore.Agent{
		Name:       "live",
		Cwd:        live,
		WorkflowID: workflowID,
	})

	service := shells.NewShellService(shells.NewShellServiceParams{Agents: stores.Agents, Shell: "sh"})

	tests := []struct {
		name    string
		dir     string
		wantErr error
		wantPwd string
	}{
		{
			name:    "a directory no agent works in is refused",
			dir:     root,
			wantErr: agentdirs.ErrOutsideAgents,
		},
		{
			name:    "a link to a live agent's directory opens where it points",
			dir:     link,
			wantPwd: live,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			terminal, err := service.Open(t.Context(), shells.OpenParams{
				Dir:  tt.dir,
				Size: pty.Size{Rows: 24, Cols: 80},
			})
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)

			_, err = terminal.Write([]byte("pwd -P; exit\n"))
			require.NoError(t, err)

			var output bytes.Buffer
			buffer := make([]byte, 1024)
			for {
				n, err := terminal.Read(buffer)
				output.Write(buffer[:n])
				if err != nil {
					break
				}
			}

			code, err := terminal.Wait()
			require.NoError(t, err)
			require.NoError(t, terminal.Close())

			assert.Equal(t, 0, code)
			assert.Contains(t, output.String(), tt.wantPwd+"\r\n")
		})
	}
}
