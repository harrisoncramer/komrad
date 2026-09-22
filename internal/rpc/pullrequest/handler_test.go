package pullrequest

import (
	"testing"
	"time"

	"weave/internal/forge"
	"weave/internal/git"
	agentstore "weave/internal/store/agent"
	pullrequeststore "weave/internal/store/pullrequest"
	"weave/internal/storetest"
	"weave/lib/xptr"
	"weave/lib/xslice"
	paginationv1 "weave/proto/gen/pagination/v1"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"
	"weave/proto/gen/pullrequest/v1/pullrequestv1weave"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListPullRequests covers the stored listing: the repository named as a remote or as its
// canonical address, the branch narrowing it, the unfinished ones first and each group newest
// opened first, the state decided per row, and a page token walking the rest.
func TestListPullRequests(t *testing.T) {
	t.Parallel()

	st := storetest.Open(t)
	ctx := t.Context()
	service := pullrequestv1weave.PullRequestServiceAdapter{Impl: NewHandler(NewHandlerParams{
		Agents:           st.Agents,
		PullRequests:     st.PullRequests,
		PullRequestSyncs: st.PullRequestSyncs,
	})}

	opened := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	mergedAt := opened.Add(3 * time.Hour)
	elsewhere := storedPullRequest("PR_4", 4, opened.Add(3*time.Hour), nil)
	elsewhere.HeadBranch = "agent/y"
	for _, params := range []pullrequeststore.UpsertParams{
		storedPullRequest("PR_1", 1, opened, nil),
		storedPullRequest("PR_2", 2, opened.Add(time.Hour), nil),
		storedPullRequest("PR_3", 3, opened.Add(2*time.Hour), &mergedAt),
		elsewhere,
	} {
		_, err := st.PullRequests.Upsert(ctx, params)
		require.NoError(t, err)
	}
	approved, err := st.PullRequests.GetByIdentity(ctx, "github.com", "PR_2")
	require.NoError(t, err)
	require.NoError(t, st.PullRequests.SetStanding(ctx, approved.ID, forge.Standing{Decision: forge.ReviewDecisionApproved, Mergeability: forge.MergeabilityClean}, time.Now()))

	numbers := func(pullRequests []*pullrequestv1.PullRequest) []int32 {
		return xslice.Map(pullRequests, func(pullRequest *pullrequestv1.PullRequest) int32 { return pullRequest.GetNumber() })
	}

	onBranch, err := service.ListPullRequests(ctx, connect.NewRequest(&pullrequestv1.ListPullRequestsRequest{
		RepositoryUrl: "git@github.com:harrisoncramer/weave.git",
		HeadBranch:    "agent/x",
	}))
	require.NoError(t, err)
	assert.Equal(t, []int32{2, 1, 3}, numbers(onBranch.Msg.GetPullRequests()), "the unfinished first, newest opened first, then the merged")
	assert.Nil(t, onBranch.Msg.NextPageToken)
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED, onBranch.Msg.GetPullRequests()[0].GetState())
	assert.Equal(t, pullrequestv1.ReviewDecision_REVIEW_DECISION_APPROVED, onBranch.Msg.GetPullRequests()[0].GetReviewDecision())
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED, onBranch.Msg.GetPullRequests()[2].GetState())
	assert.Equal(t, approved.ID, onBranch.Msg.GetPullRequests()[0].GetId())

	var walked []int32
	var cursor *string
	for {
		page, err := service.ListPullRequests(ctx, connect.NewRequest(&pullrequestv1.ListPullRequestsRequest{
			RepositoryUrl: weaveRepository,
			Pagination:    &paginationv1.Pagination{Limit: xptr.Ptr(int32(2)), Cursor: cursor},
		}))
		require.NoError(t, err)
		walked = append(walked, numbers(page.Msg.GetPullRequests())...)
		if page.Msg.NextPageToken == nil {
			break
		}
		cursor = page.Msg.NextPageToken
	}
	assert.Equal(t, []int32{4, 2, 1, 3}, walked, "the whole repository, two to a page, in one order")
}

