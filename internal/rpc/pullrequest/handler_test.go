package pullrequest

import (
	"context"
	"errors"
	"testing"
	"time"

	"komrad/internal/forge"
	"komrad/internal/git"
	"komrad/internal/pullrequests"
	pullrequeststore "komrad/internal/store/pullrequest"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	paginationv1 "komrad/proto/gen/pagination/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	"komrad/proto/gen/pullrequest/v1/pullrequestv1komrad"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

const komradRepository = "https://github.com/harrisoncramer/komrad"

// fakePullRequestLister answers from its fields and records each page asked for.
type fakePullRequestLister struct {
	pullRequests   []pullrequeststore.PullRequest
	readings       []pullrequeststore.StatusCheckReading
	err            error
	listed         []pullrequests.ListPullRequestsParams
	statusChecksOf []string
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

// ListStatusChecks records the pull request asked about and answers readings unless err refuses it.
func (f *fakePullRequestLister) ListStatusChecks(_ context.Context, pullRequestID string) ([]pullrequeststore.StatusCheckReading, error) {
	f.statusChecksOf = append(f.statusChecksOf, pullRequestID)
	if f.err != nil {
		return nil, f.err
	}

	return f.readings, nil
}

// TestListStatusChecks covers what reaches the caller for a pull request's checks.
func TestListStatusChecks(t *testing.T) {
	readAt := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		readings []pullrequeststore.StatusCheckReading
		err      error
		want     *pullrequestv1.ListStatusChecksResponse
		wantCode connect.Code
	}{
		{
			name: "checks never read answer empty",
			want: &pullrequestv1.ListStatusChecksResponse{},
		},
		{
			name: "each reading answers its head and checks with their steps, in the order listed",
			readings: []pullrequeststore.StatusCheckReading{{
				HeadSHA:     "c1",
				FirstReadAt: readAt.Add(-time.Hour),
				ReadAt:      readAt,
				StatusChecks: []forge.StatusCheck{{
					ProviderID: "1",
					Kind:       forge.StatusCheckKindRun,
					Source:     "CI",
					Name:       "test",
					State:      forge.StatusCheckStateCompleted,
					Conclusion: "failure",
					Required:   true,
					URL:        "https://github.com/harrisoncramer/komrad/runs/1",
					Steps: []forge.StatusCheckStep{{
						Number:     1,
						Name:       "Run tests",
						State:      forge.StatusCheckStateCompleted,
						Conclusion: "failure",
						StartedAt:  xptr.Ptr(readAt.Add(-time.Minute)),
						FinishedAt: &readAt,
					}},
				}},
			}, {
				HeadSHA:     "c2",
				FirstReadAt: readAt.Add(time.Minute),
				ReadAt:      readAt.Add(time.Minute),
			}},
			want: &pullrequestv1.ListStatusChecksResponse{Readings: []*pullrequestv1.StatusCheckReading{{
				HeadSha:     "c1",
				FirstReadAt: xptr.Ptr("2026-10-03T11:00:00Z"),
				ReadAt:      xptr.Ptr("2026-10-03T12:00:00Z"),
				StatusChecks: []*pullrequestv1.StatusCheck{{
					ProviderId: "1",
					Kind:       pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_CHECK_RUN,
					Source:     "CI",
					Name:       "test",
					State:      pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED,
					Conclusion: "failure",
					Required:   true,
					Url:        "https://github.com/harrisoncramer/komrad/runs/1",
					Steps: []*pullrequestv1.StatusCheckStep{{
						Number:     1,
						Name:       "Run tests",
						State:      pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED,
						Conclusion: "failure",
						StartedAt:  xptr.Ptr("2026-10-03T11:59:00Z"),
						FinishedAt: xptr.Ptr("2026-10-03T12:00:00Z"),
					}},
				}},
			}, {
				HeadSha:     "c2",
				FirstReadAt: xptr.Ptr("2026-10-03T12:01:00Z"),
				ReadAt:      xptr.Ptr("2026-10-03T12:01:00Z"),
			}}},
		},
		{
			name:     "a store that cannot be read",
			err:      errors.New("connection refused"),
			wantCode: connect.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fakePullRequestLister{readings: tt.readings, err: tt.err}
			service := pullrequestv1komrad.PullRequestServiceAdapter{Impl: NewHandler(NewHandlerParams{PullRequests: reader})}

			got, err := service.ListStatusChecks(t.Context(), connect.NewRequest(&pullrequestv1.ListStatusChecksRequest{PullRequestId: "PR_1"}))
			assert.Equal(t, []string{"PR_1"}, reader.statusChecksOf)
			if tt.wantCode != 0 {
				require.Error(t, err)
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))

				return
			}
			require.NoError(t, err)
			assert.True(t, proto.Equal(tt.want, got.Msg), "got %v", got.Msg)
		})
	}
}
