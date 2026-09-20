package pullrequest

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"weave/internal/forge"
	"weave/internal/git"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	forgestore "weave/internal/store/forge"
	pullrequeststore "weave/internal/store/pullrequest"
	"weave/internal/storetest"
	"weave/lib/xptr"
	paginationv1 "weave/proto/gen/pagination/v1"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"
	"weave/proto/gen/pullrequest/v1/pullrequestv1weave"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeForge struct {
	pullRequests []forge.PullRequest
	hasMore      bool
	params       forge.ListParams
}

func (f *fakeForge) Kind() string                           { return forge.KindGitHub }
func (f *fakeForge) Verify(context.Context) (string, error) { return "", nil }
func (f *fakeForge) Get(context.Context, string) (forge.PullRequest, error) {
	return forge.PullRequest{}, nil
}

func (f *fakeForge) ListUpdated(context.Context, forge.ListUpdatedParams) (*forge.ListUpdatedResult, error) {
	return &forge.ListUpdatedResult{}, nil
}

func (f *fakeForge) ListChanged(context.Context, forge.ListChangedParams) (*forge.ListChangedResult, error) {
	return &forge.ListChangedResult{}, nil
}

func (f *fakeForge) ReviewDecisions(context.Context, git.RemoteURL, []int) (map[int]forge.ReviewDecision, error) {
	return map[int]forge.ReviewDecision{}, nil
}

func (f *fakeForge) List(_ context.Context, params forge.ListParams) (*forge.ListResult, error) {
	f.params = params

	return &forge.ListResult{
		PullRequests: f.pullRequests,
		HasMore:      f.hasMore,
		Cursor:       "end-of-page",
	}, nil
}

type fakeFinder struct {
	forge *fakeForge
}

func (f *fakeFinder) ConnectionFor(_ context.Context, host string) (*forgestore.Connection, error) {
	if host != "github.com" {
		return nil, store.ErrNotFound.Saying("%s is not connected", host)
	}

	return &forgestore.Connection{Host: host, Kind: forge.KindGitHub}, nil
}

func (f *fakeFinder) ForConnection(forgestore.Connection) (forge.Forge, error) { return f.forge, nil }

// TestListPullRequests covers the remote picking the connection by host, the branch reaching
// the forge, and each pull request mapped through with its repositories canonical.
func TestListPullRequests(t *testing.T) {
	t.Parallel()

	merged := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	upstream := git.RemoteURL{
		Scheme: "https",
		Host:   "github.com",
		Path:   "harrisoncramer/weave",
	}
	fake := &fakeForge{pullRequests: []forge.PullRequest{{
		Forge:      forge.KindGitHub,
		ProviderID: "PR_1",
		URL:        "https://github.com/harrisoncramer/weave/pull/1",
		Repository: upstream,
		Number:     1,
		Title:      "one",
		HeadBranch: "agent/x",
		MergedAt:   &merged,
		ClosedAt:   &merged,
	}}}
	service := pullrequestv1weave.PullRequestServiceAdapter{Impl: NewPullRequestService(NewPullRequestServiceParams{Forges: &fakeFinder{forge: fake}})}

	resp, err := service.ListPullRequests(t.Context(), connect.NewRequest(&pullrequestv1.ListPullRequestsRequest{
		RemoteUrl: "git@github.com:harrisoncramer/weave.git",
		Branch:    "agent/x",
	}))
	require.NoError(t, err)
	assert.Equal(t, "harrisoncramer/weave", fake.params.Repo.Path)
	assert.Equal(t, "agent/x", fake.params.Branch)
	assert.Empty(t, fake.params.Cursor)
	assert.Equal(t, 20, fake.params.PerPage, "twenty to a page when the caller says nothing")
	assert.Nil(t, resp.Msg.NextPageToken, "the only page carries no token")
	require.Len(t, resp.Msg.GetPullRequests(), 1)

	pullRequest := resp.Msg.GetPullRequests()[0]
	assert.Equal(t, "https://github.com/harrisoncramer/weave", pullRequest.GetRepositoryUrl())
	assert.Empty(t, pullRequest.GetHeadRepositoryUrl(), "a head the forge no longer has is empty rather than https:///")
	assert.NotNil(t, pullRequest.MergedAt)
}

