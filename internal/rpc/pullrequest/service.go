// Package pullrequest serves the pull requests weave reads through its forge connections.
package pullrequest

import (
	"context"
	"fmt"

	"weave/internal/fail"
	"weave/internal/forge"
	"weave/internal/git"
	"weave/internal/rpcerr"
	forgestore "weave/internal/store/forge"
	"weave/lib/dates"
	"weave/lib/pagination"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"
)

// forgeFinder is what the handler asks of the forge service: the connection a host has, and
// the forge to speak to as it.
type forgeFinder interface {
	ConnectionFor(ctx context.Context, host string) (*forgestore.Connection, error)
	ForConnection(connection forgestore.Connection) (forge.Forge, error)
}

type PullRequestService struct {
	forges forgeFinder
}

// NewPullRequestService constructs the handler over the forge service.
func NewPullRequestService(forges forgeFinder) *PullRequestService {
	return &PullRequestService{forges: forges}
}

// pageCursor is where a listing left off, which for a forge that pages by number is the
// number of the page to read next.
type pageCursor struct {
	Page int `json:"page"`
}

// Validate refuses a page nobody could have been handed.
func (c pageCursor) Validate() error {
	if c.Page < 1 {
		return fmt.Errorf("page %d is before the first", c.Page)
	}

	return nil
}

// ListPullRequests is one page of a repository's pull requests, read through the connection
// for the repository's host.
func (s *PullRequestService) ListPullRequests(ctx context.Context, req *pullrequestv1.ListPullRequestsRequest) (*pullrequestv1.ListPullRequestsResponse, *fail.Error) {
	repo, err := git.ParseRemoteURL(req.GetRemoteUrl())
	if err != nil {
		return nil, rpcerr.From(err, "failed to read the remote")
	}

	page := 1
	if cursor, err := pagination.Decode[pageCursor](req.GetPagination().GetCursor()); err != nil {
		return nil, rpcerr.From(err, "failed to read the page token")
	} else if cursor != nil {
		page = cursor.Page
	}

	connection, err := s.forges.ConnectionFor(ctx, repo.Host)
	if err != nil {
		return nil, rpcerr.From(err, "failed to find the forge for the remote")
	}

	f, err := s.forges.ForConnection(*connection)
	if err != nil {
		return nil, rpcerr.From(err, "failed to build the forge")
	}

	listed, err := f.List(ctx, forge.ListParams{
		Repo:    repo,
		Branch:  req.GetBranch(),
		Page:    page,
		PerPage: int(pagination.Bound(req.GetPagination().GetLimit())),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the pull requests")
	}

	resources := make([]*pullrequestv1.PullRequest, 0, len(listed.PullRequests))
	for _, pullRequest := range listed.PullRequests {
		resources = append(resources, renderPullRequest(pullRequest))
	}

	response := &pullrequestv1.ListPullRequestsResponse{PullRequests: resources}
	if listed.HasMore {
		next, err := pagination.Token(pageCursor{Page: page + 1})
		if err != nil {
			return nil, rpcerr.From(err, "failed to build the next page token")
		}

		response.NextPageToken = next
	}

	return response, nil
}

func renderPullRequest(pullRequest forge.PullRequest) *pullrequestv1.PullRequest {
	head := ""
	if !pullRequest.Head.IsZero() {
		head = pullRequest.Head.Canonical()
	}

	return &pullrequestv1.PullRequest{
		Forge:             pullRequest.Forge,
		ProviderId:        pullRequest.ProviderID,
		Url:               pullRequest.URL,
		RepositoryUrl:     pullRequest.Repository.Canonical(),
		HeadRepositoryUrl: head,
		Number:            int32(pullRequest.Number),
		Title:             pullRequest.Title,
		HeadBranch:        pullRequest.HeadBranch,
		HeadSha:           pullRequest.HeadSHA,
		BaseBranch:        pullRequest.BaseBranch,
		BaseSha:           pullRequest.BaseSHA,
		Draft:             pullRequest.Draft,
		State:             pullRequest.State,
		CreatedAt:         dates.Stamp(pullRequest.CreatedAt),
		UpdatedAt:         dates.Stamp(pullRequest.UpdatedAt),
		MergedAt:          dates.StampPtr(pullRequest.MergedAt),
		ClosedAt:          dates.StampPtr(pullRequest.ClosedAt),
	}
}
