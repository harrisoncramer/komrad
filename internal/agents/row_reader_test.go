package agents

import (
	"context"
	"testing"
	"time"

	"komrad/internal/forge"
	"komrad/internal/storetest"
	"komrad/lib/xptr"
	"komrad/lib/xslice"

	agentstore "komrad/internal/store/agent"
	mailstore "komrad/internal/store/mail"
	markstore "komrad/internal/store/mark"
	pullrequeststore "komrad/internal/store/pullrequest"
	reviewstore "komrad/internal/store/review"
	rowstore "komrad/internal/store/row"
	workflowstore "komrad/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const readerRepository = "https://github.com/harrisoncramer/komrad"

func newRowReader(st *storetest.Stores) *RowReader {
	reviews := reviewstore.NewReviewStore(st.Pool.GetDB())

	return NewRowReader(NewRowReaderParams{
		Agents:       st.Agents,
		Rows:         st.Rows,
		Marks:        st.Marks,
		Mail:         st.Mail,
		PullRequests: st.PullRequests,
		Repositories: st.PullRequestSyncs,
		Reviews:      reviews,
		Workflows:    st.Workflows,
	})
}

// TestListSurroundings pins which watched rows a page of agents is drawn beside.
func TestListSurroundings(t *testing.T) {
	st := storetest.Open(t)
	reader := newRowReader(st)
	ctx := t.Context()

	opened := seedAgentOnBranch(t, st, "opened", readerRepository, "agent/x")
	elsewhere := seedAgentOnBranch(t, st, "elsewhere", "https://github.com/harrisoncramer/unread", "agent/x")
	bystander := st.SeedWorkflow(t, workflowstore.Config{Name: "bystander"})

	_, err := st.Marks.Set(ctx, "a", markstore.OnAgent(elsewhere.ID))
	require.NoError(t, err)
	_, err = st.Mail.Send(ctx, mailstore.SendParams{
		FromName:  "beta",
		ToAgentID: elsewhere.ID,
		Body:      "refunded_cents",
		Deadline:  xptr.Ptr(time.Hour),
	})
	require.NoError(t, err)

	require.NoError(t, st.PullRequestSyncs.EnsureSync(ctx, readerRepository))
	openedAt := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	written, err := st.PullRequests.Upsert(ctx, pullrequeststore.UpsertParams{
		Host:              "github.com",
		Forge:             "github",
		ProviderID:        "PR_740",
		URL:               readerRepository + "/pull/740",
		RepositoryURL:     readerRepository,
		HeadRepositoryURL: readerRepository,
		Number:            740,
		HeadBranch:        "agent/x",
		BaseBranch:        "main",
		State:             "open",
		OpenedAt:          openedAt,
		ChangedAt:         openedAt,
	})
	require.NoError(t, err)
	require.NoError(t, st.PullRequests.SetStanding(ctx, written.ID, forge.Standing{Decision: forge.ReviewDecisionApproved}, openedAt))
	_, err = st.PullRequests.Observe(ctx)
	require.NoError(t, err)

	reviewed := seedReview(t, st, opened.ID)
	seedReview(t, st, elsewhere.ID)

	rows, err := reader.ListRowsByID(ctx, []string{opened.ID})
	require.NoError(t, err)

	surroundings, err := reader.ListSurroundings(ctx, rows)
	require.NoError(t, err)

	assert.Len(t, surroundings.Marks, 1, "every mark, whichever row it is on")
	assert.Len(t, surroundings.Mail, 1, "every unclaimed piece of mail, whoever it is to")
	assert.Equal(t, []string{written.ID}, xslice.Map(surroundings.PullRequests, func(p pullrequeststore.PullRequest) string {
		return p.ID
	}), "only the pull requests the agents on the page were seen with")
	assert.Equal(t, []string{readerRepository}, xslice.Map(surroundings.Repositories, func(s pullrequeststore.Sync) string {
		return s.RepositoryURL
	}), "only the repositories the agents on the page work in")
	assert.Equal(t, []string{opened.WorkflowID}, xslice.Map(surroundings.Workflows, func(w workflowstore.Workflow) string {
		return w.ID
	}), "only the workflows the agents on the page are in, never %s", bystander.ID)
	assert.Equal(t, []string{reviewed}, xslice.Map(surroundings.Reviews, func(r reviewstore.Review) string {
		return r.ID
	}), "only the reviews of the agents on the page")

	nothing, err := reader.ListSurroundings(ctx, []rowstore.Row{})
	require.NoError(t, err)
	assert.Empty(t, nothing.PullRequests, "a page of nobody reads no pull request")
	assert.Empty(t, nothing.Workflows)
}

// TestListRowsByID pins that a listing reads only the agents it names.
func TestListRowsByID(t *testing.T) {
	st := storetest.Open(t)
	reader := newRowReader(st)
	alpha := st.SeedAgent(t, agentstore.Agent{Name: "alpha"})
	st.SeedAgent(t, agentstore.Agent{Name: "beta"})

	tests := []struct {
		name    string
		ids     []string
		wantIDs []string
	}{
		{
			name:    "naming nobody reads nobody",
			wantIDs: []string{},
		},
		{
			name:    "naming one agent reads that one",
			ids:     []string{alpha.ID},
			wantIDs: []string{alpha.ID},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := reader.ListRowsByID(t.Context(), tt.ids)
			require.NoError(t, err)
			assert.Equal(t, tt.wantIDs, xslice.Map(rows, func(row rowstore.Row) string {
				return row.ID
			}))
		})
	}
}

