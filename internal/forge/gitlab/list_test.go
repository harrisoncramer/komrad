package gitlab

import (
	"testing"
	"time"

	"komrad/internal/forge"
	"komrad/internal/git"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestList covers the branch listing: the project by its full path, the source branch filter
// with no state, the token in GitLab's own header, and every row mapped into the neutral record
// with the global id cut to its number and a fork's project named.
func TestList(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{
		"List": {body: listing(false, "end", openMergeRequest, mergedMergeRequest, forkMergeRequest, draftMergeRequest)},
	})

	listed, err := f.List(t.Context(), forge.ListParams{
		Repo:    gitlabOrg,
		Branch:  "settings-cleanup",
		Cursor:  "c2",
		PerPage: 50,
	})
	require.NoError(t, err)

	seen := fake.last(t)
	assert.Equal(t, "List", seen.Operation)
	assert.Equal(t, listQuery, seen.Query)
	assert.Equal(t, map[string]any{
		"path":     "acme/widgets",
		"first":    float64(50),
		"after":    "c2",
		"branches": []any{"settings-cleanup"},
	}, seen.Variables)
	assert.Equal(t, "secret", seen.Header.Get("PRIVATE-TOKEN"))
	assert.False(t, listed.HasMore)
	assert.Equal(t, "end", listed.Cursor)
	pullRequests := listed.PullRequests
	require.Len(t, pullRequests, 4)

	assert.Equal(t, forge.PullRequest{
		Forge:      forge.KindGitLab,
		ProviderID: "535736457",
		URL:        "https://gitlab.com/acme/widgets/-/merge_requests/256538",
		Repository: gitlabOrg,
		Head:       gitlabOrg,
		Number:     256538,
		Title:      "Strip undeclared keys from the settings",
		Author:     "someone",
		Body:       "Drops keys the schema does not declare.",
		HeadBranch: "settings-cleanup",
		HeadSHA:    "14a5732c18c7186fb0b34d469255b9609e4d11f2",
		BaseBranch: "master",
		BaseSHA:    "4aabd419a0202362695a5e4147df061fbf976c9f",
		State:      "OPEN",
		CreatedAt:  time.Date(2026, 9, 20, 8, 58, 11, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 9, 20, 17, 54, 15, 0, time.UTC),
	}, pullRequests[0])
	assert.Nil(t, pullRequests[0].MergeReadiness, "a listing does not ask for the merge readiness")

	stamp := time.Date(2026, 9, 20, 6, 24, 5, 0, time.UTC)
	assert.Equal(t, "535718519", pullRequests[1].ProviderID)
	assert.Equal(t, "MERGED", pullRequests[1].State)
	assert.Equal(t, &stamp, pullRequests[1].MergedAt)
	assert.Equal(t, &stamp, pullRequests[1].ClosedAt, "a merge closes the merge request too")
	assert.True(t, pullRequests[1].IsFinished())

	assert.Equal(t, "CLOSED", pullRequests[2].State)
	assert.Equal(t, "acme-forks/acme/widgets", pullRequests[2].Head.Path)
	assert.Empty(t, pullRequests[2].Author, "a user GitLab no longer has")
	assert.Empty(t, pullRequests[2].Body)
	assert.True(t, pullRequests[2].IsFinished())

	assert.True(t, pullRequests[3].Draft)
}

// TestListWithoutABranch covers the project's own listing: open ones only, no branch filter.
func TestListWithoutABranch(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{"List": {body: listing(false, "", openMergeRequest)}})

	_, err := f.List(t.Context(), forge.ListParams{Repo: komrad, PerPage: 20})
	require.NoError(t, err)

	seen := fake.last(t)
	assert.Equal(t, "opened", seen.Variables["state"])
	assert.NotContains(t, seen.Variables, "branches")
	assert.Nil(t, seen.Variables["after"])
}

// TestListReadsTheNextPage covers GitLab saying another page follows, and where it starts.
func TestListReadsTheNextPage(t *testing.T) {
	t.Parallel()

	f, _, _ := serve(t, map[string]answer{"List": {body: listing(true, "eyJjcmVhdGVk", openMergeRequest)}})

	listed, err := f.List(t.Context(), forge.ListParams{
		Repo:    komrad,
		Branch:  "agent/x",
		PerPage: 1,
	})
	require.NoError(t, err)
	assert.True(t, listed.HasMore)
	assert.Equal(t, "eyJjcmVhdGVk", listed.Cursor)
}

// TestListAddressesANestedGroup covers a project under a subgroup, which GraphQL takes as its
// full path.
func TestListAddressesANestedGroup(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{"List": {body: listing(false, "")}})

	_, err := f.List(t.Context(), forge.ListParams{Repo: git.RemoteURL{Host: "gitlab.com", Path: "group/sub/repo"}, Branch: "main"})
	require.NoError(t, err)
	assert.Equal(t, "group/sub/repo", fake.last(t).Variables["path"])
}

// TestListRefusesABarePath covers a repository path GitLab cannot address.
func TestListRefusesABarePath(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, nil)

	_, err := f.List(t.Context(), forge.ListParams{Repo: git.RemoteURL{Host: "gitlab.com", Path: "repo"}, Branch: "main"})
	require.ErrorIs(t, err, forge.ErrNotRepository)
	_, err = f.ReadBranches(t.Context(), git.RemoteURL{Host: "gitlab.com", Path: "repo"}, []string{"main"})
	require.ErrorIs(t, err, forge.ErrNotRepository)
	assert.Empty(t, fake.calls)
}
