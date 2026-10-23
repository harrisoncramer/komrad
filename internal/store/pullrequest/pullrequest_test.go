package pullrequest

import (
	"testing"
	"time"

	"komrad/internal/dbtest"
	"komrad/internal/forge"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	taskstore "komrad/internal/store/task"
	"komrad/internal/store/task/tasktest"
	workflowstore "komrad/internal/store/workflow"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	forgev1 "komrad/proto/gen/forge/v1"

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
func (h *harness) createAgent(t *testing.T, workflowID uuid.UUID, name, repositoryURL, branch string) uuid.UUID {
	t.Helper()
	ctx := t.Context()

	taskID, err := tasktest.Seed(ctx, h.pool.GetDB(), workflowID)
	require.NoError(t, err)
	require.NoError(t, h.agents.Create(ctx, agentstore.Agent{
		TaskID:     taskID,
		Name:       name,
		Pane:       "%1",
		State:      agentstore.StateIdle,
		WorkflowID: workflowID,
	}))
	created, err := h.agents.GetInWorkflow(ctx, workflowID, name)
	require.NoError(t, err)
	h.setHead(t, created.ID, taskstore.SetHeadParams{RepositoryURL: &repositoryURL, Branch: &branch})

	return created.ID
}

// setHead records what the cwd syncer would read of the checkout of an agent's task.
func (h *harness) setHead(t *testing.T, agentID uuid.UUID, params taskstore.SetHeadParams) {
	t.Helper()

	require.NoError(t, taskstore.NewTaskStore(h.pool.GetDB()).SetHead(t.Context(), h.taskOf(t, agentID), params))
}

// taskOf is the id of the task the agent works on.
func (h *harness) taskOf(t *testing.T, agentID uuid.UUID) uuid.UUID {
	t.Helper()

	agent, err := h.agents.GetByID(t.Context(), agentID)
	require.NoError(t, err)
	require.NotNil(t, agent.TaskID)

	return *agent.TaskID
}

func (h *harness) createWorkflow(t *testing.T) uuid.UUID {
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
		Forge:             forgev1.Kind_KIND_GITHUB,
		ProviderID:        providerID,
		URL:               "https://github.com/harrisoncramer/komrad/pull/" + providerID,
		RepositoryURL:     komrad,
		HeadRepositoryURL: komrad,
		Number:            number,
		Title:             "one",
		Author:            "harrisoncramer",
		Body:              "Why it exists.",
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

	edited := openPullRequest("PR_1", 1, "agent/x")
	edited.Body = "Why it exists, said better."
	rewritten, err := h.Upsert(ctx, edited)
	require.NoError(t, err)
	assert.True(t, rewritten.Changed, "an edited description is written")

	read, err := h.GetByIdentity(ctx, "github.com", "PR_1")
	require.NoError(t, err)
	assert.Equal(t, "harrisoncramer", read.Author)
	assert.Equal(t, "Why it exists, said better.", read.Body)

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

	seen, err := h.ListObservations(ctx, []uuid.UUID{h.taskOf(t, onBranch)})
	require.NoError(t, err)
	require.Len(t, seen, 1)
	assert.Equal(t, seen[0].FirstSeenAt, seen[0].LastSeenAt)

	none, err := h.ListObservations(ctx, []uuid.UUID{h.taskOf(t, other), h.taskOf(t, onMain)})
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
	_, err := h.agents.Archive(ctx, []uuid.UUID{archived})
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
	_, err := h.agents.Archive(ctx, []uuid.UUID{archived})
	require.NoError(t, err)
	headed := h.createAgent(t, workflow, "g", komrad, "agent/y")
	repository, branch, head := komrad, "agent/y", "abc"
	h.setHead(t, headed, taskstore.SetHeadParams{
		RepositoryURL: &repository,
		Branch:        &branch,
		HeadSHA:       &head,
	})

	repositories, err := h.ListLiveRepositories(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{komrad, fixture}, repositories)

	liveBranches, err := h.syncs.ListLiveBranches(ctx)
	require.NoError(t, err)
	assert.Equal(t, []LiveBranch{
		{RepositoryURL: komrad, Branch: "agent/x"},
		{RepositoryURL: komrad, Branch: "agent/y"},
		{
			RepositoryURL: komrad,
			Branch:        "agent/y",
			HeadSHA:       "abc",
		},
		{RepositoryURL: fixture, Branch: "fixture/open"},
	}, liveBranches)
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

// TestSetMergeReadinessOnAMissingRowChangesNothing pins that a row gone since it was read reports no change.
func TestSetMergeReadinessOnAMissingRowChangesNothing(t *testing.T) {
	h := open(t)

	change, err := h.SetMergeReadiness(t.Context(), SetMergeReadinessParams{
		ID:             uuid.New(),
		MergeReadiness: forge.MergeReadiness{Listing: &forge.StatusCheckListing{StatusChecks: []forge.StatusCheck{failedTest}}},
		ReadAt:         time.Now(),
	})
	require.NoError(t, err)
	assert.Equal(t, MergeReadinessChange{}, *change)
}

// TestListRecipients pins which agents hear about a pull request.
func TestListRecipients(t *testing.T) {
	h := open(t)
	ctx := t.Context()
	workflow := h.createWorkflow(t)

	older := h.createAgent(t, workflow, "older", komrad, "agent/x")
	h.createAgent(t, workflow, "elsewhere", komrad, "agent/y")
	h.createAgent(t, workflow, "other-repo", "https://github.com/someone/else", "agent/x")
	archived := h.createAgent(t, workflow, "archived", komrad, "agent/x")
	_, err := h.agents.Archive(ctx, []uuid.UUID{archived})
	require.NoError(t, err)
	task := h.taskOf(t, older)
	require.NoError(t, h.agents.Create(ctx, agentstore.Agent{
		TaskID:     &task,
		Name:       "writer",
		Pane:       "%2",
		State:      agentstore.StateIdle,
		WorkflowID: workflow,
	}))
	writer, err := h.agents.GetInWorkflow(ctx, workflow, "writer")
	require.NoError(t, err)
	require.NoError(t, h.agents.Create(ctx, agentstore.Agent{
		TaskID:     &task,
		Name:       "top-level-reviewer",
		Pane:       "%4",
		State:      agentstore.StateIdle,
		WorkflowID: workflow,
		Role:       agentstore.RoleReviewer,
	}))
	require.NoError(t, h.agents.Create(ctx, agentstore.Agent{
		TaskID:     &task,
		Name:       "failed",
		Pane:       "%5",
		State:      agentstore.StateFailedToCreate,
		WorkflowID: workflow,
	}))
	require.NoError(t, h.agents.Create(ctx, agentstore.Agent{
		TaskID:     &task,
		Name:       "reviewer",
		Role:       agentstore.RoleReviewer,
		Pane:       "%3",
		State:      agentstore.StateIdle,
		WorkflowID: workflow,
	}))

	written, err := h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
	require.NoError(t, err)

	recipients, err := h.ListRecipients(ctx, written.ID)
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{writer.ID}, recipients, "only the task's live writer on the branch is told")
}

var failedTest = statusCheckRun("1", "test", forge.StatusCheckStateCompleted, "failure")

// TestSetMergeReadinessRemembersWhatEveryReadLeaves covers the remembered merge readiness reading back from the row as it was after each read.
func TestSetMergeReadinessRemembersWhatEveryReadLeaves(t *testing.T) {
	began := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	failing := forge.MergeReadiness{
		Decision:     forge.ReviewDecisionChangesRequested,
		Listing:      &forge.StatusCheckListing{StatusChecks: []forge.StatusCheck{failedTest}},
		Mergeability: forge.MergeabilityUnstable,
		AutoMerge:    forge.AutoMerge{On: true, Allowed: true},
	}
	passing := forge.MergeReadiness{
		Decision:     forge.ReviewDecisionApproved,
		Listing:      &forge.StatusCheckListing{StatusChecks: []forge.StatusCheck{statusCheckRun("2", "test", forge.StatusCheckStateCompleted, "success")}},
		Mergeability: forge.MergeabilityClean,
		AutoMerge:    forge.AutoMerge{Allowed: true},
	}
	blank := forge.MergeReadiness{Listing: &forge.StatusCheckListing{}}
	failingOnABC := forge.RememberedAnswer[forge.Checks]{Value: forge.ChecksFailing, HeadSHA: "abc"}
	unconfirmedOnABC := forge.RememberedAnswer[forge.Checks]{
		Value:              forge.ChecksFailing,
		HeadSHA:            "abc",
		WasLatestReadBlank: true,
	}
	type mergeReadinessRead struct {
		readiness  forge.MergeReadiness
		head       string
		wantChecks forge.RememberedAnswer[forge.Checks]
	}
	type setMergeReadinessTest struct {
		name  string
		reads []mergeReadinessRead
	}

	tests := []setMergeReadinessTest{
		{
			name: "a blank first read",
			reads: []mergeReadinessRead{
				{
					readiness:  blank,
					head:       "abc",
					wantChecks: forge.RememberedAnswer[forge.Checks]{HeadSHA: "abc", WasLatestReadBlank: true},
				},
				{
					readiness:  failing,
					head:       "abc",
					wantChecks: failingOnABC,
				},
			},
		},
		{
			name: "a read that did not list the checks",
			reads: []mergeReadinessRead{
				{
					readiness:  failing,
					head:       "abc",
					wantChecks: failingOnABC,
				},
				{
					readiness:  forge.MergeReadiness{Decision: forge.ReviewDecisionApproved},
					head:       "def",
					wantChecks: failingOnABC,
				},
			},
		},
		{
			name: "blanks on the same head",
			reads: []mergeReadinessRead{
				{
					readiness:  failing,
					head:       "abc",
					wantChecks: failingOnABC,
				},
				{
					readiness:  blank,
					head:       "abc",
					wantChecks: unconfirmedOnABC,
				},
				{
					readiness:  blank,
					head:       "abc",
					wantChecks: unconfirmedOnABC,
				},
			},
		},
		{
			name: "a blank on a new head",
			reads: []mergeReadinessRead{
				{
					readiness:  failing,
					head:       "abc",
					wantChecks: failingOnABC,
				},
				{
					readiness:  blank,
					head:       "def",
					wantChecks: forge.RememberedAnswer[forge.Checks]{HeadSHA: "def", WasLatestReadBlank: true},
				},
			},
		},
		{
			name: "an answer after a blank",
			reads: []mergeReadinessRead{
				{
					readiness:  failing,
					head:       "abc",
					wantChecks: failingOnABC,
				},
				{
					readiness:  blank,
					head:       "abc",
					wantChecks: unconfirmedOnABC,
				},
				{
					readiness:  passing,
					head:       "def",
					wantChecks: forge.RememberedAnswer[forge.Checks]{Value: forge.ChecksPassing, HeadSHA: "def"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := open(t)
			ctx := t.Context()
			written, err := h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
			require.NoError(t, err)

			var want forge.RememberedMergeReadiness
			for i, read := range tt.reads {
				pushed := openPullRequest("PR_1", 1, "agent/x")
				pushed.HeadSHA = read.head
				_, err := h.Upsert(ctx, pushed)
				require.NoError(t, err)
				at := began.Add(time.Duration(i) * time.Minute)
				change, err := h.SetMergeReadiness(ctx, SetMergeReadinessParams{
					ID:             written.ID,
					MergeReadiness: read.readiness,
					HeadSHA:        read.head,
					ReadAt:         at,
				})
				require.NoError(t, err)
				assert.Equal(t, want, inUTC(change.Before))

				want = want.RememberRead(read.readiness, read.head, at)
				want.Checks = read.wantChecks
				assert.Equal(t, want, inUTC(change.After))

				stored, err := h.GetByID(ctx, written.ID)
				require.NoError(t, err)
				assert.Equal(t, want, inUTC(stored.MergeReadiness))
			}
		})
	}
}

// TestSetMergeReadinessHandsBackTheHeadStatusChecks covers a read handing back every check stored on the head it was read for.
func TestSetMergeReadinessHandsBackTheHeadStatusChecks(t *testing.T) {
	h := open(t)
	ctx := t.Context()
	written, err := h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
	require.NoError(t, err)
	lint := statusCheckRun("2", "lint", forge.StatusCheckStateRunning, "")

	_, err = h.SetMergeReadiness(ctx, SetMergeReadinessParams{
		ID:             written.ID,
		MergeReadiness: forge.MergeReadiness{Listing: &forge.StatusCheckListing{StatusChecks: []forge.StatusCheck{failedTest, lint}}},
		HeadSHA:        "abc",
		ReadAt:         statusCheckReadAt,
	})
	require.NoError(t, err)
	change, err := h.SetMergeReadiness(ctx, SetMergeReadinessParams{
		ID:             written.ID,
		MergeReadiness: forge.MergeReadiness{Listing: &forge.StatusCheckListing{}},
		HeadSHA:        "abc",
		ReadAt:         statusCheckReadAt.Add(time.Minute),
	})
	require.NoError(t, err)

	assert.Equal(t, []forge.StatusCheck{lint, failedTest}, checksInUTC(change.HeadStatusChecks), "a blank read keeps the checks listed before it")
	stored, err := h.GetByID(ctx, written.ID)
	require.NoError(t, err)
	assert.Equal(t, change.HeadStatusChecks, stored.HeadStatusChecks)
}

// TestHeadStatusChecksAreTheHeadsReading covers a pull request's checks coming from the reading of its head, whatever
// head was read after it.
func TestHeadStatusChecksAreTheHeadsReading(t *testing.T) {
	onHead := statusCheckRun("1", "test", forge.StatusCheckStateRunning, "")
	onOld := statusCheckRun("2", "test", forge.StatusCheckStateCompleted, "cancelled")
	onNew := statusCheckRun("3", "test", forge.StatusCheckStateCompleted, "failure")
	checksOn := map[string]forge.StatusCheck{"abc": onHead, "old": onOld, "new": onNew}

	tests := []struct {
		name  string
		heads []string
		want  []forge.StatusCheck
	}{
		{
			name:  "an older head read after the head does not stand for it",
			heads: []string{"abc", "old"},
			want:  []forge.StatusCheck{onHead},
		},
		{
			name:  "a head never read stands on the latest reading",
			heads: []string{"old", "new"},
			want:  []forge.StatusCheck{onNew},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := open(t)
			checks := NewStatusCheckStore(h.pool.GetDB())
			ctx := t.Context()
			written, err := h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
			require.NoError(t, err)

			for i, head := range tt.heads {
				require.NoError(t, checks.SetStatusChecks(ctx, SetStatusChecksParams{
					PullRequestID: written.ID,
					HeadSHA:       head,
					StatusChecks:  []forge.StatusCheck{checksOn[head]},
					ReadAt:        statusCheckReadAt.Add(time.Duration(i) * time.Minute),
				}))
			}

			stored, err := h.GetByID(ctx, written.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.want, checksInUTC(stored.HeadStatusChecks))
		})
	}
}

// TestSetMergeReadinessSaysWhenAFailureIsNew covers a read saying whether it found a failed check the reading before
// it did not hold.
func TestSetMergeReadinessSaysWhenAFailureIsNew(t *testing.T) {
	failedLint := statusCheckRun("2", "lint", forge.StatusCheckStateCompleted, "failure")
	rerunTest := finishedAt(failedTest, statusCheckReadAt)
	passedTest := statusCheckRun("3", "test", forge.StatusCheckStateCompleted, "success")
	type read struct {
		head   string
		checks []forge.StatusCheck
	}

	tests := []struct {
		name  string
		reads []read
		want  bool
	}{
		{
			name:  "a first failure is new",
			reads: []read{{head: "abc", checks: []forge.StatusCheck{failedTest}}},
			want:  true,
		},
		{
			name:  "a read with nothing failed has no new failure",
			reads: []read{{head: "abc", checks: []forge.StatusCheck{passedTest}}},
		},
		{
			name: "a failure read again is not new",
			reads: []read{
				{head: "abc", checks: []forge.StatusCheck{failedTest}},
				{head: "abc", checks: []forge.StatusCheck{failedTest}},
			},
		},
		{
			name: "a blank read keeps the failure it held",
			reads: []read{
				{head: "abc", checks: []forge.StatusCheck{failedTest}},
				{head: "abc"},
			},
		},
		{
			name: "a second failure beside the first is new",
			reads: []read{
				{head: "abc", checks: []forge.StatusCheck{failedTest}},
				{head: "abc", checks: []forge.StatusCheck{failedTest, failedLint}},
			},
			want: true,
		},
		{
			name: "a failure that finished again is new",
			reads: []read{
				{head: "abc", checks: []forge.StatusCheck{failedTest}},
				{head: "abc", checks: []forge.StatusCheck{rerunTest}},
			},
			want: true,
		},
		{
			name: "a failure on a new head is new",
			reads: []read{
				{head: "abc", checks: []forge.StatusCheck{failedTest}},
				{head: "def", checks: []forge.StatusCheck{failedTest}},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := open(t)
			ctx := t.Context()
			written, err := h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
			require.NoError(t, err)

			var change *MergeReadinessChange
			for i, read := range tt.reads {
				pushed := openPullRequest("PR_1", 1, "agent/x")
				pushed.HeadSHA = read.head
				_, err = h.Upsert(ctx, pushed)
				require.NoError(t, err)
				change, err = h.SetMergeReadiness(ctx, SetMergeReadinessParams{
					ID:             written.ID,
					MergeReadiness: forge.MergeReadiness{Listing: &forge.StatusCheckListing{StatusChecks: read.checks}},
					HeadSHA:        read.head,
					ReadAt:         statusCheckReadAt.Add(time.Duration(i) * time.Minute),
				})
				require.NoError(t, err)
			}

			assert.Equal(t, tt.want, change.NewFailure)
		})
	}
}

// TestSetMergeReadinessWritesNothingNew covers a read that leaves the remembered merge readiness as it was writing nothing.
func TestSetMergeReadinessWritesNothingNew(t *testing.T) {
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
	params := SetMergeReadinessParams{
		ID:             written.ID,
		MergeReadiness: forge.MergeReadiness{Listing: &forge.StatusCheckListing{StatusChecks: []forge.StatusCheck{failedTest}}},
		HeadSHA:        "abc",
		ReadAt:         time.Now(),
	}

	_, err = h.SetMergeReadiness(ctx, params)
	require.NoError(t, err)
	before := version()
	_, err = h.SetMergeReadiness(ctx, params)
	require.NoError(t, err)
	assert.Equal(t, before, version())
}

func inUTC(readiness forge.RememberedMergeReadiness) forge.RememberedMergeReadiness {
	if readiness.MergeabilityBlankSince != nil {
		readiness.MergeabilityBlankSince = xptr.Ptr(readiness.MergeabilityBlankSince.UTC())
	}

	return readiness
}

// checksInUTC is the checks with their start times in UTC.
func checksInUTC(checks []forge.StatusCheck) []forge.StatusCheck {
	return xslice.Map(checks, func(check forge.StatusCheck) forge.StatusCheck {
		check.StartedAt = xptr.Ptr(check.StartedAt.UTC())
		check.RunStartedAt = xptr.Ptr(check.RunStartedAt.UTC())

		return check
	})
}

// TestGetParentPullRequest pins which pull request on a parent's branch is the child's parent pull request.
func TestGetParentPullRequest(t *testing.T) {
	began := time.Date(2026, 9, 18, 13, 0, 0, 0, time.UTC)
	openedAfter := func(params UpsertParams, after time.Duration) UpsertParams {
		params.OpenedAt = began.Add(after)

		return params
	}
	fromFork := openPullRequest("PR_fork", 1, "task/parent")
	fromFork.HeadRepositoryURL = fixture

	tests := []struct {
		name         string
		pullRequests []UpsertParams
		wantProvider string
	}{
		{
			name:         "a pull request that finished before the child took the parent is not its parent pull request",
			pullRequests: []UpsertParams{finishedPullRequest("PR_old", 1, "task/parent")},
		},
		{
			name:         "a pull request from a fork is not followed",
			pullRequests: []UpsertParams{fromFork},
		},
		{
			name:         "a pull request from another branch is not followed",
			pullRequests: []UpsertParams{openPullRequest("PR_other", 1, "task/other")},
		},
		{
			name:         "an open pull request is followed whenever it opened",
			pullRequests: []UpsertParams{openPullRequest("PR_open", 1, "task/parent")},
			wantProvider: "PR_open",
		},
		{
			name:         "a pull request opened since the child took the parent stays its parent pull request after it finishes",
			pullRequests: []UpsertParams{openedAfter(finishedPullRequest("PR_new", 1, "task/parent"), time.Minute)},
			wantProvider: "PR_new",
		},
		{
			name: "the earliest of several is followed",
			pullRequests: []UpsertParams{
				openedAfter(openPullRequest("PR_later", 2, "task/parent"), time.Hour),
				openPullRequest("PR_first", 1, "task/parent"),
			},
			wantProvider: "PR_first",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := open(t)
			for _, params := range tt.pullRequests {
				_, err := h.Upsert(t.Context(), params)
				require.NoError(t, err)
			}

			found, err := h.GetParentPullRequest(t.Context(), GetParentPullRequestParams{
				RepositoryURL: komrad,
				HeadBranch:    "task/parent",
				Since:         began,
			})
			if tt.wantProvider == "" {
				require.ErrorIs(t, err, store.ErrNotFound)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantProvider, found.ProviderID)
		})
	}
}

// TestGetMergeBlockers pins what turns down a merge of a pull request in a stack.
func TestGetMergeBlockers(t *testing.T) {
	childOntoParent := openPullRequest("PR_child", 2, "task/child")
	childOntoParent.BaseBranch = "task/parent"
	parentReopened := openPullRequest("PR_again", 3, "task/parent")
	parentReopened.OpenedAt = parentReopened.OpenedAt.Add(2 * time.Hour)

	tests := []struct {
		name         string
		pullRequests []UpsertParams
		url          string
		want         mergeBlockers
	}{
		{
			name:         "a pull request from a child onto its parent's open one comes from a child",
			pullRequests: []UpsertParams{openPullRequest("PR_parent", 1, "task/parent"), childOntoParent},
			url:          childOntoParent.URL,
			want:         mergeBlockers{HeadHasParent: true},
		},
		{
			name:         "a pull request onto a parent whose pull request merged targets a finished branch",
			pullRequests: []UpsertParams{finishedPullRequest("PR_parent", 1, "task/parent"), childOntoParent},
			url:          childOntoParent.URL,
			want:         mergeBlockers{HeadHasParent: true, BaseFinished: true},
		},
		{
			name:         "a parent with a pull request open again is not finished",
			pullRequests: []UpsertParams{finishedPullRequest("PR_parent", 1, "task/parent"), parentReopened, childOntoParent},
			url:          childOntoParent.URL,
			want:         mergeBlockers{HeadHasParent: true},
		},
		{
			name:         "a pull request from a task on the base branch has no blockers",
			pullRequests: []UpsertParams{openPullRequest("PR_parent", 1, "task/parent")},
			url:          openPullRequest("PR_parent", 1, "task/parent").URL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := open(t)
			ctx := t.Context()
			workflow := h.createWorkflow(t)
			parent := h.createAgent(t, workflow, "parent", komrad, "task/parent")
			child := h.createAgent(t, workflow, "child", komrad, "task/child")
			require.NoError(t, taskstore.NewTaskStore(h.pool.GetDB()).SetParent(ctx, h.taskOf(t, child), &taskstore.Parent{
				TaskID:    h.taskOf(t, parent),
				ForkPoint: "abc",
			}))
			for _, params := range tt.pullRequests {
				_, err := h.Upsert(ctx, params)
				require.NoError(t, err)
			}

			got, err := h.GetMergeBlockers(ctx, tt.url)
			require.NoError(t, err)
			assert.Equal(t, tt.want, *got)
		})
	}

	t.Run("a pull request komrad has not stored is not found", func(t *testing.T) {
		_, err := open(t).GetMergeBlockers(t.Context(), "https://github.com/harrisoncramer/komrad/pull/404")

		require.ErrorIs(t, err, store.ErrNotFound)
	})
}

