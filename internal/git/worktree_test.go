//go:build integration

package git

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationUnlockWorktreeLetsPruneDropAGoneRegistration covers a locked registration
// surviving a prune until its lock is lifted.
func TestIntegrationUnlockWorktreeLetsPruneDropAGoneRegistration(t *testing.T) {
	root := repo(t)
	tree := filepath.Join(t.TempDir(), "tree")
	run(t, root, "worktree", "add", "-q", "--lock", "-b", "feature", tree)

	require.NoError(t, client.UnlockWorktree(t.Context(), root, tree))
	require.NoError(t, os.RemoveAll(tree))
	require.NoError(t, client.PruneWorktrees(t.Context(), root))

	listed, err := client.ListWorktrees(t.Context(), root)
	require.NoError(t, err)
	assert.Len(t, listed, 1)
}

// TestIntegrationParseWorktreesReadsTheListing pins the porcelain listing being read as blocks.
func TestIntegrationParseWorktreesReadsTheListing(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want []Worktree
	}{
		{
			name: "nothing listed",
			out:  "",
			want: nil,
		},
		{
			name: "a branch line before any checkout is ignored",
			out:  "branch refs/heads/orphan\n",
			want: nil,
		},
		{
			name: "a detached checkout names no branch",
			out:  "worktree /repo\nHEAD abc\ndetached\n",
			want: []Worktree{
				{
					Dir:    "/repo",
					Branch: "",
				},
			},
		},
		{
			name: "a bare repository is two lines and holds no branch",
			out:  "worktree /repo\nbare\n",
			want: []Worktree{
				{
					Dir:    "/repo",
					Branch: "",
				},
			},
		},
		{
			name: "a registration whose directory has gone still names its branch",
			out:  "worktree /trees/gone\nHEAD abc\nbranch refs/heads/feature\nprunable gitdir file points to non-existent location\n",
			want: []Worktree{
				{
					Dir:    "/trees/gone",
					Branch: "feature",
				},
			},
		},
		{
			name: "the main checkout is listed first",
			out:  "worktree /repo\nHEAD abc\nbranch refs/heads/main\n\nworktree /trees/work\nHEAD def\nbranch refs/heads/feature\n",
			want: []Worktree{
				{
					Dir:    "/repo",
					Branch: "main",
				},
				{
					Dir:    "/trees/work",
					Branch: "feature",
				},
			},
		},
		{
			name: "a branch holding a slash keeps it",
			out:  "worktree /trees/work\nbranch refs/heads/agent/01a0-beef\n",
			want: []Worktree{
				{
					Dir:    "/trees/work",
					Branch: "agent/01a0-beef",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, parseWorktrees(tt.out))
		})
	}
}
