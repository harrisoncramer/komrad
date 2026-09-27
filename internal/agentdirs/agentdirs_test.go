package agentdirs_test

import (
	"os"
	"path/filepath"
	"testing"

	"komrad/internal/agentdirs"
	agentstore "komrad/internal/store/agent"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/storetest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolve covers which directories count as inside a live agent's and where they resolve to.
func TestResolve(t *testing.T) {
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
	stores.SeedAgent(t, agentstore.Agent{
		Name:       "live",
		Cwd:        live,
		WorkflowID: workflowID,
	})
	gone := stores.SeedAgent(t, agentstore.Agent{
		Name:       "gone",
		Cwd:        archived,
		WorkflowID: workflowID,
	})
	_, err = stores.Agents.Archive(t.Context(), []string{gone.ID})
	require.NoError(t, err)

	tests := []struct {
		name    string
		dir     string
		wantErr error
		want    string
	}{
		{
			name:    "a directory that does not exist is refused",
			dir:     filepath.Join(root, "missing"),
			wantErr: agentdirs.ErrNoDirectory,
		},
		{
			name:    "a file inside a live agent's directory is refused",
			dir:     file,
			wantErr: agentdirs.ErrNoDirectory,
		},
		{
			name:    "a relative directory is refused",
			dir:     ".",
			wantErr: agentdirs.ErrNoDirectory,
		},
		{
			name:    "a directory no agent works in is refused",
			dir:     root,
			wantErr: agentdirs.ErrOutsideAgents,
		},
		{
			name:    "an archived agent's directory is refused",
			dir:     archived,
			wantErr: agentdirs.ErrOutsideAgents,
		},
		{
			name:    "a sibling sharing the agent's prefix is refused",
			dir:     sibling,
			wantErr: agentdirs.ErrOutsideAgents,
		},
		{
			name: "a live agent's directory resolves to itself",
			dir:  live,
			want: live,
		},
		{
			name: "a directory under a live agent's resolves to itself",
			dir:  nested,
			want: nested,
		},
		{
			name: "a link to a live agent's directory resolves to where it points",
			dir:  link,
			want: live,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := agentdirs.Resolve(t.Context(), stores.Agents, tt.dir)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
