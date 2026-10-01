package pullrequest

import (
	"testing"
	"time"

	"komrad/internal/dbtest"
	"komrad/internal/forge"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	workflowstore "komrad/internal/store/workflow"
	"komrad/lib/xptr"
	"komrad/lib/xslice"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	komrad  = "https://github.com/harrisoncramer/komrad"
	fixture = "https://github.com/harrisoncramer/komrad-forge-fixture"
)

type harness struct {
	*PullRequestStore
	pool      *store.Pool
	syncs     *PullRequestSyncStore
	agents    *agentstore.AgentStore
	workflows *workflowstore.WorkflowStore
}

func open(t *testing.T) *harness {
	t.Helper()

	pool, err := store.Open(t.Context(), dbtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	return &harness{
		PullRequestStore: NewPullRequestStore(pool.GetDB()),
		syncs:            NewPullRequestSyncStore(pool.GetDB()),
		pool:             pool,
		agents:           agentstore.NewAgentStore(pool.GetDB()),
		workflows:        workflowstore.NewWorkflowStore(pool.GetDB()),
	}
}

// createAgent puts a live agent on a branch of a repository in a workflow.
func (h *harness) createAgent(t *testing.T, workflowID, name, repositoryURL, branch string) string {
	t.Helper()
	ctx := t.Context()

	require.NoError(t, h.agents.Create(ctx, agentstore.Agent{
		Name:       name,
		Pane:       "%1",
		State:      agentstore.StateIdle,
		WorkflowID: workflowID,
	}))
	created, err := h.agents.GetInWorkflow(ctx, workflowID, name)
	require.NoError(t, err)
	require.NoError(t, h.agents.SetCheckout(ctx, created.ID, agentstore.SetCheckoutParams{RepositoryURL: &repositoryURL, Branch: &branch}))

	return created.ID
}

func (h *harness) createWorkflow(t *testing.T) string {
	t.Helper()

	created, err := h.workflows.Create(t.Context(), workflowstore.Config{
		Name:     "komrad",
		Worktree: workflowstore.WorktreeConfig{Enabled: true, BaseBranch: "main"},
	})
	require.NoError(t, err)

	return created.ID
}

func openPullRequest(providerID string, number int, branch string) UpsertParams {
	opened := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

	return UpsertParams{
		Host:              "github.com",
		Forge:             "github",
		ProviderID:        providerID,
		URL:               "https://github.com/harrisoncramer/komrad/pull/" + providerID,
		RepositoryURL:     komrad,
		HeadRepositoryURL: komrad,
		Number:            number,
		Title:             "one",
		HeadBranch:        branch,
		HeadSHA:           "abc",
		BaseBranch:        "main",
		BaseSHA:           "def",
		State:             "OPEN",
		OpenedAt:          opened,
		ChangedAt:         opened.Add(time.Hour),
	}
}

// TestUpsertSaysWhatItDid covers the guarded write: a new row is inserted, the same row again is
// left alone, and a row whose forge-owned columns moved is updated, with merged_at read back
// as finished.
func TestUpsertSaysWhatItDid(t *testing.T) {
	h := open(t)
	ctx := t.Context()

	first, err := h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
	require.NoError(t, err)
	assert.True(t, first.Inserted)
	assert.True(t, first.Changed)

	again, err := h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
	require.NoError(t, err)
	assert.Equal(t, first.ID, again.ID)
	assert.False(t, again.Inserted)
	assert.False(t, again.Changed, "the same row again is not written")
	assert.Nil(t, first.OldState, "a new row replaced nothing")
	assert.Nil(t, again.OldState, "an untouched row replaced nothing")

	merged := openPullRequest("PR_1", 1, "agent/x")
	mergedAt := merged.ChangedAt.Add(time.Hour)
	merged.MergedAt = &mergedAt
	merged.ClosedAt = &mergedAt
	merged.State = "MERGED"
	merged.ChangedAt = mergedAt
	moved, err := h.Upsert(ctx, merged)
	require.NoError(t, err)
	assert.Equal(t, first.ID, moved.ID)
	assert.False(t, moved.Inserted)
	assert.True(t, moved.Changed)
	require.NotNil(t, moved.OldState)
	assert.Equal(t, "OPEN", *moved.OldState, "a changed row says what it replaced")

	stored, err := h.GetByIdentity(ctx, "github.com", "PR_1")
	require.NoError(t, err)
	assert.True(t, stored.IsFinished())
	require.NotNil(t, stored.MergedAt)
	assert.True(t, stored.MergedAt.Equal(mergedAt))
}

// TestIdentityIsPerHost covers github.com and an enterprise host handing out one node id for
// two different pull requests, which the host keeps apart.
func TestIdentityIsPerHost(t *testing.T) {
	h := open(t)
	ctx := t.Context()

	public, err := h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
	require.NoError(t, err)

	enterprise := openPullRequest("PR_1", 7, "agent/y")
	enterprise.Host = "ghe.example.com"
	enterprise.URL = "https://ghe.example.com/team/repo/pull/7"
	enterprise.RepositoryURL = "https://ghe.example.com/team/repo"
	enterprise.HeadRepositoryURL = enterprise.RepositoryURL
	private, err := h.Upsert(ctx, enterprise)
	require.NoError(t, err)
	assert.True(t, private.Inserted)
	assert.NotEqual(t, public.ID, private.ID)

	_, err = h.GetByIdentity(ctx, "gitlab.com", "PR_1")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestObserveJoinsAgentsToTheirBranches covers attribution: an agent on the branch a pull
// request came from is recorded beside it, one on another branch or on the workflow's base
// branch is not, a fork's pull request is attributed to nobody, and a second pass inside the
// hour writes nothing.
func TestObserveJoinsAgentsToTheirBranches(t *testing.T) {
	h := open(t)
	ctx := t.Context()

	workflow := h.createWorkflow(t)
	onBranch := h.createAgent(t, workflow, "on-branch", komrad, "agent/x")
	other := h.createAgent(t, workflow, "elsewhere", komrad, "agent/y")
	onMain := h.createAgent(t, workflow, "on-main", komrad, "main")

	_, err := h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
	require.NoError(t, err)
	_, err = h.Upsert(ctx, openPullRequest("PR_2", 2, "main"))
	require.NoError(t, err)
	fork := openPullRequest("PR_3", 3, "agent/x")
	fork.HeadRepositoryURL = "https://github.com/someone/komrad"
	fork.URL = "https://github.com/harrisoncramer/komrad/pull/3"
	_, err = h.Upsert(ctx, fork)
	require.NoError(t, err)

	written, err := h.Observe(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), written)

	seen, err := h.ListObservations(ctx, []string{onBranch})
	require.NoError(t, err)
	require.Len(t, seen, 1)
	assert.Equal(t, seen[0].FirstSeenAt, seen[0].LastSeenAt)

	none, err := h.ListObservations(ctx, []string{other, onMain, "not-an-id"})
	require.NoError(t, err)
	assert.Empty(t, none)

	again, err := h.Observe(ctx)
	require.NoError(t, err)
	assert.Zero(t, again, "a pair seen within the hour is not written again")
}

// TestObserveLinksArchivedAgents covers that an archived agent is attributed like a live one.
func TestObserveLinksArchivedAgents(t *testing.T) {
	h := open(t)
	ctx := t.Context()

	workflow := h.createWorkflow(t)
	archived := h.createAgent(t, workflow, "archived", komrad, "agent/x")
	_, err := h.agents.Archive(ctx, []string{archived})
	require.NoError(t, err)
	_, err = h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
	require.NoError(t, err)

	written, err := h.Observe(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), written)
}

