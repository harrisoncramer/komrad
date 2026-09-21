//go:build integration

package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"weave/internal/weavetest"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateWorkflowFillsTheBaseFromTheClone covers a blank base being read off the clone once:
// what origin/HEAD names when the clone has one, and the branch the checkout is on when it has
// no origin, with the response carrying the name stored.
func TestCreateWorkflowFillsTheBaseFromTheClone(t *testing.T) {
	hermeticGit(t)

	tests := []struct {
		name string
		cwd  func(t *testing.T) string
		want string
	}{
		{
			name: "a clone stores what its origin/HEAD names",
			cwd: func(t *testing.T) string {
				clone := initGitRepo(t)
				runGitIn(t, clone, "checkout", "--quiet", "-b", "wip")

				return clone
			},
			want: "main",
		},
		{
			name: "a repository with no origin stores the branch it is on",
			cwd: func(t *testing.T) string {
				dir := t.TempDir()
				runGitIn(t, dir, "init", "--quiet", "-b", "trunk")
				runGitIn(t, dir, "config", "user.email", "test@weave.test")
				runGitIn(t, dir, "config", "user.name", "weave test")
				require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644))
				runGitIn(t, dir, "add", "README.md")
				runGitIn(t, dir, "commit", "--quiet", "-m", "initial")

				return dir
			},
			want: "trunk",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instance := weavetest.Start(t, weavetest.Params{WorktreesDir: t.TempDir()})

			created, err := instance.Workflows.CreateWorkflow(t.Context(), connect.NewRequest(&workflowv1.CreateWorkflowRequest{
				Name:     "backend",
				Cwd:      tt.cwd(t),
				Worktree: &workflowv1.WorktreeConfig{Enabled: true},
			}))
			require.NoError(t, err)
			assert.Equal(t, tt.want, created.Msg.GetWorkflow().GetWorktree().GetBaseBranch())

			read, err := instance.Workflows.GetWorkflow(t.Context(), connect.NewRequest(&workflowv1.GetWorkflowRequest{Id: created.Msg.GetWorkflow().GetId()}))
			require.NoError(t, err)
			assert.Equal(t, tt.want, read.Msg.GetWorkflow().GetWorktree().GetBaseBranch())
		})
	}
}
