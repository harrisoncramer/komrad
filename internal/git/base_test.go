//go:build integration

package git_test

import (
	"path/filepath"
	"testing"

	"weave/internal/git"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationResolveBaseReadsTheCloneAsItStands covers the three answers: origin's copy when the clone
// holds one, the local branch otherwise, and a refusal naming the branch when it has neither.
func TestIntegrationResolveBaseReadsTheCloneAsItStands(t *testing.T) {
	t.Parallel()

	_, clone := newClonePair(t)
	run(t, clone, "symbolic-ref", "-d", "refs/remotes/origin/HEAD")

	ref, err := client.ResolveBase(t.Context(), clone, "main")
	require.NoError(t, err)
	assert.Equal(t, "origin/main", ref, "origin's copy is read without origin/HEAD")

	_, err = client.ResolveBase(t.Context(), clone, "trunk")
	require.ErrorIs(t, err, git.ErrBaseMissing)
	assert.Contains(t, err.Error(), "origin/trunk")

	_, err = client.ResolveBase(t.Context(), clone, "")
	require.ErrorIs(t, err, git.ErrBaseMissing, "a blank name is refused rather than guessed")

	local := newRepo(t)
	ref, err = client.ResolveBase(t.Context(), local, "main")
	require.NoError(t, err)
	assert.Equal(t, "refs/heads/main", ref, "a repository with no origin measures against its own branch")

	_, err = client.ResolveBase(t.Context(), local, "trunk")
	require.ErrorIs(t, err, git.ErrBaseMissing)

	unpushed := newRepo(t)
	run(t, unpushed, "remote", "add", "origin", filepath.Join(t.TempDir(), "remote.git"))
	ref, err = client.ResolveBase(t.Context(), unpushed, "main")
	require.NoError(t, err)
	assert.Equal(t, "refs/heads/main", ref, "an origin that holds no copy of the base leaves the local branch")

	_, err = client.ResolveBase(t.Context(), unpushed, "trunk")
	require.ErrorIs(t, err, git.ErrBaseMissing)
	assert.Contains(t, err.Error(), "trunk")
}

// TestIntegrationDefaultBaseReadsTheCloneOnce covers what a workflow left without a base is given: what
// origin/HEAD names, else the branch the checkout is on, else main.
func TestIntegrationDefaultBaseReadsTheCloneOnce(t *testing.T) {
	t.Parallel()

	_, clone := newClonePair(t)
	run(t, clone, "checkout", "--quiet", "-b", "wip")
	assert.Equal(t, "main", git.DefaultBase(t.Context(), clone), "origin/HEAD wins over the branch checked out")

	run(t, clone, "symbolic-ref", "-d", "refs/remotes/origin/HEAD")
	assert.Equal(t, "wip", git.DefaultBase(t.Context(), clone), "without origin/HEAD, the branch checked out")

	trunk := t.TempDir()
	run(t, trunk, "init", "--quiet", "-b", "trunk", ".")
	assert.Equal(t, "trunk", git.DefaultBase(t.Context(), trunk), "an unborn branch still has a name")

	assert.Equal(t, "main", git.DefaultBase(t.Context(), filepath.Join(t.TempDir(), "nowhere")), "a directory that is no repository")
	assert.Equal(t, "main", git.DefaultBase(t.Context(), ""), "no directory at all")
}