// TestLiveRepositoriesAndSyncTargets covers the two listings the pass starts from: the
// repositories live agents sit on, and the branches they sit on away from the base branch, each
// with its agent's head, archived agents left out of both.
func TestLiveRepositoriesAndSyncTargets(t *testing.T) {
	h := open(t)
	ctx := t.Context()

	workflow := h.createWorkflow(t)
	h.createAgent(t, workflow, "a", komrad, "agent/x")
	h.createAgent(t, workflow, "b", komrad, "agent/y")
	h.createAgent(t, workflow, "c", komrad, "agent/y")
	h.createAgent(t, workflow, "d", komrad, "main")
	h.createAgent(t, workflow, "e", fixture, "fixture/open")
	archived := h.createAgent(t, workflow, "f", "https://github.com/harrisoncramer/gone", "agent/z")
	_, err := h.agents.Archive(ctx, []string{archived})
	require.NoError(t, err)
	headed := h.createAgent(t, workflow, "g", komrad, "agent/y")
	repository, branch, head := komrad, "agent/y", "abc"
	require.NoError(t, h.agents.SetCheckout(ctx, headed, agentstore.SetCheckoutParams{
		RepositoryURL: &repository,
		Branch:        &branch,
		HeadSHA:       &head,
	}))

	repositories, err := h.ListLiveRepositories(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{komrad, fixture}, repositories)

	targets, err := h.syncs.ListTargets(ctx)
	require.NoError(t, err)
	assert.Equal(t, []Target{
		{RepositoryURL: komrad, Branch: "agent/x"},
		{RepositoryURL: komrad, Branch: "agent/y"},
		{
			RepositoryURL: komrad,
			Branch:        "agent/y",
			HeadSHA:       "abc",
		},
		{RepositoryURL: fixture, Branch: "fixture/open"},
	}, targets)
}

