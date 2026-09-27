//go:build integration

package git

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationReadChangesReadsTheCheckout covers the tracked and untracked halves against a real tree:
// a file edited in place, one staged, one deleted and one git ignores.
func TestIntegrationReadChangesReadsTheCheckout(t *testing.T) {
	root := repo(t)
	write(t, root, "kept.go", "package main\n\nvar changed = true\n")
	write(t, root, "staged.go", "package main\n")
	run(t, root, "add", "staged.go")
	require.NoError(t, os.Remove(filepath.Join(root, "proto/gen/api.pb.go")))
	write(t, root, "scratch.txt", "untracked\n")
	write(t, root, ".gitignore", "ignored.txt\n")
	write(t, root, "ignored.txt", "ignored\n")

	changes, err := client.ReadChanges(t.Context(), root)
	require.NoError(t, err)

	assert.Equal(t, []string{"kept.go", "proto/gen/api.pb.go", "staged.go"}, changes.Modified)
	assert.Equal(t, []string{".gitignore", "scratch.txt"}, changes.Untracked)
}

// TestIntegrationReadChangesOfACleanTree pins an empty listing reading as nothing rather than as one
// empty path.
func TestIntegrationReadChangesOfACleanTree(t *testing.T) {
	changes, err := client.ReadChanges(t.Context(), repo(t))
	require.NoError(t, err)

	assert.Empty(t, changes.Modified)
	assert.Empty(t, changes.Untracked)
}

// TestIntegrationCountAheadBehind covers a branch two commits past a base that has itself moved one.
func TestIntegrationCountAheadBehind(t *testing.T) {
	root := repo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "one.go", "package main\n")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-q", "-m", "one")
	write(t, root, "two.go", "package main\n")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-q", "-m", "two")
	run(t, root, "checkout", "-q", "main")
	write(t, root, "base.go", "package main\n")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-q", "-m", "base moved")
	run(t, root, "checkout", "-q", "feature")

	ahead, behind, err := client.CountAheadBehind(t.Context(), root, "main")
	require.NoError(t, err)

	assert.Equal(t, 2, ahead)
	assert.Equal(t, 1, behind)
}

// TestIntegrationIsBranchHeldElsewhere covers a branch whose tip only it reaches, and the same branch
// once main has merged it.
func TestIntegrationIsBranchHeldElsewhere(t *testing.T) {
	root := repo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "one.go", "package main\n")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-q", "-m", "one")

	held, err := client.IsBranchHeldElsewhere(t.Context(), root, "feature")
	require.NoError(t, err)
	assert.False(t, held)

	run(t, root, "checkout", "-q", "main")
	run(t, root, "merge", "-q", "--ff-only", "feature")

	held, err = client.IsBranchHeldElsewhere(t.Context(), root, "feature")
	require.NoError(t, err)
	assert.True(t, held)
}

// TestIntegrationListCommitsSince covers the branch's own commits coming back newest first and capped.
func TestIntegrationListCommitsSince(t *testing.T) {
	root := repo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	for _, name := range []string{"one", "two", "three"} {
		write(t, root, name+".go", "package main\n")
		run(t, root, "add", "-A")
		run(t, root, "commit", "-q", "-m", name)
	}

	commits, err := client.ListCommitsSince(t.Context(), root, "main", 2)
	require.NoError(t, err)

	require.Len(t, commits, 2)
	assert.Equal(t, "three", commits[0].Subject)
	assert.Equal(t, "two", commits[1].Subject)
	assert.Len(t, commits[0].SHA, 40)
	assert.False(t, commits[0].CommittedAt.IsZero())
}

// TestIntegrationPruneWorktreesDropsAGoneRegistration covers a directory removed by hand leaving a
// registration that prune takes away.
func TestIntegrationPruneWorktreesDropsAGoneRegistration(t *testing.T) {
	root := repo(t)
	tree := filepath.Join(t.TempDir(), "tree")
	run(t, root, "worktree", "add", "-q", "-b", "feature", tree)
	require.NoError(t, os.RemoveAll(tree))

	listed, err := client.ListWorktrees(t.Context(), root)
	require.NoError(t, err)
	require.Len(t, listed, 2)

	require.NoError(t, client.PruneWorktrees(t.Context(), root))

	listed, err = client.ListWorktrees(t.Context(), root)
	require.NoError(t, err)
	assert.Len(t, listed, 1)
}

