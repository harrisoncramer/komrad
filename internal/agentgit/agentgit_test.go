package agentgit_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"komrad/internal/agentdirs"
	"komrad/internal/agentgit"
	"komrad/internal/git"
	"komrad/internal/gittest"
	agentstore "komrad/internal/store/agent"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/storetest"

	"github.com/stretchr/testify/require"
)

// TestEveryCallKeepsToTheAgentsDirectories covers each call refused outside a live agent's
// directory and reaching git inside one.
func TestEveryCallKeepsToTheAgentsDirectories(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	live := filepath.Join(root, "tree")
	outside := filepath.Join(root, "elsewhere")
	for _, dir := range []string{live, outside} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}

	stores := storetest.Open(t)
	workflowID := stores.SeedWorkflow(t, workflowstore.Config{Name: "flow"}).ID
	stores.SeedAgent(t, agentstore.Agent{
		Name:       "live",
		Cwd:        live,
		WorkflowID: workflowID,
	})

	service := agentgit.NewGitService(agentgit.NewGitServiceParams{Agents: stores.Agents, Git: gittest.NoRepo{}})

	tests := []struct {
		name string
		call func(ctx context.Context, dir string) error
	}{
		{
			name: "GetRoot",
			call: func(ctx context.Context, dir string) error {
				_, err := service.GetRoot(ctx, dir)

				return err
			},
		},
		{
			name: "ReadFiles",
			call: func(ctx context.Context, dir string) error {
				_, err := service.ReadFiles(ctx, dir, []git.FileRevision{{SHA: "a", Path: "b"}})

				return err
			},
		},
		{
			name: "CountChangedFiles",
			call: func(ctx context.Context, dir string) error {
				_, err := service.CountChangedFiles(ctx, git.CountChangedFilesParams{Dir: dir})

				return err
			},
		},
		{
			name: "Diff",
			call: func(ctx context.Context, dir string) error {
				_, err := service.Diff(ctx, git.DiffParams{Dir: dir})

				return err
			},
		},
		{
			name: "MapRanges",
			call: func(ctx context.Context, dir string) error {
				_, err := service.MapRanges(ctx, git.MapRangesParams{Dir: dir})

				return err
			},
		},
		{
			name: "Replay",
			call: func(ctx context.Context, dir string) error {
				_, err := service.Replay(ctx, git.ReplayParams{Dir: dir})

				return err
			},
		},
		{
			name: "IsAncestor",
			call: func(ctx context.Context, dir string) error {
				_, err := service.IsAncestor(ctx, git.IsAncestorParams{Dir: dir, MaybeAncestor: "a", Of: "b"})

				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorIs(t, tt.call(t.Context(), outside), agentdirs.ErrOutsideAgents)
			require.ErrorIs(t, tt.call(t.Context(), live), gittest.ErrNoGit)
		})
	}
}