// TestListPullRequestsPages covers the token round trip: a page with more behind it hands
// back a token carrying the forge's cursor, and that token asks the forge for the page after
// at the size the first was cut at, whatever limit travels beside it.
func TestListPullRequestsPages(t *testing.T) {
	t.Parallel()

	fake := &fakeForge{hasMore: true}
	service := pullrequestv1weave.PullRequestServiceAdapter{Impl: NewPullRequestService(NewPullRequestServiceParams{Forges: &fakeFinder{forge: fake}})}

	first, err := service.ListPullRequests(t.Context(), connect.NewRequest(&pullrequestv1.ListPullRequestsRequest{
		RemoteUrl:  "git@github.com:harrisoncramer/weave.git",
		Pagination: &paginationv1.Pagination{Limit: xptr.Ptr(int32(50))},
	}))
	require.NoError(t, err)
	require.NotNil(t, first.Msg.NextPageToken)
	assert.Equal(t, 50, fake.params.PerPage)

	fake.hasMore = false
	second, err := service.ListPullRequests(t.Context(), connect.NewRequest(&pullrequestv1.ListPullRequestsRequest{
		RemoteUrl:  "git@github.com:harrisoncramer/weave.git",
		Pagination: &paginationv1.Pagination{Cursor: first.Msg.NextPageToken},
	}))
	require.NoError(t, err)
	assert.Equal(t, "end-of-page", fake.params.Cursor)
	assert.Equal(t, 50, fake.params.PerPage, "the token carries the size, so a dropped limit reads the same window")
	assert.Nil(t, second.Msg.NextPageToken)
}

// TestListPullRequestsRefusesABadToken covers a cursor that is not one and a cursor nobody
// could have been handed.
func TestListPullRequestsRefusesABadToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		cursor string
	}{
		{
			name:   "not a token",
			cursor: "not-a-token",
		},
		{
			name:   "a token with no cursor",
			cursor: base64.URLEncoding.EncodeToString([]byte(`{"per_page":20}`)),
		},
		{
			name:   "a token with no page size",
			cursor: base64.URLEncoding.EncodeToString([]byte(`{"cursor":"Y3Vyc29yOjM="}`)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := pullrequestv1weave.PullRequestServiceAdapter{Impl: NewPullRequestService(NewPullRequestServiceParams{Forges: &fakeFinder{forge: &fakeForge{}}})}

			_, err := service.ListPullRequests(t.Context(), connect.NewRequest(&pullrequestv1.ListPullRequestsRequest{
				RemoteUrl:  "git@github.com:harrisoncramer/weave.git",
				Pagination: &paginationv1.Pagination{Cursor: xptr.Ptr(tt.cursor)},
			}))
			require.Error(t, err)
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		})
	}
}

// TestListPullRequestsRefusals covers a remote that is not one, and a host nothing is
// connected to.
func TestListPullRequestsRefusals(t *testing.T) {
	t.Parallel()

	service := NewPullRequestService(NewPullRequestServiceParams{Forges: &fakeFinder{forge: &fakeForge{}}})

	_, failure := service.ListPullRequests(t.Context(), &pullrequestv1.ListPullRequestsRequest{RemoteUrl: "/local/path", Branch: "main"})
	require.NotNil(t, failure)
	require.ErrorIs(t, failure, git.ErrNotForgeRemote)

	_, failure = service.ListPullRequests(t.Context(), &pullrequestv1.ListPullRequestsRequest{RemoteUrl: "git@gitlab.com:o/r.git", Branch: "main"})
	require.NotNil(t, failure)
	require.ErrorIs(t, failure, store.ErrNotFound)
}