// TestListTasksLeftOnParentBranches pins which tasks that left their parent still have a pull request onto the parent's branch.
func TestListTasksLeftOnParentBranches(t *testing.T) {
	childOntoParent := openPullRequest("PR_child", 2, "task/child")
	childOntoParent.BaseBranch = "task/parent"
	childOntoMain := openPullRequest("PR_child", 2, "task/child")
	childChanged := childOntoParent
	childChanged.ChangedAt = childChanged.ChangedAt.Add(time.Hour)

	tests := []struct {
		name         string
		pullRequests []UpsertParams
		left         bool
		hasParent    bool
		refused      bool
		changed      bool
		wantChild    bool
	}{
		{
			name:         "a pull request onto the parent the task left is listed",
			pullRequests: []UpsertParams{openPullRequest("PR_parent", 1, "task/parent"), childOntoParent},
			left:         true,
			wantChild:    true,
		},
		{
			name:         "a pull request onto another task's branch that no parent link put there is not listed",
			pullRequests: []UpsertParams{openPullRequest("PR_parent", 1, "task/parent"), childOntoParent},
		},
		{
			name:         "a task given a parent again is left to the cascade",
			pullRequests: []UpsertParams{openPullRequest("PR_parent", 1, "task/parent"), childOntoParent},
			left:         true,
			hasParent:    true,
		},
		{
			name:         "a pull request already on the base branch is not listed",
			pullRequests: []UpsertParams{finishedPullRequest("PR_parent", 1, "task/parent"), childOntoMain},
			left:         true,
		},
		{
			name:         "a pull request the forge refused to re-target as it stands is not listed",
			pullRequests: []UpsertParams{finishedPullRequest("PR_parent", 1, "task/parent"), childOntoParent},
			left:         true,
			refused:      true,
		},
		{
			name:         "a pull request that changed since the forge refused is listed again",
			pullRequests: []UpsertParams{finishedPullRequest("PR_parent", 1, "task/parent"), childOntoParent},
			left:         true,
			refused:      true,
			changed:      true,
			wantChild:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := open(t)
			ctx := t.Context()
			tasks := taskstore.NewTaskStore(h.pool.GetDB())
			workflow := h.createWorkflow(t)
			parent := h.createAgent(t, workflow, "parent", komrad, "task/parent")
			child := h.createAgent(t, workflow, "child", komrad, "task/child")
			if tt.left {
				require.NoError(t, tasks.ClearParent(ctx, h.taskOf(t, child), "task/parent"))
			}
			if tt.hasParent {
				require.NoError(t, tasks.SetParent(ctx, h.taskOf(t, child), &taskstore.Parent{
					TaskID:    h.taskOf(t, parent),
					ForkPoint: "abc",
				}))
			}
			for _, params := range tt.pullRequests {
				_, err := h.Upsert(ctx, params)
				require.NoError(t, err)
			}
			if tt.refused {
				refused, err := h.GetByIdentity(ctx, "github.com", "PR_child")
				require.NoError(t, err)
				require.NoError(t, h.RecordRetargetRefused(ctx, *refused))
			}
			if tt.changed {
				_, err := h.Upsert(ctx, childChanged)
				require.NoError(t, err)
			}

			got, err := h.ListTasksLeftOnParentBranches(ctx)
			require.NoError(t, err)
			if !tt.wantChild {
				assert.Empty(t, got)

				return
			}
			assert.Equal(t, []LeftParent{{TaskID: h.taskOf(t, child), Branch: "task/parent"}}, got)
		})
	}
}