// TestListPullRequestsRefusals covers a repository that names no forge and a cursor nobody
// could have been handed.
func TestListPullRequestsRefusals(t *testing.T) {
	t.Parallel()

	st := storetest.Open(t)
	service := NewHandler(NewHandlerParams{Agents: st.Agents, PullRequests: st.PullRequests, PullRequestSyncs: st.PullRequestSyncs})

	_, failure := service.ListPullRequests(t.Context(), &pullrequestv1.ListPullRequestsRequest{RepositoryUrl: "/local/path"})
	require.NotNil(t, failure)
	require.ErrorIs(t, failure, git.ErrNotForgeRemote)

	_, failure = service.ListPullRequests(t.Context(), &pullrequestv1.ListPullRequestsRequest{
		RepositoryUrl: weaveRepository,
		Pagination:    &paginationv1.Pagination{Cursor: xptr.Ptr("not-a-token")},
	})
	require.NotNil(t, failure)
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(failure))
}

// TestListAgentPullRequests covers the stored listing: an agent nobody holds, one whose
// checkout is not on a forge repository, one whose repository has not been read, and one whose
// branch holds several, newest opened first with the current one marked and each state decided.
func TestListAgentPullRequests(t *testing.T) {
	t.Parallel()

	opened := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		seed        func(t *testing.T, st *storetest.Stores) string
		wantCode    connect.Code
		wantRead    bool
		wantNumbers []int
		wantCurrent int
		wantStates  []pullrequestv1.PullRequestState
	}{
		{
			name: "an id nobody holds",
			seed: func(*testing.T, *storetest.Stores) string {
				return "00000000-0000-0000-0000-000000000000"
			},
			wantCode: connect.CodeNotFound,
		},
		{
			name: "a checkout that is not on a forge repository lists nothing",
			seed: func(t *testing.T, st *storetest.Stores) string {
				t.Helper()

				return st.SeedAgent(t, agentstore.Agent{Name: "local"}).ID
			},
		},
		{
			name: "a repository the sync has not read lists nothing and no read stamp",
			seed: func(t *testing.T, st *storetest.Stores) string {
				t.Helper()

				return seedAgentOn(t, st, "unread", "agent/x")
			},
		},
		{
			name: "a repository asked about but never swept has no read stamp",
			seed: func(t *testing.T, st *storetest.Stores) string {
				t.Helper()
				ctx := t.Context()

				id := seedAgentOn(t, st, "missing", "agent/missing")
				require.NoError(t, st.PullRequestSyncs.EnsureSync(ctx, weaveRepository))
				require.NoError(t, st.PullRequestSyncs.RecordSyncRequest(ctx, weaveRepository))

				return id
			},
		},
		{
			name: "the branch's pull requests, newest opened first, with the current one marked",
			seed: func(t *testing.T, st *storetest.Stores) string {
				t.Helper()
				ctx := t.Context()

				id := seedAgentOn(t, st, "on-branch", "agent/x")
				require.NoError(t, st.PullRequestSyncs.EnsureSync(ctx, weaveRepository))
				require.NoError(t, st.PullRequestSyncs.RecordSyncRequest(ctx, weaveRepository))
				require.NoError(t, st.PullRequestSyncs.StartSweep(ctx, pullrequeststore.StartSweepParams{
					RepositoryURL: weaveRepository,
					Newest:        opened,
					ListedAt:      opened,
				}))
				require.NoError(t, st.PullRequestSyncs.CompleteSweep(ctx, weaveRepository))

				mergedAt := opened.Add(2 * time.Hour)
				for _, params := range []pullrequeststore.UpsertParams{
					storedPullRequest("PR_1", 1, opened, nil),
					storedPullRequest("PR_2", 2, opened.Add(time.Hour), nil),
					storedPullRequest("PR_3", 3, opened.Add(2*time.Hour), &mergedAt),
				} {
					_, err := st.PullRequests.Upsert(ctx, params)
					require.NoError(t, err)
				}
				approved, err := st.PullRequests.GetByIdentity(ctx, "github.com", "PR_2")
				require.NoError(t, err)
				require.NoError(t, st.PullRequests.SetStanding(ctx, approved.ID, forge.Standing{Decision: forge.ReviewDecisionApproved, Mergeability: forge.MergeabilityClean}, time.Now()))

				return id
			},
			wantRead:    true,
			wantNumbers: []int{3, 2, 1},
			wantCurrent: 2,
			wantStates: []pullrequestv1.PullRequestState{
				pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
				pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED,
				pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := storetest.Open(t)
			service := pullrequestv1weave.PullRequestServiceAdapter{Impl: NewHandler(NewHandlerParams{
				Agents:           st.Agents,
				PullRequests:     st.PullRequests,
				PullRequestSyncs: st.PullRequestSyncs,
			})}
			id := tt.seed(t, st)

			resp, err := service.ListAgentPullRequests(t.Context(), connect.NewRequest(&pullrequestv1.ListAgentPullRequestsRequest{AgentId: id}))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantRead, resp.Msg.RepositoryReadAt != nil)

			var numbers []int
			var states []pullrequestv1.PullRequestState
			current := 0
			for _, pullRequest := range resp.Msg.GetPullRequests() {
				numbers = append(numbers, int(pullRequest.GetNumber()))
				states = append(states, pullRequest.GetState())
				if pullRequest.GetCurrent() {
					current = int(pullRequest.GetNumber())
				}
			}
			assert.Equal(t, tt.wantNumbers, numbers)
			assert.Equal(t, tt.wantStates, states)
			assert.Equal(t, tt.wantCurrent, current, "the newest unfinished is current over a newer merged one")
		})
	}
}