// TestIntegrationResolveBaseReadsTheCloneAsItStands covers the three answers: origin's copy when the clone
// holds one, the local branch otherwise, and a refusal naming the branch when it has neither.
func TestIntegrationResolveBaseReadsTheCloneAsItStands(t *testing.T) {
	t.Parallel()

	clone := clonedRepo(t)
	run(t, clone, "symbolic-ref", "-d", "refs/remotes/origin/HEAD")

	ref, err := client.ResolveBase(t.Context(), clone, "main")
	require.NoError(t, err)
	assert.Equal(t, "origin/main", ref, "origin's copy is read without origin/HEAD")

	_, err = client.ResolveBase(t.Context(), clone, "trunk")
	require.ErrorIs(t, err, ErrBaseMissing)
	assert.Contains(t, err.Error(), "origin/trunk")

	_, err = client.ResolveBase(t.Context(), clone, "")
	require.ErrorIs(t, err, ErrBaseMissing, "a blank name is refused rather than guessed")

	local := repo(t)
	ref, err = client.ResolveBase(t.Context(), local, "main")
	require.NoError(t, err)
	assert.Equal(t, "refs/heads/main", ref, "a repository with no origin measures against its own branch")

	_, err = client.ResolveBase(t.Context(), local, "trunk")
	require.ErrorIs(t, err, ErrBaseMissing)

	unpushed := repo(t)
	run(t, unpushed, "remote", "add", "origin", filepath.Join(t.TempDir(), "remote.git"))
	ref, err = client.ResolveBase(t.Context(), unpushed, "main")
	require.NoError(t, err)
	assert.Equal(t, "refs/heads/main", ref, "an origin that holds no copy of the base leaves the local branch")

	_, err = client.ResolveBase(t.Context(), unpushed, "trunk")
	require.ErrorIs(t, err, ErrBaseMissing)
	assert.Contains(t, err.Error(), "trunk")
}

// TestIntegrationReadDiffStat covers the lines a working tree adds and removes since its base.
func TestIntegrationReadDiffStat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		exclude   []string
		change    func(t *testing.T, root string)
		wantErr   bool
		wantErrIs error
		want      DiffStat
	}{
		{
			name: "a base the clone does not hold is refused",
			change: func(t *testing.T, root string) {
				run(t, root, "branch", "-q", "-D", "main")
			},
			wantErr: true,
		},
		{
			name: "a base the branch shares no history with is refused as unrelated",
			change: func(t *testing.T, root string) {
				run(t, root, "checkout", "-q", "--orphan", "alone")
				write(t, root, "alone.go", "package alone\n")
				run(t, root, "add", "-A")
				run(t, root, "commit", "-q", "-m", "alone")
			},
			wantErr:   true,
			wantErrIs: ErrBaseUnrelated,
		},
		{
			name:    "an excluded path counts for nothing",
			exclude: []string{"proto/gen"},
			change: func(t *testing.T, root string) {
				write(t, root, "proto/gen/api.pb.go", "package gen\n\nvar a = 1\nvar b = 2\n")
			},
			want: DiffStat{},
		},
		{
			name: "a binary file counts for no lines",
			change: func(t *testing.T, root string) {
				write(t, root, "logo.png", "\x00\x01\x02\x03")
				run(t, root, "add", "logo.png")
				run(t, root, "commit", "-q", "-m", "logo")
			},
			want: DiffStat{},
		},
		{
			name: "a rename counts only the lines it changed",
			change: func(t *testing.T, root string) {
				write(t, root, "big.go", "package main\n\nvar a = 1\nvar b = 2\nvar c = 3\nvar d = 4\nvar e = 5\n")
				run(t, root, "add", "-A")
				run(t, root, "commit", "-q", "-m", "big")
				run(t, root, "branch", "-q", "-f", "main")
				run(t, root, "mv", "big.go", "moved.go")
				write(t, root, "moved.go", "package main\n\nvar a = 1\nvar b = 2\nvar c = 3\nvar d = 4\nvar e = 6\n")
				run(t, root, "add", "-A")
				run(t, root, "commit", "-q", "-m", "moved")
			},
			want: DiffStat{
				Added:   1,
				Removed: 1,
			},
		},
		{
			name: "committed, staged and unstaged lines all count",
			change: func(t *testing.T, root string) {
				write(t, root, "one.go", "package main\n\nvar one = 1\n")
				run(t, root, "add", "-A")
				run(t, root, "commit", "-q", "-m", "one")
				write(t, root, "staged.go", "package main\n")
				run(t, root, "add", "staged.go")
				write(t, root, "kept.go", "package kept\n")
			},
			want: DiffStat{
				Added:   5,
				Removed: 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := repo(t)
			run(t, root, "checkout", "-q", "-b", "feature")
			tt.change(t, root)

			stat, err := client.ReadDiffStat(t.Context(), ReadDiffStatParams{
				Dir:     root,
				Ref:     "main",
				Exclude: tt.exclude,
			})
			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrIs != nil {
					require.ErrorIs(t, err, tt.wantErrIs)
				}

				return
			}

			require.NoError(t, err)
			assert.Equal(t, &tt.want, stat)
		})
	}
}

