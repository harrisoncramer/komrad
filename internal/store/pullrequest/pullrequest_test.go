package pullrequest

import (
	"testing"
	"time"

	"komrade/internal/dbtest"
	"komrade/internal/forge"
	"komrade/internal/store"
	agentstore "komrade/internal/store/agent"
	workflowstore "komrade/internal/store/workflow"
	"komrade/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	komrade = "https://github.com/harrisoncramer/komrade"
	fixture = "https://github.com/harrisoncramer/weave-forge-fixture"
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
		Name:     "komrade",
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
		URL:               "https://github.com/harrisoncramer/komrade/pull/" + providerID,
		RepositoryURL:     komrade,
		HeadRepositoryURL: komrade,
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
	onBranch := h.createAgent(t, workflow, "on-branch", komrade, "agent/x")
	other := h.createAgent(t, workflow, "elsewhere", komrade, "agent/y")
	onMain := h.createAgent(t, workflow, "on-main", komrade, "main")

	_, err := h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
	require.NoError(t, err)
	_, err = h.Upsert(ctx, openPullRequest("PR_2", 2, "main"))
	require.NoError(t, err)
	fork := openPullRequest("PR_3", 3, "agent/x")
	fork.HeadRepositoryURL = "https://github.com/someone/komrade"
	fork.URL = "https://github.com/harrisoncramer/komrade/pull/3"
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
	archived := h.createAgent(t, workflow, "archived", komrade, "agent/x")
	_, err := h.agents.Archive(ctx, []string{archived})
	require.NoError(t, err)
	_, err = h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
	require.NoError(t, err)

	written, err := h.Observe(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), written)
}

