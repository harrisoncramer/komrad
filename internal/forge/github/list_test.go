package github

import (
	"testing"
	"time"

	"komrad/internal/forge"
	"komrad/internal/git"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestList covers the branch listing: the variables GitHub wants, the token on the request,
// and every row mapped into the neutral record, a fork and a deleted fork included.
func TestList(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{
		"List": {body: listing(false, "end", openPull, mergedPull, forkPull, deletedForkPull, draftPull)},
	})

	listed, err := f.List(t.Context(), forge.ListParams{
		Repo:    fixture,
		Branch:  "fixture/open",
		Cursor:  "c2",
		PerPage: 50,
	})
	require.NoError(t, err)

	seen := fake.last(t)
	assert.Equal(t, "/graphql", seen.Path)
	assert.Equal(t, "List", seen.Operation)
	assert.Equal(t, listQuery, seen.Query)
	assert.Equal(t, map[string]any{
		"owner":  "harrisoncramer",
		"name":   "komrad-forge-fixture",
		"first":  float64(50),
		"after":  "c2",
		"branch": "fixture/open",
		"states": []any{"OPEN", "CLOSED", "MERGED"},
	}, seen.Variables)
	assert.Equal(t, "Bearer secret", seen.Header.Get("Authorization"))
	assert.False(t, listed.HasMore)
	assert.Equal(t, "end", listed.Cursor)
	pullRequests := listed.PullRequests
	require.Len(t, pullRequests, 5)

	repository := git.RemoteURL{
		Raw:    "https://github.com/harrisoncramer/komrad-forge-fixture",
		Scheme: "https",
		Host:   "github.com",
		Path:   "harrisoncramer/komrad-forge-fixture",
	}
	assert.Equal(t, forge.PullRequest{
		Forge:      forge.KindGitHub,
		ProviderID: "PR_kwDOUgkrbM8AAAABEIdmHw",
		URL:        "https://github.com/harrisoncramer/komrad-forge-fixture/pull/1",
		Repository: repository,
		Head:       repository,
		Number:     1,
		Title:      "fixture: open",
		Author:     "harrisoncramer",
		Body:       "Opens the fixture.",
		HeadBranch: "fixture/open",
		HeadSHA:    "acc6d4fd6caf3511c998a673406e6fed2df7a28b",
		BaseBranch: "main",
		BaseSHA:    "8b32d7985e62febbf494e815545c22b446276b5d",
		State:      "OPEN",
		CreatedAt:  time.Date(2026, 9, 18, 19, 14, 17, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 9, 18, 19, 14, 17, 0, time.UTC),
	}, pullRequests[0])
	assert.False(t, pullRequests[0].IsFinished())
	assert.Nil(t, pullRequests[0].MergeReadiness, "a listing does not ask for the merge readiness")

	stamp := time.Date(2026, 9, 20, 18, 0, 7, 0, time.UTC)
	assert.Equal(t, "PR_kwDOTgPfPc8AAAABEU5L_Q", pullRequests[1].ProviderID)
	assert.Equal(t, "MERGED", pullRequests[1].State)
	assert.Equal(t, &stamp, pullRequests[1].MergedAt)
	assert.Equal(t, &stamp, pullRequests[1].ClosedAt)
	assert.True(t, pullRequests[1].IsFinished())

	assert.Equal(t, "someone/widgets", pullRequests[2].Head.Path)

	assert.True(t, pullRequests[3].Head.IsZero())
	assert.Empty(t, pullRequests[3].Author, "an account GitHub no longer has")
	assert.Empty(t, pullRequests[3].Body)
	assert.True(t, pullRequests[3].IsFinished())

	assert.True(t, pullRequests[4].Draft)
}

// TestListWithoutABranch covers the repository's own listing: open ones only, no branch.
func TestListWithoutABranch(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{"List": {body: listing(false, "", openPull)}})

	listed, err := f.List(t.Context(), forge.ListParams{Repo: fixture})
	require.NoError(t, err)
	require.Len(t, listed.PullRequests, 1)
	seen := fake.last(t)
	assert.Equal(t, []any{"OPEN"}, seen.Variables["states"])
	assert.Nil(t, seen.Variables["after"], "the first page names no cursor")
	assert.NotContains(t, seen.Variables, "branch")
	assert.InDelta(t, 1, seen.Variables["first"], 0)
}

// TestListReadsTheNextPage covers GitHub saying another page follows, and where it starts.
func TestListReadsTheNextPage(t *testing.T) {
	t.Parallel()

	f, _, _ := serve(t, map[string]answer{"List": {body: listing(true, "Y3Vyc29yOnYyOpK0", openPull)}})

	listed, err := f.List(t.Context(), forge.ListParams{Repo: fixture})
	require.NoError(t, err)
	assert.True(t, listed.HasMore)
	assert.Equal(t, "Y3Vyc29yOnYyOpK0", listed.Cursor)
}

// TestListRefusesANestedPath covers a repository path GitHub cannot address.
func TestListRefusesANestedPath(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, nil)

	_, err := f.List(t.Context(), forge.ListParams{Repo: git.RemoteURL{Host: "github.com", Path: "group/sub/repo"}, Branch: "main"})
	require.ErrorIs(t, err, forge.ErrNotRepository)
	_, err = f.ReadBranches(t.Context(), git.RemoteURL{Host: "github.com", Path: "group/sub/repo"}, []string{"main"})
	require.ErrorIs(t, err, forge.ErrNotRepository)
	assert.Empty(t, fake.calls)
}
