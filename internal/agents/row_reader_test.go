package agents

import (
	"context"
	"testing"
	"time"

	"komrad/internal/forge"
	agentstore "komrad/internal/store/agent"
	mailstore "komrad/internal/store/mail"
	pullrequeststore "komrad/internal/store/pullrequest"
	reviewstore "komrad/internal/store/review"
	rowstore "komrad/internal/store/row"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/storetest"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	forgev1 "komrad/proto/gen/forge/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const readerRepository = "https://github.com/harrisoncramer/komrad"

func newRowReader(st *storetest.Stores) *RowReader {
	reviews := reviewstore.NewReviewStore(st.Pool.GetDB())

	return NewRowReader(NewRowReaderParams{
		Agents:        st.Agents,
		Rows:          st.Rows,
		Mail:          st.Mail,
		PullRequests:  st.PullRequests,
		Repositories:  st.PullRequestSyncs,
		Reviews:       reviews,
		ReviewThreads: reviewstore.NewThreadStore(st.Pool.GetDB()),
		Workflows:     st.Workflows,
		Tasks:         st.Tasks,
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

	_, err := st.Mail.Send(ctx, mailstore.SendParams{
		FromName:  "beta",
		ToAgentID: elsewhere.ID,
		Body:      "refunded_cents",
		Deadline:  time.Hour,
	})
	require.NoError(t, err)

	require.NoError(t, st.PullRequestSyncs.EnsureSync(ctx, readerRepository))
	openedAt := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	written, err := st.PullRequests.Upsert(ctx, pullrequeststore.UpsertParams{
		Host:              "github.com",
		Forge:             forgev1.Kind_KIND_GITHUB,
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
	_, err = st.PullRequests.SetMergeReadiness(ctx, pullrequeststore.SetMergeReadinessParams{
		ID:             written.ID,
		MergeReadiness: forge.MergeReadiness{Decision: pullrequestv1.ReviewDecision_REVIEW_DECISION_APPROVED},
		ReadAt:         openedAt,
	})
	require.NoError(t, err)
	_, err = st.PullRequests.Observe(ctx)
	require.NoError(t, err)

	reviewed := seedReview(t, st, opened.ID)
	threaded := seedThread(t, st, reviewed)
	seedThread(t, st, seedReview(t, st, elsewhere.ID))

	rows, err := reader.ListRowsByID(ctx, []uuid.UUID{opened.ID})
	require.NoError(t, err)

	surroundings, err := reader.ListSurroundings(ctx, rows)
	require.NoError(t, err)

	assert.Len(t, surroundings.Mail, 1, "every unclaimed piece of mail, whoever it is to")
	assert.Equal(t, []uuid.UUID{written.ID}, xslice.Map(surroundings.PullRequests, func(p pullrequeststore.PullRequest) uuid.UUID {
		return p.ID
	}), "only the pull requests the agents on the page were seen with")
	assert.Equal(t, []string{readerRepository}, xslice.Map(surroundings.Repositories, func(s pullrequeststore.Sync) string {
		return s.RepositoryURL
	}), "only the repositories the agents on the page work in")
	assert.Equal(t, []uuid.UUID{opened.WorkflowID}, xslice.Map(surroundings.Workflows, func(w workflowstore.Workflow) uuid.UUID {
		return w.ID
	}), "only the workflows the agents on the page are in, never %s", bystander.ID)
	assert.Equal(t, []uuid.UUID{reviewed}, xslice.Map(surroundings.Reviews, func(r reviewstore.Review) uuid.UUID {
		return r.ID
	}), "only the reviews of the agents on the page")
	assert.Equal(t, []uuid.UUID{threaded}, xslice.Map(surroundings.ReviewThreads, func(thread reviewstore.ThreadWithMessages) uuid.UUID {
		return thread.ID
	}), "only the review threads on the work of the agents on the page")

	nothing, err := reader.ListSurroundings(ctx, []rowstore.Row{})
	require.NoError(t, err)
	assert.Empty(t, nothing.PullRequests, "a page of nobody reads no pull request")
	assert.Empty(t, nothing.Workflows)
	assert.Empty(t, nothing.ReviewThreads)
}

// TestListRowsByID pins that a listing reads only the agents it names.
func TestListRowsByID(t *testing.T) {
	st := storetest.Open(t)
	reader := newRowReader(st)
	alpha := st.SeedAgent(t, agentstore.Agent{Name: "alpha"})
	st.SeedAgent(t, agentstore.Agent{Name: "beta"})

	tests := []struct {
		name    string
		ids     []uuid.UUID
		wantIDs []uuid.UUID
	}{
		{
			name:    "naming nobody reads nobody",
			wantIDs: []uuid.UUID{},
		},
		{
			name:    "naming one agent reads that one",
			ids:     []uuid.UUID{alpha.ID},
			wantIDs: []uuid.UUID{alpha.ID},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := reader.ListRowsByID(t.Context(), tt.ids)
			require.NoError(t, err)
			assert.Equal(t, tt.wantIDs, xslice.Map(rows, func(row rowstore.Row) uuid.UUID {
				return row.ID
			}))
		})
	}
}

// seedReview opens a review of the first snapshot of the agent's task and returns its id.
func seedReview(t *testing.T, st *storetest.Stores, agentID uuid.UUID) uuid.UUID {
	t.Helper()

	reviews := reviewstore.NewReviewStore(st.Pool.GetDB())
	agent, err := st.Agents.GetByID(t.Context(), agentID)
	require.NoError(t, err)

	snapshot, err := reviews.CreateSnapshot(t.Context(), reviewstore.CreateSnapshotParams{
		TaskID:  *agent.TaskID,
		BaseSHA: "base",
		SHA:     "snapshot",
	})
	require.NoError(t, err)

	review, err := reviews.OpenReview(t.Context(), reviewstore.OpenReviewParams{
		TaskID:     snapshot.TaskID,
		SnapshotID: snapshot.ID,
		AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
	})
	require.NoError(t, err)

	return review.ID
}

func seedThread(t *testing.T, st *storetest.Stores, reviewID uuid.UUID) uuid.UUID {
	t.Helper()

	reviews := reviewstore.NewReviewStore(st.Pool.GetDB())
	review, err := reviews.GetReview(t.Context(), reviewID)
	require.NoError(t, err)

	thread, err := reviews.CreateThread(t.Context(), reviewstore.CreateThreadParams{
		TaskID:      review.TaskID,
		ReviewID:    &review.ID,
		SnapshotID:  review.SnapshotID,
		BaseSHA:     "base",
		FilePath:    "main.go",
		NewStart:    3,
		NewEnd:      3,
		QuotedLines: []string{"func rename() {}"},
		Body:        "rename this",
		AuthorKind:  agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
	})
	require.NoError(t, err)

	return thread.ID
}

// seedAgentOnBranch is a live agent whose checkout is on a branch of a repository.
func seedAgentOnBranch(t *testing.T, st *storetest.Stores, name, repositoryURL, branch string) *agentstore.Agent {
	t.Helper()

	agent := st.SeedAgent(t, agentstore.Agent{Name: name})
	st.SeedTaskHead(t, agent.ID, taskstore.SetHeadParams{
		RepositoryURL: &repositoryURL,
		Branch:        &branch,
	})

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
