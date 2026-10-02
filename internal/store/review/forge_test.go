package review

import (
	"testing"
	"time"

	agentstore "komrad/internal/store/agent"
	pullrequeststore "komrad/internal/store/pullrequest"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReleaseForgeReviews covers releasing the agent's held forge reviews and nothing else.
func TestReleaseForgeReviews(t *testing.T) {
	st := openFixture(t)
	ctx := t.Context()
	agentID := st.seedAgent(t, "worker")
	otherID := st.seedAgent(t, "other")
	round := st.openRound(t, agentID, "tree-one")
	otherRound := st.openRound(t, otherID, "tree-one")

	held := st.openForgeReview(t, round, "mallory", false)
	sent := st.openForgeReview(t, round, "alice", true)
	elsewhere := st.openForgeReview(t, otherRound, "mallory", false)
	person := st.openReview(t, round)

	released, err := st.ReleaseForgeReviews(ctx, agentID)
	require.NoError(t, err)
	assert.Equal(t, []string{held.ID}, xslice.Map(released, func(review Review) string { return review.ID }))

	for _, tt := range []struct {
		name     string
		review   *Review
		released bool
	}{
		{name: "a held forge review is released", review: held, released: true},
		{name: "a forge review already sent stays as it was", review: sent, released: true},
		{name: "another agent's held forge review stays held", review: elsewhere, released: false},
		{name: "the person's open review stays open", review: person, released: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := st.GetReview(ctx, tt.review.ID)
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, tt.released, got.ReleasedAt != nil)
		})
	}
}

// TestReleaseForgeReviewsInThread covers releasing the held forge reviews that wrote in one thread
// and leaving the agent's others held.
func TestReleaseForgeReviewsInThread(t *testing.T) {
	st := openFixture(t)
	ctx := t.Context()
	agentID := st.seedAgent(t, "worker")
	round := st.openRound(t, agentID, "tree-one")

	opener := st.openForgeReview(t, round, "mallory", false)
	replier := st.openForgeReview(t, round, "eve", false)
	elsewhere := st.openForgeReview(t, round, "trudy", false)
	thread := st.openThread(t, opener, "use a guard")
	st.openThread(t, elsewhere, "drop the guard")
	_, err := st.CreateMessage(ctx, CreateMessageParams{
		ThreadID:   thread.ID,
		ReviewID:   &replier.ID,
		AuthorKind: AuthorForge,
		Body:       "agreed",
	})
	require.NoError(t, err)

	released, err := st.ReleaseForgeReviewsInThread(ctx, thread.ID)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{opener.ID, replier.ID}, xslice.Map(released, func(review Review) string { return review.ID }))

	got, err := st.GetReview(ctx, elsewhere.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Nil(t, got.ReleasedAt)
}

// TestListForgeMirrorTargets covers the agents a pull request's forge threads are mirrored onto.
func TestListForgeMirrorTargets(t *testing.T) {
	tests := []struct {
		name  string
		seed  func(t *testing.T, st *fixture, agentID string)
		check func(t *testing.T, targets []ForgeMirrorTarget, agentID string)
	}{
		{
			name: "an agent on the pull request is mirrored onto at its newest round",
			seed: func(t *testing.T, st *fixture, agentID string) {
				st.openRound(t, agentID, "tree-one")
				st.openRound(t, agentID, "tree-two")
			},
			check: func(t *testing.T, targets []ForgeMirrorTarget, agentID string) {
				require.Len(t, targets, 1)
				assert.Equal(t, agentID, targets[0].AgentID)
				assert.Equal(t, "snapshot-tree-two", targets[0].SnapshotSHA)
				assert.Equal(t, "base-tree-two", targets[0].BaseSHA)
			},
		},
		{
			name: "an agent with no round has nothing to mirror onto",
			seed: func(*testing.T, *fixture, string) {},
			check: func(t *testing.T, targets []ForgeMirrorTarget, _ string) {
				assert.Empty(t, targets)
			},
		},
		{
			name: "an archived agent is left out",
			seed: func(t *testing.T, st *fixture, agentID string) {
				st.openRound(t, agentID, "tree-one")
				_, err := st.agents.Archive(t.Context(), []string{agentID})
				require.NoError(t, err)
			},
			check: func(t *testing.T, targets []ForgeMirrorTarget, _ string) {
				assert.Empty(t, targets)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := openFixture(t)
			agentID := st.seedAgent(t, "worker")
			pullRequestID := st.seedPullRequest(t, agentID)
			tt.seed(t, st, agentID)

			targets, err := st.ListForgeMirrorTargets(t.Context(), pullRequestID)
			require.NoError(t, err)
			tt.check(t, targets, agentID)
		})
	}
}

func (st *fixture) openForgeReview(t *testing.T, round *Round, login string, released bool) *Review {
	t.Helper()

	review, err := st.OpenForgeReview(t.Context(), OpenForgeReviewParams{
		AgentID:  round.AgentID,
		RoundID:  round.ID,
		Login:    login,
		Released: released,
	})
	require.NoError(t, err)
	require.NotNil(t, review)

	return review
}

// seedPullRequest opens a pull request from the agent's branch and returns its id.
func (st *fixture) seedPullRequest(t *testing.T, agentID string) string {
	t.Helper()
	ctx := t.Context()

	repository, branch := "https://github.com/harrisoncramer/komrad", "agent/worker"
	require.NoError(t, st.agents.SetCheckout(ctx, agentID, agentstore.SetCheckoutParams{
		RepositoryURL: &repository,
		Branch:        &branch,
	}))
	require.NoError(t, pullrequeststore.NewPullRequestSyncStore(st.pool).EnsureSync(ctx, repository))

	pullRequests := pullrequeststore.NewPullRequestStore(st.pool)
	opened := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	_, err := pullRequests.Upsert(ctx, pullrequeststore.UpsertParams{
		Host:              "github.com",
		Forge:             "github",
		ProviderID:        "PR_1",
		URL:               repository + "/pull/1",
		RepositoryURL:     repository,
		HeadRepositoryURL: repository,
		Number:            1,
		HeadBranch:        branch,
		BaseBranch:        "main",
		State:             "OPEN",
		OpenedAt:          opened,
		ChangedAt:         opened,
	})
	require.NoError(t, err)
	_, err = pullRequests.Observe(ctx)
	require.NoError(t, err)

	pullRequest, err := pullRequests.GetByIdentity(ctx, "github.com", "PR_1")
	require.NoError(t, err)
	require.NotNil(t, pullRequest)

	return pullRequest.ID
}
