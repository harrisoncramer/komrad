// Package pullrequest serves the pull requests komrad has stored from its forges.
package pullrequest

import (
	"context"

	"komrad/internal/fail"
	"komrad/internal/pullrequests"
	"komrad/internal/rpc/render"
	"komrad/internal/rpcerr"
	"komrad/internal/rpcpage"
	pullrequeststore "komrad/internal/store/pullrequest"
	"komrad/lib/xslice"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
)

// pullRequestLister is what the handler reads of what the sync has stored.
type pullRequestLister interface {
	ListPullRequests(ctx context.Context, params pullrequests.ListPullRequestsParams) ([]pullrequeststore.PullRequest, error)
	ListStatusChecks(ctx context.Context, pullRequestID string) ([]pullrequeststore.StatusCheckReading, error)
}

type NewHandlerParams struct {
	PullRequests pullRequestLister
}

type Handler struct {
	pullRequests pullRequestLister
}

// NewHandler constructs the handler over the lister.
func NewHandler(params NewHandlerParams) *Handler {
	return &Handler{
		pullRequests: params.PullRequests,
	}
}

// ListPullRequests is one page of a repository's stored pull requests, the unfinished ones
// first and each group newest opened first.
func (s *Handler) ListPullRequests(ctx context.Context, req *pullrequestv1.ListPullRequestsRequest) (*pullrequestv1.ListPullRequestsResponse, *fail.Error) {
	page, failure := rpcpage.List(req.GetPagination(), func(limit int64, cursor *pullrequeststore.Cursor) ([]pullRequestPage, *fail.Error) {
		listed, err := s.pullRequests.ListPullRequests(ctx, pullrequests.ListPullRequestsParams{
			RepositoryURL: req.GetRepositoryUrl(),
			HeadBranch:    req.GetHeadBranch(),
			Limit:         int32(limit),
			Cursor:        cursor,
		})
		if err != nil {
			return nil, rpcerr.From(err, "failed to list the pull requests")
		}

		return xslice.Map(listed, renderPullRequestPage), nil
	})
	if failure != nil {
		return nil, failure
	}

	return &pullrequestv1.ListPullRequestsResponse{
		PullRequests:  xslice.Map(page.Resources, func(resource pullRequestPage) *pullrequestv1.PullRequest { return resource.PullRequest }),
		NextPageToken: page.Next,
	}, nil
}

type pullRequestPage struct {
	*pullrequestv1.PullRequest
	cursor pullrequeststore.Cursor
}

// Cursor returns the keyset cursor locating this pull request.
func (p pullRequestPage) Cursor() (pullrequeststore.Cursor, error) {
	return p.cursor, nil
}

func renderPullRequestPage(stored pullrequeststore.PullRequest) pullRequestPage {
	return pullRequestPage{
		PullRequest: render.PullRequest(stored),
		cursor: pullrequeststore.Cursor{
			Finished: stored.IsFinished(),
			OpenedAt: stored.OpenedAt,
			ID:       stored.ID,
		},
	}
}

// ListStatusChecks is the status checks read on each of a pull request's heads, oldest first and empty when they never were.
func (s *Handler) ListStatusChecks(ctx context.Context, req *pullrequestv1.ListStatusChecksRequest) (*pullrequestv1.ListStatusChecksResponse, *fail.Error) {
	readings, err := s.pullRequests.ListStatusChecks(ctx, req.GetPullRequestId())
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the checks")
	}

	return render.StatusCheckReadings(readings), nil
}
