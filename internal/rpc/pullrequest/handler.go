// Package pullrequest serves the pull requests weave has stored from its forges.
package pullrequest

import (
	"context"

	"weave/internal/fail"
	"weave/internal/git"
	"weave/internal/rpc/render"
	"weave/internal/rpcerr"
	pullrequeststore "weave/internal/store/pullrequest"
	"weave/lib/pagination"
	"weave/lib/xslice"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"
)

// storedPullRequestReader is what the handler reads of what the sync has stored.
type storedPullRequestReader interface {
	ListPage(ctx context.Context, params pullrequeststore.ListPageParams) ([]pullrequeststore.PullRequest, error)
}

type NewHandlerParams struct {
	PullRequests storedPullRequestReader
}

type Handler struct {
	pullRequests storedPullRequestReader
}

// NewHandler constructs the handler over the store.
func NewHandler(params NewHandlerParams) *Handler {
	return &Handler{pullRequests: params.PullRequests}
}

// ListPullRequests is one page of a repository's stored pull requests, the unfinished ones
// first and each group newest opened first.
func (s *Handler) ListPullRequests(ctx context.Context, req *pullrequestv1.ListPullRequestsRequest) (*pullrequestv1.ListPullRequestsResponse, *fail.Error) {
	repo, err := git.ParseRemoteURL(req.GetRepositoryUrl())
	if err != nil {
		return nil, rpcerr.From(err, "failed to read the repository")
	}

	paginator, err := pagination.New[pullrequeststore.Cursor, pullRequestPage](req.GetPagination())
	if err != nil {
		return nil, rpcerr.From(err, "failed to read the pagination")
	}

	listed, err := s.pullRequests.ListPage(ctx, pullrequeststore.ListPageParams{
		RepositoryURL: repo.Canonical(),
		HeadBranch:    req.GetHeadBranch(),
		Limit:         int32(paginator.Limit()),
		Cursor:        paginator.Cursor(),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the pull requests")
	}

	resources := xslice.Map(listed, renderPullRequestPage)

	next, err := paginator.NextToken(resources)
	if err != nil {
		return nil, rpcerr.From(err, "failed to build the next page token")
	}

	return &pullrequestv1.ListPullRequestsResponse{
		PullRequests:  xslice.Map(resources, func(resource pullRequestPage) *pullrequestv1.PullRequest { return resource.PullRequest }),
		NextPageToken: next,
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
