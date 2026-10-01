package pullrequest

import (
	"testing"

	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListWatchedIsTheLinkedSet pins the watched set to the links: a pull request is carried
// once an agent has been seen with it and stays carried after that agent is archived, and one
// nobody was seen with is a tombstone.
func TestListWatchedIsTheLinkedSet(t *testing.T) {
	ctx := t.Context()
	h := open(t)
	flow := h.createWorkflow(t)
	onBranch := h.createAgent(t, flow, "on-branch", komrad, "agent/x")

	held, err := h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
	require.NoError(t, err)
	orphan, err := h.Upsert(ctx, openPullRequest("PR_2", 2, "agent/nobody"))
	require.NoError(t, err)

	ids := func(pullRequests []PullRequest) []string {
		return xslice.Map(pullRequests, func(p PullRequest) string { return p.ID })
	}

	unlinked, err := h.ListWatched(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, unlinked, "a pull request on an agent's branch is not carried before it is observed")

	_, err = h.Observe(ctx)
	require.NoError(t, err)

	listed, err := h.ListWatched(ctx, []string{held.ID, orphan.ID})
	require.NoError(t, err)
	assert.Equal(t, []string{held.ID}, ids(listed), "a pull request nobody was seen with is a tombstone")

	snapshot, err := h.ListWatched(ctx, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{held.ID}, ids(snapshot))

	_, err = h.agents.Archive(ctx, []string{onBranch})
	require.NoError(t, err)

	listed, err = h.ListWatched(ctx, []string{held.ID})
	require.NoError(t, err)
	assert.Equal(t, []string{held.ID}, ids(listed), "an archived agent's pull request stays in the watched set")

	none, err := h.ListWatched(ctx, []string{})
	require.NoError(t, err)
	assert.Empty(t, none)
}

// TestListWatchedRepositories pins that a repository is carried once it has been read, and one
// never read is a tombstone.
func TestListWatchedRepositories(t *testing.T) {
	ctx := t.Context()
	h := open(t)

	require.NoError(t, h.syncs.EnsureSync(ctx, fixture))
	require.NoError(t, h.syncs.EnsureSync(ctx, komrad))

	urls := func(syncs []Sync) []string {
		return xslice.Map(syncs, func(sync Sync) string { return sync.RepositoryURL })
	}

	snapshot, err := h.syncs.ListWatched(ctx, nil)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{fixture, komrad}, urls(snapshot))

	listedByKey, err := h.syncs.ListWatched(ctx, []string{fixture, "https://github.com/nobody/nothing"})
	require.NoError(t, err)
	assert.Equal(t, []string{fixture}, urls(listedByKey), "a repository with no sync is a tombstone")
}