// TestLiveBranchesFollowTheParentPullRequest covers a parent's pull request staying a sync target
// while a live task follows it, after every agent on the parent is archived.
func TestLiveBranchesFollowTheParentPullRequest(t *testing.T) {
	h := open(t)
	ctx := t.Context()
	workflow := h.createWorkflow(t)
	parent := h.createAgent(t, workflow, "parent", komrad, "task/parent")
	child := h.createAgent(t, workflow, "child", komrad, "task/child")
	_, err := h.Upsert(ctx, openPullRequest("PR_parent", 1, "task/parent"))
	require.NoError(t, err)
	followed, err := h.GetByIdentity(ctx, "github.com", "PR_parent")
	require.NoError(t, err)
	require.NoError(t, taskstore.NewTaskStore(h.pool.GetDB()).SetParent(ctx, h.taskOf(t, child), &taskstore.Parent{
		TaskID:        h.taskOf(t, parent),
		ForkPoint:     "abc",
		PullRequestID: &followed.ID,
	}))
	_, err = h.agents.Archive(ctx, []uuid.UUID{parent})
	require.NoError(t, err)

	liveBranches, err := h.syncs.ListLiveBranches(ctx)
	require.NoError(t, err)
	assert.Equal(t, []LiveBranch{
		{RepositoryURL: komrad, Branch: "task/child"},
		{RepositoryURL: komrad, Branch: "task/parent", HeadSHA: "abc"},
	}, liveBranches)
}