// TestLiveRepositoriesAndUnscannedBranches covers the two listings the pass starts from: the
// repositories live agents sit on, and the branches of one whose scan has not completed.
func TestLiveRepositoriesAndUnscannedBranches(t *testing.T) {
	h := open(t)
	ctx := t.Context()

	workflow := h.createWorkflow(t)
	h.createAgent(t, workflow, "a", komrade, "agent/x")
	h.createAgent(t, workflow, "b", komrade, "agent/y")
	h.createAgent(t, workflow, "c", fixture, "fixture/open")
	archived := h.createAgent(t, workflow, "d", "https://github.com/harrisoncramer/gone", "agent/z")
	_, err := h.agents.Archive(ctx, []string{archived})
	require.NoError(t, err)

	repositories, err := h.ListLiveRepositories(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{komrade, fixture}, repositories)

	branches, err := h.syncs.ListUnscannedBranches(ctx, komrade)
	require.NoError(t, err)
	assert.Equal(t, []string{"agent/x", "agent/y"}, branches)

	require.NoError(t, h.syncs.SetBranchScanCursor(ctx, komrade, "agent/x", "c2"))
	scan, err := h.syncs.GetBranchScan(ctx, komrade, "agent/x")
	require.NoError(t, err)
	assert.Equal(t, "c2", scan.NextCursor)
	assert.Nil(t, scan.CompletedAt)

	branches, err = h.syncs.ListUnscannedBranches(ctx, komrade)
	require.NoError(t, err)
	assert.Equal(t, []string{"agent/x", "agent/y"}, branches, "a scan in progress is still unscanned")

	require.NoError(t, h.syncs.CompleteBranchScan(ctx, komrade, "agent/x", "c3"))
	branches, err = h.syncs.ListUnscannedBranches(ctx, komrade)
	require.NoError(t, err)
	assert.Equal(t, []string{"agent/y"}, branches)

	_, err = h.syncs.GetBranchScan(ctx, komrade, "agent/q")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestSyncRowRoundTrip covers the cursor row through a first sweep and the passes after it:
// requests are counted as they are made, a sweep holds its cursor until it completes,
// completion promotes the newest stamp and never regresses the mark, the search moves the mark
// forward only, the listing clock moves only when asked, and a reset hands the repository back
// to a first sweep.
func TestSyncRowRoundTrip(t *testing.T) {
	h := open(t)
	ctx := t.Context()

	_, err := h.syncs.GetSync(ctx, komrade)
	require.ErrorIs(t, err, store.ErrNotFound)

	require.NoError(t, h.syncs.EnsureSync(ctx, komrade))
	require.NoError(t, h.syncs.EnsureSync(ctx, komrade))
	sync, err := h.syncs.GetSync(ctx, komrade)
	require.NoError(t, err)
	assert.Nil(t, sync.UpdatedThrough)
	assert.False(t, sync.IsSweeping())
	assert.False(t, sync.IsSwept())
	assert.Zero(t, sync.Calls)

	require.NoError(t, h.syncs.RecordSyncRequest(ctx, komrade))
	require.NoError(t, h.syncs.RecordSyncRequest(ctx, komrade))
	sync, err = h.syncs.GetSync(ctx, komrade)
	require.NoError(t, err)
	assert.Equal(t, 2, sync.Calls)
	assert.NotNil(t, sync.ReadAt)
	assert.Nil(t, sync.ListedAt, "a request alone is not a listing")

	newest := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	listed := newest.Add(time.Minute)
	require.NoError(t, h.syncs.StartSweep(ctx, StartSweepParams{
		RepositoryURL: komrade,
		Newest:        newest,
		ListedAt:      listed,
	}))
	sync, err = h.syncs.GetSync(ctx, komrade)
	require.NoError(t, err)
	require.True(t, sync.IsSweeping())
	assert.Empty(t, *sync.SweepCursor, "page one has no cursor")

	require.NoError(t, h.syncs.AdvanceSweep(ctx, komrade, "c3"))
	sync, err = h.syncs.GetSync(ctx, komrade)
	require.NoError(t, err)
	require.True(t, sync.IsSweeping())
	assert.Equal(t, "c3", *sync.SweepCursor)
	assert.True(t, sync.SweepNewest.Equal(newest))
	require.NotNil(t, sync.ListedAt)
	assert.True(t, sync.ListedAt.Equal(listed), "listed when the caller says, not when the database does")
	assert.Nil(t, sync.UpdatedThrough, "the mark does not move until the sweep completes")

	require.NoError(t, h.syncs.CompleteSweep(ctx, komrade))
	sync, err = h.syncs.GetSync(ctx, komrade)
	require.NoError(t, err)
	assert.False(t, sync.IsSweeping())
	assert.True(t, sync.IsSwept())
	require.NotNil(t, sync.UpdatedThrough)
	assert.True(t, sync.UpdatedThrough.Equal(newest))

	older := newest.Add(-time.Hour)
	require.NoError(t, h.syncs.StartSweep(ctx, StartSweepParams{
		RepositoryURL: komrade,
		Newest:        older,
		ListedAt:      listed,
	}))
	require.NoError(t, h.syncs.CompleteSweep(ctx, komrade))
	sync, err = h.syncs.GetSync(ctx, komrade)
	require.NoError(t, err)
	assert.True(t, sync.UpdatedThrough.Equal(newest), "an older page one never regresses the mark")

	require.NoError(t, h.syncs.AdvanceMark(ctx, komrade, older))
	sync, err = h.syncs.GetSync(ctx, komrade)
	require.NoError(t, err)
	assert.True(t, sync.UpdatedThrough.Equal(newest), "an older search never regresses the mark")

	later := newest.Add(time.Hour)
	require.NoError(t, h.syncs.AdvanceMark(ctx, komrade, later))
	sync, err = h.syncs.GetSync(ctx, komrade)
	require.NoError(t, err)
	assert.True(t, sync.UpdatedThrough.Equal(later))

	relisted := later.Add(time.Hour)
	require.NoError(t, h.syncs.MarkListed(ctx, komrade, relisted))
	sync, err = h.syncs.GetSync(ctx, komrade)
	require.NoError(t, err)
	assert.True(t, sync.ListedAt.Equal(relisted))
	assert.True(t, sync.UpdatedThrough.Equal(later), "a listing does not move the mark")

	require.NoError(t, h.syncs.ResetSyncCalls(ctx, komrade))
	sync, err = h.syncs.GetSync(ctx, komrade)
	require.NoError(t, err)
	assert.Zero(t, sync.Calls)

	require.NoError(t, h.syncs.EnsureSync(ctx, fixture))
	require.NoError(t, h.syncs.AdvanceSweep(ctx, fixture, "c9"))
	require.NoError(t, h.syncs.ResetSweeps(ctx, []string{komrade, fixture}))
	for _, repository := range []string{komrade, fixture} {
		sync, err = h.syncs.GetSync(ctx, repository)
		require.NoError(t, err)
		assert.False(t, sync.IsSwept(), "%s is back to a first sweep", repository)
		assert.False(t, sync.IsSweeping())
		assert.Nil(t, sync.SweepNewest)
	}
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

// TestListStaleStandingsAnswersOpenRowsOnAgentBranches covers which pull requests cost a
// standing read: open and not draft, on a branch an agent sits on whether or not it is archived,
// and never read, read before the last change, read for another head, or read as pending, as
// none right after a change, or as settled longer ago than the params trust.
func TestListStaleStandingsAnswersOpenRowsOnAgentBranches(t *testing.T) {
	h := open(t)
	ctx := t.Context()

	workflow := h.createWorkflow(t)
	h.createAgent(t, workflow, "a", komrade, "agent/x")
	archived := h.createAgent(t, workflow, "b", komrade, "agent/archived")
	_, err := h.agents.Archive(ctx, []string{archived})
	require.NoError(t, err)

	unread := openPullRequest("PR_1", 1, "agent/x")
	readSince := openPullRequest("PR_2", 2, "agent/x")
	changedSince := openPullRequest("PR_3", 3, "agent/x")
	draft := openPullRequest("PR_4", 4, "agent/x")
	draft.Draft = true
	merged := finishedPullRequest("PR_5", 5, "agent/x")
	noAgent := openPullRequest("PR_6", 6, "agent/nobody")
	onArchived := openPullRequest("PR_7", 7, "agent/archived")
	fork := openPullRequest("PR_8", 8, "agent/x")
	fork.HeadRepositoryURL = "https://github.com/someone/komrade"
	pushed := openPullRequest("PR_9", 9, "agent/x")
	pending := openPullRequest("PR_10", 10, "agent/x")
	failing := openPullRequest("PR_11", 11, "agent/x")
	now := time.Now()
	settling := openPullRequest("PR_12", 12, "agent/x")
	settling.ChangedAt = now
	settled := openPullRequest("PR_13", 13, "agent/x")
	for _, params := range []UpsertParams{unread, readSince, changedSince, draft, merged, noAgent, onArchived, fork, pushed, pending, failing, settling, settled} {
		_, err := h.Upsert(ctx, params)
		require.NoError(t, err)
	}

	set := func(providerID string, standing forge.Standing) {
		t.Helper()
		stored, err := h.GetByIdentity(ctx, "github.com", providerID)
		require.NoError(t, err)
		require.NoError(t, h.SetStanding(ctx, stored.ID, standing, now))
	}
	set("PR_2", forge.Standing{
		Decision: forge.ReviewDecisionApproved,
		Checks:   forge.ChecksPassing,
		HeadSHA:  "abc",
	})
	set("PR_3", forge.Standing{
		Decision: forge.ReviewDecisionApproved,
		Checks:   forge.ChecksPassing,
		HeadSHA:  "abc",
	})
	set("PR_9", forge.Standing{Checks: forge.ChecksPassing, HeadSHA: "before-the-push"})
	set("PR_10", forge.Standing{Checks: forge.ChecksPending, HeadSHA: "abc"})
	set("PR_11", forge.Standing{Checks: forge.ChecksFailing, HeadSHA: "abc"})
	set("PR_12", forge.Standing{HeadSHA: "abc"})
	set("PR_13", forge.Standing{HeadSHA: "abc"})
	changedSince.ChangedAt = changedSince.ChangedAt.Add(time.Hour)
	_, err = h.Upsert(ctx, changedSince)
	require.NoError(t, err)

	list := func(now time.Time) []int {
		t.Helper()
		stale, err := h.syncs.ListStaleStandings(ctx, ListStaleStandingsParams{
			RepositoryURL: komrade,
			Now:           now,
			PendingAfter:  time.Minute,
			SettleWithin:  10 * time.Minute,
			SettledAfter:  10 * time.Minute,
		})
		require.NoError(t, err)

		return xslice.Map(stale, func(pullRequest PullRequest) int {
			return pullRequest.Number
		})
	}

	assert.ElementsMatch(t, []int{1, 3, 7, 9}, list(now), "never read, changed since, and pushed since are stale the moment they are written, an archived agent's branch included")
	assert.ElementsMatch(t, []int{1, 3, 7, 9, 10, 12}, list(now.Add(2*time.Minute)), "a pending run and a fresh change with no checks are read again after a minute")
	assert.ElementsMatch(t, []int{1, 2, 3, 7, 9, 10, 11, 12, 13}, list(now.Add(11*time.Minute)), "every row is read again after ten minutes")
	assert.Equal(t, 3, list(now)[0], "newest change first")
}

// TestSetStandingStampsTheRead covers the standing round trip, none for the decision and the
// checks clearing their columns while still counting as read, and the read stamp moving on
// every write.
func TestSetStandingStampsTheRead(t *testing.T) {
	h := open(t)
	ctx := t.Context()
	readAt := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	written, err := h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
	require.NoError(t, err)

	require.NoError(t, h.SetStanding(ctx, written.ID, forge.Standing{
		Decision:     forge.ReviewDecisionChangesRequested,
		Checks:       forge.ChecksFailing,
		Mergeability: forge.MergeabilityUnstable,
		HeadSHA:      "abc",
	}, readAt))
	stored, err := h.GetByIdentity(ctx, "github.com", "PR_1")
	require.NoError(t, err)
	require.NotNil(t, stored.ReviewDecision)
	assert.Equal(t, forge.ReviewDecisionChangesRequested, *stored.ReviewDecision)
	require.NotNil(t, stored.DecisionChangedAt)
	assert.True(t, stored.DecisionChangedAt.Equal(stored.ChangedAt), "the read is stamped with the change it was for")
	require.NotNil(t, stored.Checks)
	assert.Equal(t, forge.ChecksFailing, *stored.Checks)
	require.NotNil(t, stored.Mergeability)
	assert.Equal(t, forge.MergeabilityUnstable, *stored.Mergeability)
	assert.Equal(t, "abc", stored.ChecksSHA)
	require.NotNil(t, stored.ChecksReadAt)
	assert.True(t, stored.ChecksReadAt.Equal(readAt))

	readAt = readAt.Add(time.Minute)
	require.NoError(t, h.SetStanding(ctx, written.ID, forge.Standing{HeadSHA: "abc"}, readAt))
	stored, err = h.GetByIdentity(ctx, "github.com", "PR_1")
	require.NoError(t, err)
	assert.Nil(t, stored.ReviewDecision)
	assert.NotNil(t, stored.DecisionChangedAt)
	assert.Nil(t, stored.Checks)
	assert.Nil(t, stored.Mergeability)
	assert.Equal(t, "abc", stored.ChecksSHA)
	assert.True(t, stored.ChecksReadAt.Equal(readAt), "the read stamp moves on a write that says nothing new")

	changed := openPullRequest("PR_1", 1, "agent/x")
	changed.ChangedAt = changed.ChangedAt.Add(time.Hour)
	_, err = h.Upsert(ctx, changed)
	require.NoError(t, err)
	require.NoError(t, h.SetStanding(ctx, written.ID, forge.Standing{HeadSHA: "abc"}, readAt))
	stored, err = h.GetByIdentity(ctx, "github.com", "PR_1")
	require.NoError(t, err)
	assert.True(t, stored.DecisionChangedAt.Equal(changed.ChangedAt), "the same decision for a later change is still a read")
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
		RepositoryURL: komrade,
		HeadBranch:    "agent/x",
		Limit:         10,
	})
	require.NoError(t, err)
	assert.Equal(t, []int{3, 5, 1, 2}, numbers(onBranch), "unfinished newest first, then the finished")

	var walked []int
	var cursor *Cursor
	for {
		page, err := h.ListPage(ctx, ListPageParams{
			RepositoryURL: komrade,
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
