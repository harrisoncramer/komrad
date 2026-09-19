// Package pullrequest serves the pull requests weave reads through its forge connections.
package pullrequest

import (
	"context"

	"weave/internal/fail"
	"weave/internal/forge"
	"weave/internal/git"
	"weave/internal/rpcerr"
	forgestore "weave/internal/store/forge"
	"weave/lib/dates"
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

// ListPullRequests is every pull request opened from a branch, read through the connection for
// the repository's host.
func (s *PullRequestService) ListPullRequests(ctx context.Context, req *pullrequestv1.ListPullRequestsRequest) (*pullrequestv1.ListPullRequestsResponse, *fail.Error) {
	repo, err := git.ParseRemoteURL(req.GetRemoteUrl())
	if err != nil {
		return nil, rpcerr.From(err, "failed to read the remote")
	}

	connection, err := s.forges.ConnectionFor(ctx, repo.Host)
	if err != nil {
		return nil, rpcerr.From(err, "failed to find the forge for the remote")
	}

	f, err := s.forges.ForConnection(*connection)
	if err != nil {
		return nil, rpcerr.From(err, "failed to build the forge")
	}

	pullRequests, err := f.ListOnBranch(ctx, repo, req.GetBranch())
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the pull requests")
	}

	resources := make([]*pullrequestv1.PullRequest, 0, len(pullRequests))
	for _, pullRequest := range pullRequests {
		resources = append(resources, renderPullRequest(pullRequest))
	}

	return &pullrequestv1.ListPullRequestsResponse{PullRequests: resources}, nil
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
