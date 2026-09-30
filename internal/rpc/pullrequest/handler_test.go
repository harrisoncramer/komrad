package pullrequest

import (
	"context"
	"errors"
	"testing"
	"time"

	"komrad/internal/caller"
	"komrad/internal/git"
	"komrad/internal/pullrequests"
	"komrad/internal/store"
	pullrequeststore "komrad/internal/store/pullrequest"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	paginationv1 "komrad/proto/gen/pagination/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	"komrad/proto/gen/pullrequest/v1/pullrequestv1komrad"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const komradRepository = "https://github.com/harrisoncramer/komrad"

// fakePullRequestLister answers from its fields and records each page asked for.
type fakePullRequestLister struct {
	pullRequests []pullrequeststore.PullRequest
	err          error
	listed       []pullrequests.ListPullRequestsParams
}

// ListPullRequests records the params and answers pullRequests unless err refuses it.
func (f *fakePullRequestLister) ListPullRequests(_ context.Context, params pullrequests.ListPullRequestsParams) ([]pullrequeststore.PullRequest, error) {
	f.listed = append(f.listed, params)
	if f.err != nil {
		return nil, f.err
	}

	return f.pullRequests, nil
}

// TestListPullRequestsRefusalsMapToCodes pins the code each refusal reaches the caller as.
func TestListPullRequestsRefusalsMapToCodes(t *testing.T) {
	tests := []struct {
		name     string
		req      *pullrequestv1.ListPullRequestsRequest
		err      error
		wantCode connect.Code
	}{
		{
			name:     "a repository that names no forge",
			req:      &pullrequestv1.ListPullRequestsRequest{RepositoryUrl: "/local/path"},
			err:      git.ErrNotForgeRemote,
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name: "a cursor nobody could have been handed",
			req: &pullrequestv1.ListPullRequestsRequest{
				RepositoryUrl: komradRepository,
				Pagination:    &paginationv1.Pagination{Cursor: xptr.Ptr("not-a-token")},
			},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "a store that cannot be read",
			req:      &pullrequestv1.ListPullRequestsRequest{RepositoryUrl: komradRepository},
			err:      errors.New("connection refused"),
			wantCode: connect.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fakePullRequestLister{err: tt.err}
			service := pullrequestv1komrad.PullRequestServiceAdapter{Impl: NewHandler(NewHandlerParams{PullRequests: reader})}

			_, err := service.ListPullRequests(t.Context(), connect.NewRequest(tt.req))
			require.Error(t, err)
			assert.Equal(t, tt.wantCode, connect.CodeOf(err))
		})
	}
}

// TestListPullRequests pins what reaches the lister and that a full page hands back its last cursor.
func TestListPullRequests(t *testing.T) {
	opened := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	merged := opened.Add(time.Hour)
	reader := &fakePullRequestLister{pullRequests: []pullrequeststore.PullRequest{
		{
			ID:       "01a0d1d7-0000-7000-8000-000000000002",
			Number:   2,
			OpenedAt: opened.Add(time.Hour),
		},
		{
			ID:       "01a0d1d7-0000-7000-8000-000000000001",
			Number:   1,
			OpenedAt: opened,
			MergedAt: &merged,
		},
	}}
	service := pullrequestv1komrad.PullRequestServiceAdapter{Impl: NewHandler(NewHandlerParams{PullRequests: reader})}

	first, err := service.ListPullRequests(t.Context(), connect.NewRequest(&pullrequestv1.ListPullRequestsRequest{
		RepositoryUrl: "git@github.com:harrisoncramer/komrad.git",
		HeadBranch:    "agent/x",
		Pagination:    &paginationv1.Pagination{Limit: xptr.Ptr(int32(2))},
	}))
	require.NoError(t, err)
	require.NotNil(t, first.Msg.NextPageToken)

	numbers := xslice.Map(first.Msg.GetPullRequests(), func(pullRequest *pullrequestv1.PullRequest) int32 {
		return pullRequest.GetNumber()
	})
	assert.Equal(t, []int32{2, 1}, numbers)

	reader.pullRequests = nil
	second, err := service.ListPullRequests(t.Context(), connect.NewRequest(&pullrequestv1.ListPullRequestsRequest{
		RepositoryUrl: komradRepository,
		Pagination:    &paginationv1.Pagination{Cursor: first.Msg.NextPageToken},
	}))
	require.NoError(t, err)
	assert.Nil(t, second.Msg.NextPageToken)

	want := []pullrequests.ListPullRequestsParams{
		{
			RepositoryURL: "git@github.com:harrisoncramer/komrad.git",
			HeadBranch:    "agent/x",
			Limit:         2,
		},
		{
			RepositoryURL: komradRepository,
			Limit:         20,
			Cursor: &pullrequeststore.Cursor{
				Finished: true,
				OpenedAt: opened,
				ID:       "01a0d1d7-0000-7000-8000-000000000001",
			},
		},
	}
	assert.Equal(t, want, reader.listed)
}

// fakeDismisser records each dismissal asked for and answers err.
type fakeDismisser struct {
	err       error
	dismissed []pullrequests.DismissForgeThreadParams
}

// DismissForgeThread records the params and answers err.
func (f *fakeDismisser) DismissForgeThread(_ context.Context, params pullrequests.DismissForgeThreadParams) error {
	f.dismissed = append(f.dismissed, params)

	return f.err
}

// TestDismissForgeThread pins who the dismissal is asked for and the code each refusal reaches the caller as.
func TestDismissForgeThread(t *testing.T) {
	const threadID = "01a0d1d7-0000-7000-8000-000000000003"
	tests := []struct {
		name     string
		req      *pullrequestv1.DismissForgeThreadRequest
		caller   caller.Caller
		err      error
		want     []pullrequests.DismissForgeThreadParams
		wantCode connect.Code
	}{
		{
			name: "the person's dismissal names no agent",
			req:  &pullrequestv1.DismissForgeThreadRequest{Id: threadID},
			want: []pullrequests.DismissForgeThreadParams{{ID: threadID}},
		},
		{
			name:     "an agent's dismissal names it and is refused",
			req:      &pullrequestv1.DismissForgeThreadRequest{Id: threadID},
			caller:   caller.Caller{AgentID: "01a0d1d7-0000-7000-8000-000000000004"},
			err:      pullrequests.ErrDismissHumanOnly,
			want:     []pullrequests.DismissForgeThreadParams{{ID: threadID, AgentID: "01a0d1d7-0000-7000-8000-000000000004"}},
			wantCode: connect.CodePermissionDenied,
		},
		{
			name:     "a thread that is not stored",
			req:      &pullrequestv1.DismissForgeThreadRequest{Id: threadID},
			err:      store.ErrNotFound,
			want:     []pullrequests.DismissForgeThreadParams{{ID: threadID}},
			wantCode: connect.CodeNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dismisser := &fakeDismisser{err: tt.err}
			service := pullrequestv1komrad.PullRequestServiceAdapter{Impl: NewHandler(NewHandlerParams{Dismisser: dismisser})}

			_, err := service.DismissForgeThread(caller.With(t.Context(), tt.caller), connect.NewRequest(tt.req))
			if tt.wantCode != 0 {
				require.Error(t, err)
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.want, dismisser.dismissed)
		})
	}
}
