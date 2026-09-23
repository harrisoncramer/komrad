//go:build integration

package e2e

import (
	"path/filepath"
	"testing"

	"weave/internal/weavetest"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationWorkflowCwdMustBeAPrimaryCheckout covers the cwd a worktree workflow is held to against
// real repositories.
func TestIntegrationWorkflowCwdMustBeAPrimaryCheckout(t *testing.T) {
	hermeticGit(t)

	instance := weavetest.Start(t, weavetest.Params{WorktreesDir: t.TempDir()})
	repo := initGitRepo(t)

	linked := filepath.Join(t.TempDir(), "tree")
	runGitIn(t, repo, "worktree", "add", "--quiet", "-b", "feature", linked)

	tests := []struct {
		name   string
		cwd    string
		wantOK bool
	}{
		{
			name: "a linked worktree is not the primary checkout",
			cwd:  linked,
		},
		{
			name:   "a repository git made is",
			cwd:    repo,
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := instance.Workflows.CreateWorkflow(t.Context(), connect.NewRequest(&workflowv1.CreateWorkflowRequest{
				Name:     "backend-" + tt.name,
				Cwd:      tt.cwd,
				Worktree: &workflowv1.WorktreeConfig{Enabled: true},
			}))

			if tt.wantOK {
				require.NoError(t, err)

				return
			}

			require.Error(t, err)
			assert.Contains(t, err.Error(), "primary checkout")
		})
	}
}
