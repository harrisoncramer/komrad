package shells_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"komrad/internal/pty"
	"komrad/internal/shells"
	agentstore "komrad/internal/store/agent"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/storetest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOpen covers which directories a shell opens in and where it starts.
func TestOpen(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	live := filepath.Join(root, "tree")
	archived := filepath.Join(root, "archived")
	sibling := filepath.Join(root, "tree-2")
	nested := filepath.Join(live, "nested")
	link := filepath.Join(root, "link")
	for _, dir := range []string{live, archived, sibling, nested} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	require.NoError(t, os.Symlink(live, link))
	file := filepath.Join(live, "notes.txt")
	require.NoError(t, os.WriteFile(file, nil, 0o600))

	stores := storetest.Open(t)
	workflowID := stores.SeedWorkflow(t, workflowstore.Config{Name: "flow"}).ID
	stores.SeedAgent(t, agentstore.Agent{Name: "live", Cwd: live, WorkflowID: workflowID})
	gone := stores.SeedAgent(t, agentstore.Agent{Name: "gone", Cwd: archived, WorkflowID: workflowID})
	_, err = stores.Agents.Archive(t.Context(), []string{gone.ID})
	require.NoError(t, err)

	service := shells.NewShellService(shells.NewShellServiceParams{Agents: stores.Agents, Shell: "sh"})

	tests := []struct {
		name    string
		dir     string
		wantErr error
		wantPwd string
	}{
		{
			name:    "a directory that does not exist is refused",
			dir:     filepath.Join(root, "missing"),
			wantErr: shells.ErrNoDirectory,
		},
		{
			name:    "a file inside a live agent's directory is refused",
			dir:     file,
			wantErr: shells.ErrNoDirectory,
		},
		{
			name:    "a relative directory is refused",
			dir:     ".",
			wantErr: shells.ErrNoDirectory,
		},
		{
			name:    "a directory no agent works in is refused",
			dir:     root,
			wantErr: shells.ErrOutsideAgents,
		},
		{
			name:    "an archived agent's directory is refused",
			dir:     archived,
			wantErr: shells.ErrOutsideAgents,
		},
		{
			name:    "a sibling sharing the agent's prefix is refused",
			dir:     sibling,
			wantErr: shells.ErrOutsideAgents,
		},
		{
			name:    "a live agent's directory opens",
			dir:     live,
			wantPwd: live,
		},
		{
			name:    "a directory under a live agent's opens",
			dir:     nested,
			wantPwd: nested,
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
