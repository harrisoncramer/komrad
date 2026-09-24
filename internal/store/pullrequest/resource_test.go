package pullrequest

import (
	"testing"
	"time"

	"weave/lib/xslice"

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
	onBranch := h.createAgent(t, flow, "on-branch", weave, "agent/x")

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

// TestListPageOrdersCurrentFirst pins the page's order and its cursor: the unfinished first and
// each group newest opened first, the branch narrowing it, and a cursor walking on from the
// last row of a page without skipping or repeating one.
func TestListPageOrdersCurrentFirst(t *testing.T) {
	ctx := t.Context()
	h := open(t)

	opened := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	at := func(number int, branch string, hours int, finished bool) UpsertParams {
		params := openPullRequest("PR_"+string(rune('0'+number)), number, branch)
		params.OpenedAt = opened.Add(time.Duration(hours) * time.Hour)
		if finished {
			mergedAt := params.OpenedAt.Add(time.Hour)
			params.MergedAt = &mergedAt
			params.ClosedAt = &mergedAt
		}

		return params
	}
	for _, params := range []UpsertParams{
		at(1, "agent/x", 0, false),
		at(2, "agent/x", 1, true),
		at(3, "agent/x", 2, false),
		at(4, "agent/y", 3, true),
		at(5, "agent/x", 1, false),
	} {
		_, err := h.Upsert(ctx, params)
		require.NoError(t, err)
	}

	numbers := func(pullRequests []PullRequest) []int {
		return xslice.Map(pullRequests, func(p PullRequest) int { return p.Number })
	}

	onBranch, err := h.ListPage(ctx, ListPageParams{
		RepositoryURL: weave,
		HeadBranch:    "agent/x",
		Limit:         10,
	})
	require.NoError(t, err)
	assert.Equal(t, []int{3, 5, 1, 2}, numbers(onBranch), "unfinished newest first, then the finished")

	var walked []int
	var cursor *Cursor
	for {
		page, err := h.ListPage(ctx, ListPageParams{
			RepositoryURL: weave,
			Limit:         2,
			Cursor:        cursor,
		})
		require.NoError(t, err)
		walked = append(walked, numbers(page)...)
		if len(page) < 2 {
			break
		}
		last := page[len(page)-1]
		cursor = &Cursor{
			Finished: last.IsFinished(),
			OpenedAt: last.OpenedAt,
			ID:       last.ID,
		}
	}
	assert.Equal(t, []int{3, 5, 1, 4, 2}, walked, "every page from the whole repository, in one order")

	elsewhere, err := h.ListPage(ctx, ListPageParams{RepositoryURL: fixture, Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, elsewhere)
}
