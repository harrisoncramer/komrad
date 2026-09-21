package row_test

import (
	"testing"
	"time"

	"weave/internal/forge"
	"weave/internal/storetest"
	"weave/lib/xptr"
	"weave/lib/xslice"

	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	markstore "weave/internal/store/mark"
	pullrequeststore "weave/internal/store/pullrequest"
	reviewstore "weave/internal/store/review"
	rowstore "weave/internal/store/row"
	runstore "weave/internal/store/run"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	weave   = "https://github.com/harrisoncramer/weave"
	fixture = "https://github.com/harrisoncramer/weave-forge-fixture"
)

// rowOf reads one agent's row back through the by-id listing.
func rowOf(t *testing.T, st *storetest.Stores, id string) rowstore.Row {
	t.Helper()

	rows, err := st.Rows.ListByID(t.Context(), []string{id})
	require.NoError(t, err)
	require.Len(t, rows, 1)

	return rows[0]
}

// TestRowCarriesTheAgent pins that the row is the agent as stored, metadata included, with
// nothing beside it for an agent nothing has happened to.
func TestRowCarriesTheAgent(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()
	alpha := st.SeedAgent(t, agentstore.Agent{Name: "alpha", Cwd: "/repo", Prompt: "go"})
	_, err := st.Agents.SetMetadata(ctx, agentstore.SetMetadataParams{ID: alpha.ID, Key: "branch", Value: xptr.Ptr("agent/x")})
	require.NoError(t, err)

	row := rowOf(t, st, alpha.ID)

	assert.Equal(t, "alpha", row.Name)
	assert.Equal(t, "/repo", row.Cwd)
	assert.Equal(t, "go", row.Prompt)
	assert.Equal(t, []agentstore.Metadata{{Key: "branch", Value: "agent/x"}}, row.Metadata)
	assert.False(t, row.UnreadMail)
	assert.False(t, row.FailedRun)
	assert.Nil(t, row.FailedRunLog)
	assert.Nil(t, row.Mark)
	assert.Zero(t, row.LocalRounds)
	assert.Nil(t, row.LocalVerdict)
	assert.Empty(t, row.LocalSnapshotSHA)
	assert.Nil(t, row.PullRequest)
	assert.False(t, row.RepositoryRead)
}

// TestUnreadMail pins what makes a mailbox unread: mail still waiting to be claimed, and neither
// mail already taken nor mail nobody can read any more.
func TestUnreadMail(t *testing.T) {
	tests := []struct {
		name     string
		deadline time.Duration
		claim    bool
		want     bool
	}{
		{name: "live unclaimed mail", deadline: time.Hour, want: true},
		{name: "claimed mail", deadline: time.Hour, claim: true, want: false},
		{name: "expired mail", deadline: -time.Minute, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			ctx := t.Context()
			recipient := st.SeedAgent(t, agentstore.Agent{Name: "recipient"})

			_, err := st.Mail.Send(ctx, mailstore.SendParams{ToAgentID: recipient.ID, Body: "hello", Deadline: tt.deadline})
			require.NoError(t, err)
			if tt.claim {
				_, err = st.Mail.Claim(ctx, recipient.ID, 10)
				require.NoError(t, err)
			}

			assert.Equal(t, tt.want, rowOf(t, st, recipient.ID).UnreadMail)
		})
	}
}

// TestFailedRun pins the mark a failed script puts on a row: it names the run that failed last,
// a run that succeeded is nothing to read, and reading clears it.
func TestFailedRun(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()
	alpha := st.SeedAgent(t, agentstore.Agent{Name: "alpha"})

	fine, err := st.Runs.Begin(ctx, runstore.BeginParams{AgentID: alpha.ID, Trigger: "post-launch", Script: "setup"})
	require.NoError(t, err)
	require.NoError(t, st.Runs.Finish(ctx, runstore.FinishParams{ID: fine, ExitCode: xptr.Ptr(int32(0))}))
	assert.False(t, rowOf(t, st, alpha.ID).FailedRun, "a run that succeeded is nothing to read")

	for number := range 3 {
		id, err := st.Runs.Begin(ctx, runstore.BeginParams{
			AgentID:   alpha.ID,
			Trigger:   "post-launch",
			Script:    "setup",
			LogNumber: xptr.Ptr(int32(number + 1)),
		})
		require.NoError(t, err)
		require.NoError(t, st.Runs.Finish(ctx, runstore.FinishParams{ID: id, Failure: "it failed"}))
	}

	row := rowOf(t, st, alpha.ID)
	assert.True(t, row.FailedRun)
	require.NotNil(t, row.FailedRunLog, "the mark carries the run to open on")
	assert.Equal(t, int32(3), *row.FailedRunLog, "which is the one that failed last")

	require.NoError(t, st.Runs.MarkRead(ctx, alpha.ID))
	assert.False(t, rowOf(t, st, alpha.ID).FailedRun, "reading it clears the mark")
}