// finishedPullRequest is openPullRequest merged an hour after it changed.
func finishedPullRequest(providerID string, number int, branch string) UpsertParams {
	params := openPullRequest(providerID, number, branch)
	mergedAt := params.ChangedAt.Add(time.Hour)
	params.MergedAt = &mergedAt
	params.ClosedAt = &mergedAt
	params.State = "MERGED"
	params.ChangedAt = mergedAt

	return params
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
		RepositoryURL: komrad,
		HeadBranch:    "agent/x",
		Limit:         10,
	})
	require.NoError(t, err)
	assert.Equal(t, []int{3, 5, 1, 2}, numbers(onBranch), "unfinished newest first, then the finished")

	var walked []int
	var cursor *Cursor
	for {
		page, err := h.ListPage(ctx, ListPageParams{
			RepositoryURL: komrad,
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

// TestSetStandingOnAMissingRowChangesNothing pins that a row gone since it was read reports no change.
func TestSetStandingOnAMissingRowChangesNothing(t *testing.T) {
	h := open(t)

	change, err := h.SetStanding(t.Context(), SetStandingParams{
		ID:       uuid.NewString(),
		Standing: forge.Standing{Checks: forge.ChecksFailing},
		ReadAt:   time.Now(),
	})
	require.NoError(t, err)
	assert.Equal(t, StandingChange{}, *change)
}

// TestListRecipients pins which agents hear about a pull request.
func TestListRecipients(t *testing.T) {
	h := open(t)
	ctx := t.Context()
	workflow := h.createWorkflow(t)

	onBranch := h.createAgent(t, workflow, "on-branch", komrad, "agent/x")
	h.createAgent(t, workflow, "elsewhere", komrad, "agent/y")
	h.createAgent(t, workflow, "other-repo", "https://github.com/someone/else", "agent/x")
	archived := h.createAgent(t, workflow, "archived", komrad, "agent/x")
	_, err := h.agents.Archive(ctx, []string{archived})
	require.NoError(t, err)
	require.NoError(t, h.agents.Create(ctx, agentstore.Agent{
		Name:          "child",
		Pane:          "%2",
		State:         agentstore.StateIdle,
		WorkflowID:    workflow,
		ParentAgentID: &onBranch,
	}))
	child, err := h.agents.GetInWorkflow(ctx, workflow, "child")
	require.NoError(t, err)
	branch, repository := "agent/x", komrad
	require.NoError(t, h.agents.SetCheckout(ctx, child.ID, agentstore.SetCheckoutParams{RepositoryURL: &repository, Branch: &branch}))

	written, err := h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
	require.NoError(t, err)

	recipients, err := h.ListRecipients(ctx, written.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{onBranch}, recipients, "only the live parent on the branch is told")
}

// TestSetStandingRemembersWhatEveryReadLeaves covers the remembered standing reading back from the row as it was after each read.
func TestSetStandingRemembersWhatEveryReadLeaves(t *testing.T) {
	began := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	failing := forge.Standing{
		Decision:     forge.ReviewDecisionChangesRequested,
		Checks:       forge.ChecksFailing,
		Mergeability: forge.MergeabilityUnstable,
		AutoMerge:    forge.AutoMerge{On: true, Allowed: true},
	}
	passing := forge.Standing{
		Decision:     forge.ReviewDecisionApproved,
		Checks:       forge.ChecksPassing,
		Mergeability: forge.MergeabilityClean,
		AutoMerge:    forge.AutoMerge{Allowed: true},
	}
	type standingRead struct {
		standing forge.Standing
		head     string
	}
	type setStandingTest struct {
		name  string
		reads []standingRead
	}

	tests := []setStandingTest{
		{
			name: "a blank first read",
			reads: []standingRead{
				{head: "abc"},
				{standing: failing, head: "abc"},
			},
		},
		{
			name:  "a read with no head",
			reads: []standingRead{{standing: failing}},
		},
		{
			name: "blanks on the same head",
			reads: []standingRead{
				{standing: failing, head: "abc"},
				{head: "abc"},
				{head: "abc"},
			},
		},
		{
			name: "a blank on a new head",
			reads: []standingRead{
				{standing: failing, head: "abc"},
				{head: "def"},
			},
		},
		{
			name: "an answer after a blank",
			reads: []standingRead{
				{standing: failing, head: "abc"},
				{head: "abc"},
				{standing: passing, head: "def"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := open(t)
			ctx := t.Context()
			written, err := h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
			require.NoError(t, err)

			var want forge.RememberedStanding
			for i, read := range tt.reads {
				at := began.Add(time.Duration(i) * time.Minute)
				change, err := h.SetStanding(ctx, SetStandingParams{
					ID:       written.ID,
					Standing: read.standing,
					HeadSHA:  read.head,
					ReadAt:   at,
				})
				require.NoError(t, err)
				assert.Equal(t, want, inUTC(change.Before))

				want = want.RememberRead(read.standing, read.head, at)
				assert.Equal(t, want, inUTC(change.After))

				stored, err := h.GetByID(ctx, written.ID)
				require.NoError(t, err)
				assert.Equal(t, want, inUTC(stored.Standing))
			}
		})
	}
}

// TestSetStandingWritesNothingNew covers a read that leaves the remembered standing as it was writing nothing.
func TestSetStandingWritesNothingNew(t *testing.T) {
	h := open(t)
	ctx := t.Context()
	written, err := h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
	require.NoError(t, err)
	version := func() string {
		t.Helper()
		var xmin string
		require.NoError(t, h.pool.GetDB().QueryRow(ctx, "SELECT xmin::text FROM pull_requests WHERE id = $1", written.ID).Scan(&xmin))

		return xmin
	}
	params := SetStandingParams{
		ID:       written.ID,
		Standing: forge.Standing{Checks: forge.ChecksFailing},
		HeadSHA:  "abc",
		ReadAt:   time.Now(),
	}

	_, err = h.SetStanding(ctx, params)
	require.NoError(t, err)
	before := version()
	_, err = h.SetStanding(ctx, params)
	require.NoError(t, err)
	assert.Equal(t, before, version())
}

func inUTC(standing forge.RememberedStanding) forge.RememberedStanding {
	if standing.MergeabilityBlankSince != nil {
		standing.MergeabilityBlankSince = xptr.Ptr(standing.MergeabilityBlankSince.UTC())
	}

	return standing
}
