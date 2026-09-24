package pullrequest

import (
	"context"
	"errors"
	"testing"
	"time"

	"komrade/internal/git"
	"komrade/internal/pullrequests"
	pullrequeststore "komrade/internal/store/pullrequest"
	"komrade/lib/xptr"
	"komrade/lib/xslice"
	paginationv1 "komrade/proto/gen/pagination/v1"
	pullrequestv1 "komrade/proto/gen/pullrequest/v1"
	"komrade/proto/gen/pullrequest/v1/pullrequestv1komrade"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const komradeRepository = "https://github.com/harrisoncramer/komrade"

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
				RepositoryUrl: komradeRepository,
				Pagination:    &paginationv1.Pagination{Cursor: xptr.Ptr("not-a-token")},
			},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "a store that cannot be read",
			req:      &pullrequestv1.ListPullRequestsRequest{RepositoryUrl: komradeRepository},
			err:      errors.New("connection refused"),
			wantCode: connect.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fakePullRequestLister{err: tt.err}
			service := pullrequestv1komrade.PullRequestServiceAdapter{Impl: NewHandler(NewHandlerParams{PullRequests: reader})}

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
	service := pullrequestv1komrade.PullRequestServiceAdapter{Impl: NewHandler(NewHandlerParams{PullRequests: reader})}

	first, err := service.ListPullRequests(t.Context(), connect.NewRequest(&pullrequestv1.ListPullRequestsRequest{
		RepositoryUrl: "git@github.com:harrisoncramer/komrade.git",
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
		RepositoryUrl: komradeRepository,
		Pagination:    &paginationv1.Pagination{Cursor: first.Msg.NextPageToken},
	}))
	require.NoError(t, err)
	assert.Nil(t, second.Msg.NextPageToken)

	want := []pullrequests.ListPullRequestsParams{
		{
			RepositoryURL: "git@github.com:harrisoncramer/komrade.git",
			HeadBranch:    "agent/x",
			Limit:         2,
		},
		{
			RepositoryURL: komradeRepository,
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