// TestMark pins that the letter on an agent's row is carried, and a workflow's letter is not.
func TestMark(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()
	alpha := st.SeedAgent(t, agentstore.Agent{Name: "alpha"})
	beta := st.SeedAgent(t, agentstore.Agent{Name: "beta"})

	_, err := st.Marks.Set(ctx, "a", markstore.OnAgent(alpha.ID))
	require.NoError(t, err)
	_, err = st.Marks.Set(ctx, "w", markstore.OnWorkflow(beta.WorkflowID))
	require.NoError(t, err)

	assert.Equal(t, xptr.Ptr("a"), rowOf(t, st, alpha.ID).Mark)
	assert.Nil(t, rowOf(t, st, beta.ID).Mark)
}

// TestLocalReview pins the loop the row draws: released rounds are counted against the agent
// whose work went round, the last conclusion stands with the round it was given on, and a
// comment concludes nothing.
func TestLocalReview(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()
	reviews := reviewstore.New(st.Pool.GetDB())
	agent := st.SeedAgent(t, agentstore.Agent{Name: "worker"})
	reviewer := st.SeedAgent(t, agentstore.Agent{Name: "reader", WorkflowID: agent.WorkflowID})

	release := func(tree string, kind reviewstore.AuthorKind, verdict reviewstore.Verdict) {
		round, err := reviews.CreateRound(ctx, reviewstore.CreateRoundParams{AgentID: agent.ID, BaseSHA: "base-" + tree, SnapshotSHA: "snapshot-" + tree})
		require.NoError(t, err)

		params := reviewstore.OpenReviewParams{AgentID: agent.ID, RoundID: round.ID, AuthorKind: kind}
		if kind == reviewstore.AuthorAgent {
			params.AuthorAgentID = &reviewer.ID
		}
		review, err := reviews.OpenReview(ctx, params)
		require.NoError(t, err)
		_, err = reviews.ReleaseReview(ctx, reviewstore.ReleaseReviewParams{ID: review.ID, Verdict: verdict, Path: "/tmp/" + tree + ".md"})
		require.NoError(t, err)
	}

	release("one", reviewstore.AuthorAgent, reviewstore.VerdictChangesRequested)
	release("two", reviewstore.AuthorAgent, reviewstore.VerdictApproved)
	release("three", reviewstore.AuthorHuman, reviewstore.VerdictCommented)

	row := rowOf(t, st, agent.ID)
	assert.Equal(t, int32(3), row.LocalRounds)
	assert.Equal(t, xptr.Ptr(reviewstore.VerdictApproved), row.LocalVerdict)
	assert.Equal(t, "snapshot-two", row.LocalSnapshotSHA, "the comment on the third round leaves the approval and its round standing")

	assert.Zero(t, rowOf(t, st, reviewer.ID).LocalRounds, "the reviewer holds no diff of its own to have gone round")
}

// TestPullRequest pins which pull request a row stands for: the newest unfinished one on the
// agent's branch beats a newer finished one, a branch holding only finished ones gets the newest
// of those, a fork's and the base branch's are left out, and an archived agent stands for none.
func TestPullRequest(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()
	workflow := st.SeedWorkflow(t, workflowstore.Config{
		Name:     "weave",
		Worktree: workflowstore.WorktreeConfig{Enabled: true, BaseBranch: "main"},
	})
	onBranch := func(name, branch string) *agentstore.Agent {
		agent := st.SeedAgent(t, agentstore.Agent{Name: name, WorkflowID: workflow.ID})
		require.NoError(t, st.Agents.SetCheckout(ctx, agent.ID, agentstore.SetCheckoutParams{RepositoryURL: xptr.Ptr(weave), Branch: &branch}))

		return agent
	}
	unfinished := onBranch("unfinished", "agent/x")
	finished := onBranch("finished", "agent/y")
	none := onBranch("none", "agent/z")
	onMain := onBranch("on-main", "main")
	archived := onBranch("archived", "agent/w")
	_, err := st.Agents.Archive(ctx, []string{archived.ID})
	require.NoError(t, err)

	older := openPullRequest("PR_1", 1, "agent/x")
	newerMerged := finishedPullRequest("PR_2", 2, "agent/x")
	newerMerged.OpenedAt = older.OpenedAt.Add(time.Hour)
	oldMerged := finishedPullRequest("PR_3", 3, "agent/y")
	newMerged := finishedPullRequest("PR_4", 4, "agent/y")
	newMerged.OpenedAt = oldMerged.OpenedAt.Add(time.Hour)
	fork := openPullRequest("PR_6", 6, "agent/z")
	fork.HeadRepositoryURL = "https://github.com/someone/weave"
	for _, params := range []pullrequeststore.UpsertParams{older, newerMerged, oldMerged, newMerged, openPullRequest("PR_5", 5, "main"), fork, openPullRequest("PR_7", 7, "agent/w")} {
		_, err := st.PullRequests.Upsert(ctx, params)
		require.NoError(t, err)
	}
	approved, err := st.PullRequests.Upsert(ctx, older)
	require.NoError(t, err)
	require.NoError(t, st.PullRequests.SetReviewDecision(ctx, approved.ID, forge.ReviewDecisionApproved))

	assert.Equal(t, &pullrequeststore.Summary{
		Number:         1,
		URL:            "https://github.com/harrisoncramer/weave/pull/PR_1",
		Title:          "one",
		ReviewDecision: xptr.Ptr(forge.ReviewDecisionApproved),
	}, rowOf(t, st, unfinished.ID).PullRequest)

	current := rowOf(t, st, finished.ID).PullRequest
	require.NotNil(t, current)
	assert.Equal(t, 4, current.Number)
	assert.NotNil(t, current.MergedAt)

	assert.Nil(t, rowOf(t, st, none.ID).PullRequest, "a fork's pull request is not the agent's")
	assert.Nil(t, rowOf(t, st, onMain.ID).PullRequest, "the base branch's pull requests are nobody's")
	assert.Nil(t, rowOf(t, st, archived.ID).PullRequest)
}