// TestListAgentPullRequests covers the stored listing: an agent nobody holds, one whose
// checkout is not on a forge repository, one whose repository has not been read, and one whose
// branch holds several, newest opened first with the current one marked and each state decided.
func TestListAgentPullRequests(t *testing.T) {
	t.Parallel()

	opened := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		seed        func(t *testing.T, st *storetest.Stores, pullRequests *pullrequeststore.Store) string
		wantCode    connect.Code
		wantRead    bool
		wantNumbers []int
		wantCurrent int
		wantStates  []pullrequestv1.PullRequestState
	}{
		{
			name: "an id nobody holds",
			seed: func(*testing.T, *storetest.Stores, *pullrequeststore.Store) string {
				return "00000000-0000-0000-0000-000000000000"
			},
			wantCode: connect.CodeNotFound,
		},
		{
			name: "a checkout that is not on a forge repository lists nothing",
			seed: func(t *testing.T, st *storetest.Stores, _ *pullrequeststore.Store) string {
				t.Helper()

				return st.SeedAgent(t, agentstore.Agent{Name: "local"}).ID
			},
		},
		{
			name: "a repository the sync has not read lists nothing and no read stamp",
			seed: func(t *testing.T, st *storetest.Stores, _ *pullrequeststore.Store) string {
				t.Helper()

				return seedAgentOn(t, st, "unread", "agent/x")
			},
		},
		{
			name: "the branch's pull requests, newest opened first, with the current one marked",
			seed: func(t *testing.T, st *storetest.Stores, pullRequests *pullrequeststore.Store) string {
				t.Helper()
				ctx := t.Context()

				id := seedAgentOn(t, st, "on-branch", "agent/x")
				require.NoError(t, pullRequests.EnsureSync(ctx, weaveRepository))
				require.NoError(t, pullRequests.RecordSyncRequest(ctx, weaveRepository))

				mergedAt := opened.Add(2 * time.Hour)
				for _, params := range []pullrequeststore.UpsertParams{
					storedPullRequest("PR_1", 1, opened, nil),
					storedPullRequest("PR_2", 2, opened.Add(time.Hour), nil),
					storedPullRequest("PR_3", 3, opened.Add(2*time.Hour), &mergedAt),
				} {
					_, err := pullRequests.Upsert(ctx, params)
					require.NoError(t, err)
				}
				approved, err := pullRequests.GetByIdentity(ctx, "github.com", "PR_2")
				require.NoError(t, err)
				require.NoError(t, pullRequests.SetReviewDecision(ctx, approved.ID, forge.ReviewDecisionApproved))

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
			pullRequests := pullrequeststore.New(st.Pool.GetDB())
			service := pullrequestv1weave.PullRequestServiceAdapter{Impl: NewPullRequestService(NewPullRequestServiceParams{
				Forges:       &fakeFinder{forge: &fakeForge{}},
				Agents:       st.Agents,
				PullRequests: pullRequests,
			})}
			id := tt.seed(t, st, pullRequests)

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
	pullRequests := pullrequeststore.New(st.Pool.GetDB())
	service := pullrequestv1weave.PullRequestServiceAdapter{Impl: NewPullRequestService(NewPullRequestServiceParams{
		Agents:       st.Agents,
		PullRequests: pullRequests,
	})}
	id := seedAgentOn(t, st, "finished", "agent/x")

	opened := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	closedAt := opened.Add(3 * time.Hour)
	for _, params := range []pullrequeststore.UpsertParams{
		storedPullRequest("PR_1", 1, opened, &closedAt),
		storedPullRequest("PR_2", 2, opened.Add(time.Hour), &closedAt),
	} {
		_, err := pullRequests.Upsert(ctx, params)
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
	require.NoError(t, st.Agents.SetRepositoryURLByID(ctx, id, &repository))
	require.NoError(t, st.Agents.SetBranchByID(ctx, id, &branch))

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