// TestListAgentPullRequestsFallsBackToTheNewestFinished covers a branch holding only finished
// pull requests, where the newest opened of them is current.
func TestListAgentPullRequestsFallsBackToTheNewestFinished(t *testing.T) {
	t.Parallel()

	st := storetest.Open(t)
	ctx := t.Context()
	service := pullrequestv1weave.PullRequestServiceAdapter{Impl: NewHandler(NewHandlerParams{
		Agents:           st.Agents,
		PullRequests:     st.PullRequests,
		PullRequestSyncs: st.PullRequestSyncs,
	})}
	id := seedAgentOn(t, st, "finished", "agent/x")

	opened := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	closedAt := opened.Add(3 * time.Hour)
	for _, params := range []pullrequeststore.UpsertParams{
		storedPullRequest("PR_1", 1, opened, &closedAt),
		storedPullRequest("PR_2", 2, opened.Add(time.Hour), &closedAt),
	} {
		_, err := st.PullRequests.Upsert(ctx, params)
		require.NoError(t, err)
	}

	resp, err := service.ListAgentPullRequests(ctx, connect.NewRequest(&pullrequestv1.ListAgentPullRequestsRequest{AgentId: id}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.GetPullRequests(), 2)
	assert.True(t, resp.Msg.GetPullRequests()[0].GetCurrent())
	assert.Equal(t, int32(2), resp.Msg.GetPullRequests()[0].GetNumber())
	assert.False(t, resp.Msg.GetPullRequests()[1].GetCurrent())
	assert.Nil(t, resp.Msg.RepositoryReadAt)
}

const weaveRepository = "https://github.com/harrisoncramer/weave"

// seedAgentOn is a live agent on a branch of the weave repository.
func seedAgentOn(t *testing.T, st *storetest.Stores, name, branch string) string {
	t.Helper()
	ctx := t.Context()

	id := st.SeedAgent(t, agentstore.Agent{Name: name}).ID
	repository := weaveRepository
	require.NoError(t, st.Agents.SetCheckout(ctx, id, agentstore.SetCheckoutParams{RepositoryURL: &repository, Branch: &branch}))

	return id
}

// storedPullRequest is a pull request from agent/x of the weave repository, merged when a
// stamp is given.
func storedPullRequest(providerID string, number int, opened time.Time, mergedAt *time.Time) pullrequeststore.UpsertParams {
	state := "open"
	if mergedAt != nil {
		state = "closed"
	}

	return pullrequeststore.UpsertParams{
		Host:              "github.com",
		Forge:             forge.KindGitHub,
		ProviderID:        providerID,
		URL:               "https://github.com/harrisoncramer/weave/pull/" + providerID,
		RepositoryURL:     weaveRepository,
		HeadRepositoryURL: weaveRepository,
		Number:            number,
		Title:             providerID,
		HeadBranch:        "agent/x",
		HeadSHA:           "abc",
		BaseBranch:        "main",
		BaseSHA:           "def",
		State:             state,
		OpenedAt:          opened,
		ChangedAt:         opened,
		MergedAt:          mergedAt,
		ClosedAt:          mergedAt,
	}
}
