package row_test

import (
	"testing"
	"time"

	"weave/internal/store"
	"weave/internal/storetest"
	"weave/lib/xptr"
	"weave/lib/xslice"

	agentstore "weave/internal/store/agent"
	pullrequeststore "weave/internal/store/pullrequest"
	reviewstore "weave/internal/store/review"
	rowstore "weave/internal/store/row"
	runstore "weave/internal/store/run"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fixture = "https://github.com/harrisoncramer/weave-forge-fixture"

// rowOf reads one agent's row back.
func rowOf(t *testing.T, st *storetest.Stores, id string) rowstore.Row {
	t.Helper()

	row, err := st.Rows.Get(t.Context(), id)
	require.NoError(t, err)

	return *row
}

// TestRowCarriesTheAgent pins that the row is the agent as stored, metadata included, with
// nothing beside it for an agent nothing has happened to.
func TestRowCarriesTheAgent(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()
	alpha := st.SeedAgent(t, agentstore.Agent{
		Name:   "alpha",
		Cwd:    "/repo",
		Prompt: "go",
	})
	_, err := st.Agents.SetMetadata(ctx, agentstore.SetMetadataParams{
		ID:    alpha.ID,
		Key:   "branch",
		Value: xptr.Ptr("agent/x"),
	})
	require.NoError(t, err)

	row := rowOf(t, st, alpha.ID)

	assert.Equal(t, "alpha", row.Name)
	assert.Equal(t, "/repo", row.Cwd)
	assert.Equal(t, "go", row.Prompt)
	assert.Equal(t, []agentstore.Metadata{{Key: "branch", Value: "agent/x"}}, row.Metadata)
	assert.False(t, row.FailedRun)
	assert.Nil(t, row.FailedRunLog)
	assert.Zero(t, row.LocalRounds)
	assert.Nil(t, row.LocalVerdict)
	assert.Empty(t, row.LocalReviewSHA)
	assert.False(t, row.RepositoryRead)
}

// TestFailedRun pins the mark a failed script puts on a row: it names the run that failed last,
// a run that succeeded is nothing to read, and reading clears it.
func TestFailedRun(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()
	alpha := st.SeedAgent(t, agentstore.Agent{Name: "alpha"})

	fine, err := st.Runs.Begin(ctx, runstore.BeginParams{
		AgentID: alpha.ID,
		Trigger: "post-launch",
		Script:  "setup",
	})
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

// TestLocalReview pins the loop the row draws: released rounds are counted against the agent
// whose work went round, the last conclusion stands with the round it was given on, and a
// comment concludes nothing.
func TestLocalReview(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()
	reviews := reviewstore.NewReviewStore(st.Pool.GetDB())
	agent := st.SeedAgent(t, agentstore.Agent{Name: "worker"})
	reviewer := st.SeedAgent(t, agentstore.Agent{Name: "reader", WorkflowID: agent.WorkflowID})

	release := func(tree string, kind reviewstore.AuthorKind, verdict reviewstore.Verdict) {
		round, err := reviews.CreateRound(ctx, reviewstore.CreateRoundParams{
			AgentID:     agent.ID,
			BaseSHA:     "base-" + tree,
			SnapshotSHA: "snapshot-" + tree,
		})
		require.NoError(t, err)

		params := reviewstore.OpenReviewParams{
			AgentID:    agent.ID,
			RoundID:    round.ID,
			AuthorKind: kind,
		}
		if kind == reviewstore.AuthorAgent {
			params.AuthorAgentID = &reviewer.ID
		}
		review, err := reviews.OpenReview(ctx, params)
		require.NoError(t, err)
		_, err = reviews.ReleaseReview(ctx, reviewstore.ReleaseReviewParams{
			ID:      review.ID,
			Verdict: verdict,
			Path:    "/tmp/" + tree + ".md",
		})
		require.NoError(t, err)
	}

	release("one", reviewstore.AuthorAgent, reviewstore.VerdictChangesRequested)
	release("two", reviewstore.AuthorAgent, reviewstore.VerdictApproved)
	release("three", reviewstore.AuthorHuman, reviewstore.VerdictCommented)

	row := rowOf(t, st, agent.ID)
	assert.Equal(t, int32(3), row.LocalRounds)
	assert.Equal(t, xptr.Ptr(reviewstore.VerdictApproved), row.LocalVerdict)
	assert.Equal(t, "snapshot-two", row.LocalReviewSHA, "the comment on the third round leaves the approval and its round standing")

	assert.Zero(t, rowOf(t, st, reviewer.ID).LocalRounds, "the reviewer holds no diff of its own to have gone round")
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
	require.NoError(t, st.PullRequestSyncs.StartSweep(ctx, pullrequeststore.StartSweepParams{
		RepositoryURL: fixture,
		Newest:        listed,
		ListedAt:      listed,
	}))
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

// TestListByKey pins the watched set: the live rows among the given ids answer, an archived one,
// one a delete has claimed and an id nothing holds are left out, and one that could not be an
// id at all does not fail the read.
func TestListByKey(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()
	first := st.SeedAgent(t, agentstore.Agent{Name: "first"})
	second := st.SeedAgent(t, agentstore.Agent{Name: "second"})
	archived := st.SeedAgent(t, agentstore.Agent{Name: "archived"})
	deleting := st.SeedAgent(t, agentstore.Agent{Name: "deleting"})
	_, err := st.Agents.Archive(ctx, []string{archived.ID})
	require.NoError(t, err)
	require.NoError(t, st.Agents.MarkDeleting(ctx, []string{deleting.ID}))

	rows, err := st.Rows.ListByKey(ctx, []string{first.ID, second.ID, archived.ID, deleting.ID, "00000000-0000-0000-0000-000000000000", "not-an-id"})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, second.ID, rows[0].ID)
	assert.Equal(t, first.ID, rows[1].ID)

	rows, err = st.Rows.ListByKey(ctx, []string{"not-an-id"})
	require.NoError(t, err)
	assert.Empty(t, rows)

	live, err := st.Rows.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{second.ID, first.ID}, xslice.Map(live, func(row rowstore.Row) string { return row.ID }), "the whole watched set reads the same way")
}

// TestGet pins the one-row read: an archived row answers, and an id nothing holds, or that
// could not be an id at all, is not found.
func TestGet(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()
	archived := st.SeedAgent(t, agentstore.Agent{Name: "archived"})
	_, err := st.Agents.Archive(ctx, []string{archived.ID})
	require.NoError(t, err)

	row, err := st.Rows.Get(ctx, archived.ID)
	require.NoError(t, err)
	assert.Equal(t, "archived", row.Name)
	assert.NotNil(t, row.ArchivedAt)

	_, err = st.Rows.Get(ctx, "00000000-0000-0000-0000-000000000000")
	require.ErrorIs(t, err, store.ErrNotFound)

	_, err = st.Rows.Get(ctx, "not-an-id")
	require.ErrorIs(t, err, store.ErrNotFound)
}