// TestIntegrationCountAgainstRemote covers where a branch stands against its copy on origin.
func TestIntegrationCountAgainstRemote(t *testing.T) {
	t.Parallel()

	commit := func(t *testing.T, root, name string) {
		t.Helper()

		write(t, root, name, "package main\n")
		run(t, root, "add", "-A")
		run(t, root, "commit", "-q", "-m", name)
	}

	tests := []struct {
		name   string
		change func(t *testing.T, root string)
		want   RemoteStanding
	}{
		{
			name: "a branch origin has never held is not pushed",
			change: func(t *testing.T, root string) {
				commit(t, root, "one.go")
			},
			want: RemoteStanding{},
		},
		{
			name: "commits past the pushed copy are unpushed",
			change: func(t *testing.T, root string) {
				commit(t, root, "one.go")
				run(t, root, "push", "-q", "origin", "feature")
				commit(t, root, "two.go")
				commit(t, root, "three.go")
			},
			want: RemoteStanding{
				Pushed:   true,
				Unpushed: 2,
			},
		},
		{
			name: "commits only the pushed copy holds are unpulled",
			change: func(t *testing.T, root string) {
				commit(t, root, "one.go")
				run(t, root, "push", "-q", "origin", "feature")
				run(t, root, "reset", "-q", "--hard", "HEAD~1")
			},
			want: RemoteStanding{
				Pushed:   true,
				Unpulled: 1,
			},
		},
		{
			name: "a branch level with its pushed copy is even",
			change: func(t *testing.T, root string) {
				commit(t, root, "one.go")
				run(t, root, "push", "-q", "origin", "feature")
			},
			want: RemoteStanding{
				Pushed: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := clonedRepo(t)
			run(t, root, "config", "user.email", "test@example.com")
			run(t, root, "config", "user.name", "test")
			run(t, root, "checkout", "-q", "-b", "feature")
			tt.change(t, root)

			standing, err := client.CountAgainstRemote(t.Context(), root, "feature")
			require.NoError(t, err)

			assert.Equal(t, &tt.want, standing)
		})
	}
}

// TestIntegrationDoesBaseHoldRemote covers a base that took a branch's pushed work as a squash
// being told apart from one that never took it.
func TestIntegrationDoesBaseHoldRemote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(t *testing.T, root string)
		want   bool
	}{
		{
			name: "a base that shares no history with the pushed work does not hold it",
			change: func(t *testing.T, root string) {
				run(t, root, "checkout", "-q", "--orphan", "unrelated")
				run(t, root, "rm", "-rqf", ".")
				write(t, root, "other.go", "package other\n")
				run(t, root, "add", "-A")
				run(t, root, "commit", "-q", "-m", "other")
				run(t, root, "branch", "-f", "main", "unrelated")
			},
			want: false,
		},
		{
			name: "a base without the pushed work does not hold it",
			change: func(t *testing.T, root string) {
				run(t, root, "checkout", "-q", "main")
				write(t, root, "other.go", "package other\n")
				run(t, root, "add", "-A")
				run(t, root, "commit", "-q", "-m", "other")
			},
			want: false,
		},
		{
			name: "a base that squashed the pushed work holds it",
			change: func(t *testing.T, root string) {
				run(t, root, "checkout", "-q", "main")
				write(t, root, "one.go", "package main\n")
				write(t, root, "two.go", "package main\n")
				run(t, root, "add", "-A")
				run(t, root, "commit", "-q", "-m", "squash")
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := clonedRepo(t)
			run(t, root, "config", "user.email", "test@example.com")
			run(t, root, "config", "user.name", "test")
			run(t, root, "checkout", "-q", "-b", "feature")
			for _, name := range []string{"one.go", "two.go"} {
				write(t, root, name, "package main\n")
				run(t, root, "add", "-A")
				run(t, root, "commit", "-q", "-m", name)
			}
			run(t, root, "push", "-q", "origin", "feature")
			tt.change(t, root)

			held, err := client.DoesBaseHoldRemote(t.Context(), DoesBaseHoldRemoteParams{
				Dir:    root,
				Branch: "feature",
				Base:   "main",
			})
			require.NoError(t, err)

			assert.Equal(t, tt.want, held)
		})
	}
}