// TestRepositoryRead pins the flag that tells an unread repository from one whose first sweep
// completed and found nothing.
func TestRepositoryRead(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()
	agent := st.SeedAgent(t, agentstore.Agent{Name: "worker"})
	require.NoError(t, st.Agents.SetCheckout(ctx, agent.ID, agentstore.SetCheckoutParams{RepositoryURL: xptr.Ptr(fixture), Branch: xptr.Ptr("agent/x")}))

	require.NoError(t, st.PullRequestSyncs.EnsureSync(ctx, fixture))
	require.NoError(t, st.PullRequestSyncs.RecordSyncRequest(ctx, fixture))
	listed := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	require.NoError(t, st.PullRequestSyncs.StartSweep(ctx, pullrequeststore.StartSweepParams{RepositoryURL: fixture, Newest: listed, ListedAt: listed}))
	assert.False(t, rowOf(t, st, agent.ID).RepositoryRead, "a request alone, or a sweep in progress, is not a read")

	require.NoError(t, st.PullRequestSyncs.CompleteSweep(ctx, fixture))
	assert.True(t, rowOf(t, st, agent.ID).RepositoryRead)
}

// TestListPage pins the paging: live or archived rows, newest first, from the cursor on, with
// the rows a delete has claimed left out.
func TestListPage(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()
	st.SeedAgent(t, agentstore.Agent{Name: "first"})
	second := st.SeedAgent(t, agentstore.Agent{Name: "second"})
	third := st.SeedAgent(t, agentstore.Agent{Name: "third"})
	archived := st.SeedAgent(t, agentstore.Agent{Name: "archived"})
	deleting := st.SeedAgent(t, agentstore.Agent{Name: "deleting"})
	archivedDeleting := st.SeedAgent(t, agentstore.Agent{Name: "archived-deleting"})
	_, err := st.Agents.Archive(ctx, []string{archived.ID, archivedDeleting.ID})
	require.NoError(t, err)
	require.NoError(t, st.Agents.MarkDeleting(ctx, []string{deleting.ID, archivedDeleting.ID}))

	names := func(rows []rowstore.Row) []string {
		listed := xslice.Map(rows, func(row rowstore.Row) string {
			return row.Name
		})

		return listed
	}

	live, err := st.Rows.ListPage(ctx, rowstore.Page{Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, []string{"third", "second", "first"}, names(live))

	rest, err := st.Rows.ListPage(ctx, rowstore.Page{Limit: 1, Cursor: &agentstore.Cursor{ID: third.ID}})
	require.NoError(t, err)
	assert.Equal(t, []string{"second"}, names(rest))
	assert.Equal(t, second.ID, rest[0].ID)

	old, err := st.Rows.ListPage(ctx, rowstore.Page{Limit: 10, Archived: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"archived"}, names(old))
	assert.Equal(t, archived.ID, old[0].ID)
}

// TestListByID pins the by-id read: archived rows answer too, an id nothing holds is left
// out, and one that could not be an id at all does not fail the read.
func TestListByID(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()
	live := st.SeedAgent(t, agentstore.Agent{Name: "live"})
	archived := st.SeedAgent(t, agentstore.Agent{Name: "archived"})
	_, err := st.Agents.Archive(ctx, []string{archived.ID})
	require.NoError(t, err)

	rows, err := st.Rows.ListByID(ctx, []string{live.ID, archived.ID, "00000000-0000-0000-0000-000000000000", "not-an-id"})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, archived.ID, rows[0].ID)
	assert.Equal(t, live.ID, rows[1].ID)

	rows, err = st.Rows.ListByID(ctx, []string{"not-an-id"})
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func openPullRequest(providerID string, number int, branch string) pullrequeststore.UpsertParams {
	opened := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

	return pullrequeststore.UpsertParams{
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
		State:             "OPEN",
		OpenedAt:          opened,
		ChangedAt:         opened.Add(time.Hour),
	}
}

func finishedPullRequest(providerID string, number int, branch string) pullrequeststore.UpsertParams {
	params := openPullRequest(providerID, number, branch)
	mergedAt := params.ChangedAt.Add(time.Hour)
	params.MergedAt = &mergedAt
	params.ClosedAt = &mergedAt
	params.State = "MERGED"
	params.ChangedAt = mergedAt

	return params
}