// seedReview opens a review of the agent's first round and returns its id.
func seedReview(t *testing.T, st *storetest.Stores, agentID string) string {
	t.Helper()

	reviews := reviewstore.NewReviewStore(st.Pool.GetDB())
	round, err := reviews.CreateRound(t.Context(), reviewstore.CreateRoundParams{
		AgentID:     agentID,
		BaseSHA:     "base",
		SnapshotSHA: "snapshot",
	})
	require.NoError(t, err)

	review, err := reviews.OpenReview(t.Context(), reviewstore.OpenReviewParams{
		AgentID:    agentID,
		RoundID:    round.ID,
		AuthorKind: reviewstore.AuthorHuman,
	})
	require.NoError(t, err)

	return review.ID
}

// seedAgentOnBranch is a live agent whose checkout is on a branch of a repository.
func seedAgentOnBranch(t *testing.T, st *storetest.Stores, name, repositoryURL, branch string) *agentstore.Agent {
	t.Helper()

	agent := st.SeedAgent(t, agentstore.Agent{Name: name})
	require.NoError(t, st.Agents.SetCheckout(t.Context(), agent.ID, agentstore.SetCheckoutParams{
		RepositoryURL: &repositoryURL,
		Branch:        &branch,
	}))

	return agent
}

// TestListWatchedByKeyReadsNothingWithoutKeys pins that an empty key list never falls through to the snapshot.
func TestListWatchedByKeyReadsNothingWithoutKeys(t *testing.T) {
	tests := []struct {
		name       string
		keys       []string
		wantCalled bool
		want       []string
	}{
		{
			name:       "nil keys never read the snapshot",
			keys:       nil,
			wantCalled: false,
			want:       nil,
		},
		{
			name:       "an empty key list never reads the snapshot",
			keys:       []string{},
			wantCalled: false,
			want:       nil,
		},
		{
			name:       "keys are read by key",
			keys:       []string{"a", "b"},
			wantCalled: true,
			want:       []string{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lister := &keyEchoingLister{}

			got, err := listWatchedByKey(t.Context(), lister, tt.keys)
			require.NoError(t, err)

			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantCalled, lister.called)
		})
	}
}

// keyEchoingLister answers a read with the keys it was given.
type keyEchoingLister struct {
	called bool
}

func (l *keyEchoingLister) ListWatched(_ context.Context, keys []string) ([]string, error) {
	l.called = true

	return keys, nil
}
