package pullrequest

import (
	"context"
	"errors"
	"testing"
	"time"

	"komrad/internal/checklog"
	"komrad/internal/forge"
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
	lines          *pullrequeststore.CheckLogLines
	search         *pullrequeststore.CheckLogSearch
	readsOf        []string
	searchesOf     [][]string
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
					Log:        &pullrequestv1.CheckLog{State: pullrequestv1.CheckLogState_CHECK_LOG_STATE_NONE},
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
			name: "a check and step that completed with a passing conclusion answer passed",
			readings: []pullrequeststore.StatusCheckReading{{
				HeadSHA:     "c1",
				FirstReadAt: readAt,
				ReadAt:      readAt,
				StatusChecks: []forge.StatusCheck{{
					ProviderID: "1",
					Kind:       forge.StatusCheckKindRun,
					Name:       "docs",
					State:      forge.StatusCheckStateCompleted,
					Conclusion: "skipped",
					Steps: []forge.StatusCheckStep{{
						Number:     1,
						Name:       "Build docs",
						State:      forge.StatusCheckStateCompleted,
						Conclusion: "success",
					}},
				}},
			}},
			want: &pullrequestv1.ListStatusChecksResponse{Readings: []*pullrequestv1.StatusCheckReading{{
				HeadSha:     "c1",
				FirstReadAt: xptr.Ptr("2026-10-03T12:00:00Z"),
				ReadAt:      xptr.Ptr("2026-10-03T12:00:00Z"),
				StatusChecks: []*pullrequestv1.StatusCheck{{
					ProviderId: "1",
					Kind:       pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_CHECK_RUN,
					Name:       "docs",
					State:      pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED,
					Conclusion: "skipped",
					Passed:     true,
					Log:        &pullrequestv1.CheckLog{State: pullrequestv1.CheckLogState_CHECK_LOG_STATE_NONE},
					Steps: []*pullrequestv1.StatusCheckStep{{
						Number:     1,
						Name:       "Build docs",
						State:      pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED,
						Conclusion: "success",
						Passed:     true,
					}},
				}},
			}}},
		},
		{
			name: "a check komrad stopped reading in flight answers unknown",
			readings: []pullrequeststore.StatusCheckReading{{
				HeadSHA:     "c1",
				FirstReadAt: readAt,
				ReadAt:      readAt,
				StatusChecks: []forge.StatusCheck{{
					ProviderID: "1",
					Kind:       forge.StatusCheckKindRun,
					Name:       "test",
					State:      forge.StatusCheckStateUnknown,
				}},
			}},
			want: &pullrequestv1.ListStatusChecksResponse{Readings: []*pullrequestv1.StatusCheckReading{{
				HeadSha:     "c1",
				FirstReadAt: xptr.Ptr("2026-10-03T12:00:00Z"),
				ReadAt:      xptr.Ptr("2026-10-03T12:00:00Z"),
				StatusChecks: []*pullrequestv1.StatusCheck{{
					ProviderId: "1",
					Kind:       pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_CHECK_RUN,
					Name:       "test",
					State:      pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_UNKNOWN,
					Log:        &pullrequestv1.CheckLog{State: pullrequestv1.CheckLogState_CHECK_LOG_STATE_NONE},
				}},
			}}},
		},
		{
			name: "a check run in a job answers its id and where its log stands",
			readings: []pullrequeststore.StatusCheckReading{{
				HeadSHA:     "c1",
				FirstReadAt: readAt,
				ReadAt:      readAt,
				StatusChecks: []forge.StatusCheck{{
					ProviderID: "1",
					Kind:       forge.StatusCheckKindRun,
					Name:       "test",
					State:      forge.StatusCheckStateCompleted,
					JobID:      xptr.Ptr(int64(7)),
				}, {
					ProviderID: "2",
					Kind:       forge.StatusCheckKindRun,
					Name:       "lint",
					State:      forge.StatusCheckStateRunning,
					JobID:      xptr.Ptr(int64(8)),
				}, {
					ProviderID: "3",
					Kind:       forge.StatusCheckKindRun,
					Name:       "build",
					State:      forge.StatusCheckStateCompleted,
					JobID:      xptr.Ptr(int64(9)),
				}},
				CheckIDs: map[string]string{"1": "check-1", "2": "check-2", "3": "check-3"},
				Logs: map[string]pullrequeststore.CheckLog{
					"check-1": {
						CheckID:    "check-1",
						FetchedAt:  readAt,
						Outcome:    pullrequeststore.CheckLogFetched,
						LineCount:  40,
						ErrorCount: 2,
						Steps: []checklog.StepRange{{
							Number:     1,
							Name:       "Run tests",
							Conclusion: "failure",
							FirstLine:  3,
							LineCount:  37,
							ErrorCount: 2,
						}, {
							Number:     2,
							Name:       "Post job",
							Conclusion: "success",
							FirstLine:  40,
							LineCount:  1,
						}},
					},
					"check-3": {
						CheckID:   "check-3",
						FetchedAt: readAt,
						Outcome:   pullrequeststore.CheckLogTooLarge,
					},
				},
			}},
			want: &pullrequestv1.ListStatusChecksResponse{Readings: []*pullrequestv1.StatusCheckReading{{
				HeadSha:     "c1",
				FirstReadAt: xptr.Ptr("2026-10-03T12:00:00Z"),
				ReadAt:      xptr.Ptr("2026-10-03T12:00:00Z"),
				StatusChecks: []*pullrequestv1.StatusCheck{{
					Id:         "check-1",
					ProviderId: "1",
					Kind:       pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_CHECK_RUN,
					Name:       "test",
					State:      pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED,
					Log: &pullrequestv1.CheckLog{
						State:      pullrequestv1.CheckLogState_CHECK_LOG_STATE_FETCHED,
						FetchedAt:  xptr.Ptr("2026-10-03T12:00:00Z"),
						LineCount:  40,
						ErrorCount: 2,
						Steps: []*pullrequestv1.CheckLogStep{{
							Number:     1,
							Name:       "Run tests",
							Conclusion: "failure",
							FirstLine:  3,
							LineCount:  37,
							ErrorCount: 2,
						}, {
							Number:     2,
							Name:       "Post job",
							Conclusion: "success",
							FirstLine:  40,
							LineCount:  1,
							Passed:     true,
						}},
					},
				}, {
					Id:         "check-2",
					ProviderId: "2",
					Kind:       pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_CHECK_RUN,
					Name:       "lint",
					State:      pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_RUNNING,
					Log:        &pullrequestv1.CheckLog{State: pullrequestv1.CheckLogState_CHECK_LOG_STATE_PENDING},
				}, {
					Id:         "check-3",
					ProviderId: "3",
					Kind:       pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_CHECK_RUN,
					Name:       "build",
					State:      pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED,
					Log: &pullrequestv1.CheckLog{
						State:     pullrequestv1.CheckLogState_CHECK_LOG_STATE_TOO_LARGE,
						FetchedAt: xptr.Ptr("2026-10-03T12:00:00Z"),
					},
				}},
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

// ReadCheckLog records the check asked about and answers lines unless err refuses it.
func (f *fakePullRequestLister) ReadCheckLog(_ context.Context, checkID string, _, _ int) (*pullrequeststore.CheckLogLines, error) {
	f.readsOf = append(f.readsOf, checkID)
	if f.err != nil {
		return nil, f.err
	}

	return f.lines, nil
}

// SearchCheckLogs records the checks asked about and answers search unless err refuses it.
func (f *fakePullRequestLister) SearchCheckLogs(_ context.Context, checkIDs []string, _ string) (*pullrequeststore.CheckLogSearch, error) {
	f.searchesOf = append(f.searchesOf, checkIDs)
	if f.err != nil {
		return nil, f.err
	}

	return f.search, nil
}

// TestReadCheckLog covers what reaches the caller for a window of a check's log.
func TestReadCheckLog(t *testing.T) {
	tests := []struct {
		name     string
		lines    *pullrequeststore.CheckLogLines
		err      error
		want     *pullrequestv1.ReadCheckLogResponse
		wantCode connect.Code
	}{
		{
			name:  "the window answers its lines and the whole log's length",
			lines: &pullrequeststore.CheckLogLines{Lines: []string{"go test ./...", "FAIL"}, LineCount: 40},
			want: &pullrequestv1.ReadCheckLogResponse{
				Lines:     []*pullrequestv1.CheckLogLine{{Text: "go test ./..."}, {Text: "FAIL"}},
				LineCount: 40,
			},
		},
		{
			name:     "a check with no fetched log",
			err:      store.ErrNotFound.Saying("no fetched log for check %s", "check-1"),
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "a store that cannot be read",
			err:      errors.New("connection refused"),
			wantCode: connect.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fakePullRequestLister{lines: tt.lines, err: tt.err}
			service := pullrequestv1komrad.PullRequestServiceAdapter{Impl: NewHandler(NewHandlerParams{PullRequests: reader})}

			got, err := service.ReadCheckLog(t.Context(), connect.NewRequest(&pullrequestv1.ReadCheckLogRequest{
				CheckId:   "check-1",
				FirstLine: 1,
				Count:     2,
			}))
			assert.Equal(t, []string{"check-1"}, reader.readsOf)
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

// TestSearchCheckLogs covers what reaches the caller for a search across check logs.
func TestSearchCheckLogs(t *testing.T) {
	tests := []struct {
		name     string
		search   *pullrequeststore.CheckLogSearch
		err      error
		want     *pullrequestv1.SearchCheckLogsResponse
		wantCode connect.Code
	}{
		{
			name: "each hit answers its check, line, step and whether it was cut",
			search: &pullrequeststore.CheckLogSearch{
				Hits: []pullrequeststore.CheckLogHit{
					{
						CheckID:    "check-1",
						Line:       4,
						StepNumber: 2,
						Text:       "--- FAIL: TestX",
					},
					{
						CheckID: "check-2",
						Line:    1,
						Text:    "FAIL long",
						Cut:     true,
					},
				},
				Limited: true,
			},
			want: &pullrequestv1.SearchCheckLogsResponse{
				Hits: []*pullrequestv1.CheckLogHit{
					{
						CheckId:    "check-1",
						Line:       4,
						StepNumber: 2,
						Text:       "--- FAIL: TestX",
					},
					{
						CheckId: "check-2",
						Line:    1,
						Text:    "FAIL long",
						Cut:     true,
					},
				},
				Limited: true,
			},
		},
		{
			name:     "a pattern that does not compile",
			err:      pullrequeststore.ErrInvalidPattern.Saying("bad pattern"),
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "a store that cannot be read",
			err:      errors.New("connection refused"),
			wantCode: connect.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fakePullRequestLister{search: tt.search, err: tt.err}
			service := pullrequestv1komrad.PullRequestServiceAdapter{Impl: NewHandler(NewHandlerParams{PullRequests: reader})}

			got, err := service.SearchCheckLogs(t.Context(), connect.NewRequest(&pullrequestv1.SearchCheckLogsRequest{CheckIds: []string{"check-1", "check-2"}, Pattern: "FAIL"}))
			assert.Equal(t, [][]string{{"check-1", "check-2"}}, reader.searchesOf)
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
