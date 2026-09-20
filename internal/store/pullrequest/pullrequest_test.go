package pullrequest

import (
	"testing"
	"time"

	"weave/internal/dbtest"
	"weave/internal/forge"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	weave   = "https://github.com/harrisoncramer/weave"
	fixture = "https://github.com/harrisoncramer/weave-forge-fixture"
)

type harness struct {
	*Store
	pool      *store.Pool
	agents    *agentstore.Store
	workflows *workflowstore.Store
}

func open(t *testing.T) *harness {
	t.Helper()

	pool, err := store.Open(t.Context(), dbtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	return &harness{
		Store:     New(pool.GetDB()),
		pool:      pool,
		agents:    agentstore.New(pool.GetDB()),
		workflows: workflowstore.New(pool.GetDB()),
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
	require.NoError(t, h.agents.SetRepositoryURLByID(ctx, created.ID, &repositoryURL))
	require.NoError(t, h.agents.SetBranchByID(ctx, created.ID, &branch))

	return created.ID
}

func (h *harness) createWorkflow(t *testing.T, name, baseBranch string) string {
	t.Helper()

	created, err := h.workflows.Create(t.Context(), workflowstore.Config{
		Name:     name,
		Worktree: workflowstore.WorktreeConfig{Enabled: baseBranch != "", BaseBranch: baseBranch},
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
		URL:               "https://github.com/harrisoncramer/weave/pull/" + providerID,
		RepositoryURL:     weave,
		HeadRepositoryURL: weave,
		Number:            number,
		Title:             "one",
		HeadBranch:        branch,
		HeadSHA:           "abc",
		BaseBranch:        "main",
		BaseSHA:           "def",
		State:             "open",
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
	merged.State = "closed"
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

// TestObserveJoinsLiveAgentsToTheirBranches covers attribution: an agent on the branch a pull
// request came from is recorded beside it, one on another branch or on the workflow's base
// branch is not, a fork's pull request is attributed to nobody, and a second pass inside the
// hour writes nothing.
func TestObserveJoinsLiveAgentsToTheirBranches(t *testing.T) {
	h := open(t)
	ctx := t.Context()

	workflow := h.createWorkflow(t, "weave", "main")
	onBranch := h.createAgent(t, workflow, "on-branch", weave, "agent/x")
	other := h.createAgent(t, workflow, "elsewhere", weave, "agent/y")
	onMain := h.createAgent(t, workflow, "on-main", weave, "main")

	_, err := h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
	require.NoError(t, err)
	_, err = h.Upsert(ctx, openPullRequest("PR_2", 2, "main"))
	require.NoError(t, err)
	fork := openPullRequest("PR_3", 3, "agent/x")
	fork.HeadRepositoryURL = "https://github.com/someone/weave"
	fork.URL = "https://github.com/harrisoncramer/weave/pull/3"
	_, err = h.Upsert(ctx, fork)
	require.NoError(t, err)

	written, err := h.Observe(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), written)

	seen, err := h.ListObservations(ctx, onBranch)
	require.NoError(t, err)
	require.Len(t, seen, 1)
	assert.Equal(t, seen[0].FirstSeenAt, seen[0].LastSeenAt)

	for _, id := range []string{other, onMain} {
		none, err := h.ListObservations(ctx, id)
		require.NoError(t, err)
		assert.Empty(t, none)
	}

	again, err := h.Observe(ctx)
	require.NoError(t, err)
	assert.Zero(t, again, "a pair seen within the hour is not written again")
}

// TestObserveReachesAnAgentWithNoBaseBranchOnMain covers a workflow that names no base branch,
// where nothing is excluded.
func TestObserveReachesAnAgentWithNoBaseBranchOnMain(t *testing.T) {
	h := open(t)
	ctx := t.Context()

	workflow := h.createWorkflow(t, "plain", "")
	onMain := h.createAgent(t, workflow, "on-main", weave, "main")
	_, err := h.Upsert(ctx, openPullRequest("PR_2", 2, "main"))
	require.NoError(t, err)

	written, err := h.Observe(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), written)

	seen, err := h.ListObservations(ctx, onMain)
	require.NoError(t, err)
	assert.Len(t, seen, 1)
}

// TestObserveSkipsArchivedAgents covers that only live agents are attributed.
func TestObserveSkipsArchivedAgents(t *testing.T) {
	h := open(t)
	ctx := t.Context()

	workflow := h.createWorkflow(t, "weave", "main")
	archived := h.createAgent(t, workflow, "archived", weave, "agent/x")
	_, err := h.agents.Archive(ctx, []string{archived})
	require.NoError(t, err)
	_, err = h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
	require.NoError(t, err)

	written, err := h.Observe(ctx)
	require.NoError(t, err)
	assert.Zero(t, written)
}

// TestLiveRepositoriesAndUnscannedBranches covers the two listings the pass starts from: the
// repositories live agents sit on, and the branches of one whose scan has not completed.
func TestLiveRepositoriesAndUnscannedBranches(t *testing.T) {
	h := open(t)
	ctx := t.Context()

	workflow := h.createWorkflow(t, "weave", "main")
	h.createAgent(t, workflow, "a", weave, "agent/x")
	h.createAgent(t, workflow, "b", weave, "agent/y")
	h.createAgent(t, workflow, "c", fixture, "fixture/open")
	archived := h.createAgent(t, workflow, "d", "https://github.com/harrisoncramer/gone", "agent/z")
	_, err := h.agents.Archive(ctx, []string{archived})
	require.NoError(t, err)

	repositories, err := h.ListLiveRepositories(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{weave, fixture}, repositories)

	branches, err := h.ListUnscannedBranches(ctx, weave)
	require.NoError(t, err)
	assert.Equal(t, []string{"agent/x", "agent/y"}, branches)

	require.NoError(t, h.SetBranchScanNextPage(ctx, weave, "agent/x", 2))
	scan, err := h.GetBranchScan(ctx, weave, "agent/x")
	require.NoError(t, err)
	assert.Equal(t, 2, scan.NextPage)
	assert.Nil(t, scan.CompletedAt)

	branches, err = h.ListUnscannedBranches(ctx, weave)
	require.NoError(t, err)
	assert.Equal(t, []string{"agent/x", "agent/y"}, branches, "a scan in progress is still unscanned")

	require.NoError(t, h.CompleteBranchScan(ctx, weave, "agent/x", 3))
	branches, err = h.ListUnscannedBranches(ctx, weave)
	require.NoError(t, err)
	assert.Equal(t, []string{"agent/y"}, branches)

	_, err = h.GetBranchScan(ctx, weave, "agent/q")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestSyncRowRoundTrip covers the cursor row through a sweep: requests are counted as they are
// made, a sweep holds its columns until it completes, completion promotes the newest stamp and
// the ETag and never regresses the mark, and the validation clock moves only when asked.
func TestSyncRowRoundTrip(t *testing.T) {
	h := open(t)
	ctx := t.Context()

	_, err := h.GetSync(ctx, weave)
	require.ErrorIs(t, err, store.ErrNotFound)

	require.NoError(t, h.EnsureSync(ctx, weave))
	require.NoError(t, h.EnsureSync(ctx, weave))
	sync, err := h.GetSync(ctx, weave)
	require.NoError(t, err)
	assert.Nil(t, sync.UpdatedThrough)
	assert.False(t, sync.IsSweeping())
	assert.Zero(t, sync.Calls)

	require.NoError(t, h.RecordSyncRequest(ctx, weave))
	require.NoError(t, h.RecordSyncRequest(ctx, weave))
	sync, err = h.GetSync(ctx, weave)
	require.NoError(t, err)
	assert.Equal(t, 2, sync.Calls)
	assert.NotNil(t, sync.ReadAt)
	assert.Nil(t, sync.ValidatedAt, "a request alone does not validate")

	newest := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	require.NoError(t, h.StartSweep(ctx, StartSweepParams{
		RepositoryURL: weave,
		Newest:        newest,
		ETag:          `"a"`,
		ValidatedAt:   &newest,
	}))
	require.NoError(t, h.AdvanceSweep(ctx, weave, 3))
	sync, err = h.GetSync(ctx, weave)
	require.NoError(t, err)
	require.True(t, sync.IsSweeping())
	assert.Equal(t, 3, *sync.SweepNextPage)
	assert.True(t, sync.SweepNewest.Equal(newest))
	assert.Equal(t, `"a"`, *sync.SweepETag)
	require.NotNil(t, sync.ValidatedAt)
	assert.True(t, sync.ValidatedAt.Equal(newest), "validated when the caller says, not when the database does")
	assert.Nil(t, sync.UpdatedThrough, "the mark does not move until the sweep completes")

	require.NoError(t, h.CompleteSweep(ctx, weave))
	sync, err = h.GetSync(ctx, weave)
	require.NoError(t, err)
	assert.False(t, sync.IsSweeping())
	require.NotNil(t, sync.UpdatedThrough)
	assert.True(t, sync.UpdatedThrough.Equal(newest))
	assert.Equal(t, `"a"`, sync.ETag)

	older := newest.Add(-time.Hour)
	require.NoError(t, h.StartSweep(ctx, StartSweepParams{
		RepositoryURL: weave,
		Newest:        older,
		ETag:          `"b"`,
	}))
	require.NoError(t, h.CompleteSweep(ctx, weave))
	sync, err = h.GetSync(ctx, weave)
	require.NoError(t, err)
	assert.True(t, sync.UpdatedThrough.Equal(newest), "an older page one never regresses the mark")
	assert.Equal(t, `"b"`, sync.ETag, "the ETag is always the latest page one's")

	require.NoError(t, h.ResetSyncCalls(ctx, weave))
	sync, err = h.GetSync(ctx, weave)
	require.NoError(t, err)
	assert.Zero(t, sync.Calls)
}

// TestListOnBranchIsNewestFirst covers the read the future readers make: pull requests from one
// branch ordered by when the forge opened them.
func TestListOnBranchIsNewestFirst(t *testing.T) {
	h := open(t)
	ctx := t.Context()

	first := openPullRequest("PR_1", 1, "agent/x")
	second := openPullRequest("PR_2", 2, "agent/x")
	second.OpenedAt = first.OpenedAt.Add(time.Hour)
	second.URL = "https://github.com/harrisoncramer/weave/pull/2"
	for _, params := range []UpsertParams{first, second} {
		_, err := h.Upsert(ctx, params)
		require.NoError(t, err)
	}

	listed, err := h.ListOnBranch(ctx, weave, "agent/x")
	require.NoError(t, err)
	require.Len(t, listed, 2)
	assert.Equal(t, 2, listed[0].Number)
	assert.Equal(t, 1, listed[1].Number)
}

// finishedPullRequest is openPullRequest merged an hour after it changed.
func finishedPullRequest(providerID string, number int, branch string) UpsertParams {
	params := openPullRequest(providerID, number, branch)
	mergedAt := params.ChangedAt.Add(time.Hour)
	params.MergedAt = &mergedAt
	params.ClosedAt = &mergedAt
	params.State = "closed"
	params.ChangedAt = mergedAt

	return params
}

// TestListCurrentPullRequestsPicksOnePerAgent covers the choice the row stands for: the newest
// unfinished pull request beats a newer finished one, an agent whose branch holds only finished
// ones gets the newest of those, and an agent whose branch holds none is left out.
func TestListCurrentPullRequestsPicksOnePerAgent(t *testing.T) {
	h := open(t)
	ctx := t.Context()

	workflow := h.createWorkflow(t, "weave", "main")
	unfinished := h.createAgent(t, workflow, "unfinished", weave, "agent/x")
	finished := h.createAgent(t, workflow, "finished", weave, "agent/y")
	h.createAgent(t, workflow, "none", weave, "agent/z")
	h.createAgent(t, workflow, "on-main", weave, "main")

	older := openPullRequest("PR_1", 1, "agent/x")
	newerMerged := finishedPullRequest("PR_2", 2, "agent/x")
	newerMerged.OpenedAt = older.OpenedAt.Add(time.Hour)
	oldMerged := finishedPullRequest("PR_3", 3, "agent/y")
	newMerged := finishedPullRequest("PR_4", 4, "agent/y")
	newMerged.OpenedAt = oldMerged.OpenedAt.Add(time.Hour)
	onMain := openPullRequest("PR_5", 5, "main")
	fork := openPullRequest("PR_6", 6, "agent/z")
	fork.HeadRepositoryURL = "https://github.com/someone/weave"
	for _, params := range []UpsertParams{older, newerMerged, oldMerged, newMerged, onMain, fork} {
		params.URL = "https://github.com/harrisoncramer/weave/pull/" + params.ProviderID
		_, err := h.Upsert(ctx, params)
		require.NoError(t, err)
	}

	current, err := h.ListCurrentPullRequests(ctx)
	require.NoError(t, err)

	byAgent := map[string]int{}
	for _, entry := range current {
		byAgent[entry.AgentID] = entry.PullRequest.Number
	}
	assert.Equal(t, map[string]int{unfinished: 1, finished: 4}, byAgent)
}

// TestListCurrentPullRequestsSkipsArchivedAgents covers that only live rows are answered.
func TestListCurrentPullRequestsSkipsArchivedAgents(t *testing.T) {
	h := open(t)
	ctx := t.Context()

	workflow := h.createWorkflow(t, "weave", "main")
	archived := h.createAgent(t, workflow, "archived", weave, "agent/x")
	_, err := h.agents.Archive(ctx, []string{archived})
	require.NoError(t, err)
	_, err = h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
	require.NoError(t, err)

	current, err := h.ListCurrentPullRequests(ctx)
	require.NoError(t, err)
	assert.Empty(t, current)
}

// TestListReadRepositories covers the sync rows that tell an unread repository from one read
// and found empty.
func TestListReadRepositories(t *testing.T) {
	h := open(t)
	ctx := t.Context()

	require.NoError(t, h.EnsureSync(ctx, weave))
	require.NoError(t, h.EnsureSync(ctx, fixture))
	require.NoError(t, h.RecordSyncRequest(ctx, fixture))

	read, err := h.ListReadRepositories(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{fixture}, read)
}

// TestListStaleDecisionsAnswersOpenRowsOnLiveBranches covers which pull requests cost a
// decision read: open and not draft, on a branch a live agent sits on, and never read or read
// before the last change.
func TestListStaleDecisionsAnswersOpenRowsOnLiveBranches(t *testing.T) {
	h := open(t)
	ctx := t.Context()

	workflow := h.createWorkflow(t, "weave", "main")
	h.createAgent(t, workflow, "a", weave, "agent/x")
	archived := h.createAgent(t, workflow, "b", weave, "agent/archived")
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
	fork.HeadRepositoryURL = "https://github.com/someone/weave"
	for _, params := range []UpsertParams{unread, readSince, changedSince, draft, merged, noAgent, onArchived, fork} {
		params.URL = "https://github.com/harrisoncramer/weave/pull/" + params.ProviderID
		_, err := h.Upsert(ctx, params)
		require.NoError(t, err)
	}

	for _, providerID := range []string{"PR_2", "PR_3"} {
		stored, err := h.GetByIdentity(ctx, "github.com", providerID)
		require.NoError(t, err)
		require.NoError(t, h.SetReviewDecision(ctx, stored.ID, forge.ReviewDecisionApproved))
	}
	changedSince.ChangedAt = changedSince.ChangedAt.Add(time.Hour)
	changedSince.URL = "https://github.com/harrisoncramer/weave/pull/PR_3"
	_, err = h.Upsert(ctx, changedSince)
	require.NoError(t, err)

	stale, err := h.ListStaleDecisions(ctx, weave)
	require.NoError(t, err)

	numbers := make([]int, 0, len(stale))
	for _, pullRequest := range stale {
		numbers = append(numbers, pullRequest.Number)
	}
	assert.Equal(t, []int{3, 1}, numbers, "newest change first")
}

// TestSetReviewDecisionStampsTheRead covers the decision round trip, an empty decision
// clearing the column while still counting as read.
func TestSetReviewDecisionStampsTheRead(t *testing.T) {
	h := open(t)
	ctx := t.Context()

	written, err := h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
	require.NoError(t, err)

	require.NoError(t, h.SetReviewDecision(ctx, written.ID, forge.ReviewDecisionChangesRequested))
	stored, err := h.GetByIdentity(ctx, "github.com", "PR_1")
	require.NoError(t, err)
	require.NotNil(t, stored.ReviewDecision)
	assert.Equal(t, forge.ReviewDecisionChangesRequested, *stored.ReviewDecision)
	require.NotNil(t, stored.DecisionChangedAt)
	assert.True(t, stored.DecisionChangedAt.Equal(stored.ChangedAt), "the read is stamped with the change it was for")

	require.NoError(t, h.SetReviewDecision(ctx, written.ID, forge.ReviewDecisionNone))
	stored, err = h.GetByIdentity(ctx, "github.com", "PR_1")
	require.NoError(t, err)
	assert.Nil(t, stored.ReviewDecision)
	assert.NotNil(t, stored.DecisionChangedAt)
}

// TestListForAgentIsTheBranchNewestFirst covers the listing the details pane reads: every pull
// request from the agent's branch on the terms its row is attributed by, newest opened first,
// and none for an agent on the workflow's base branch or one nobody holds.
func TestListForAgentIsTheBranchNewestFirst(t *testing.T) {
	h := open(t)
	ctx := t.Context()

	workflow := h.createWorkflow(t, "weave", "main")
	agent := h.createAgent(t, workflow, "a", weave, "agent/x")
	onMain := h.createAgent(t, workflow, "on-main", weave, "main")

	first := openPullRequest("PR_1", 1, "agent/x")
	second := finishedPullRequest("PR_2", 2, "agent/x")
	second.OpenedAt = first.OpenedAt.Add(time.Hour)
	fork := openPullRequest("PR_3", 3, "agent/x")
	fork.HeadRepositoryURL = "https://github.com/someone/weave"
	fromMain := openPullRequest("PR_4", 4, "main")
	for _, params := range []UpsertParams{first, second, fork, fromMain} {
		params.URL = "https://github.com/harrisoncramer/weave/pull/" + params.ProviderID
		_, err := h.Upsert(ctx, params)
		require.NoError(t, err)
	}

	listed, err := h.ListForAgent(ctx, agent)
	require.NoError(t, err)
	require.Len(t, listed, 2)
	assert.Equal(t, 2, listed[0].Number)
	assert.Equal(t, 1, listed[1].Number)

	none, err := h.ListForAgent(ctx, onMain)
	require.NoError(t, err)
	assert.Empty(t, none)

	_, err = h.ListForAgent(ctx, "not-an-id")
	require.ErrorIs(t, err, store.ErrNotFound)
}
