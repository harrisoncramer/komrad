// Package pullrequest serves the pull requests weave has stored from its forges.
package pullrequest

import (
	"context"
	"errors"

	"weave/internal/fail"
	"weave/internal/git"
	"weave/internal/rpc/render"
	"weave/internal/rpcerr"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	pullrequeststore "weave/internal/store/pullrequest"
	"weave/lib/dates"
	"weave/lib/pagination"
	"weave/lib/xslice"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"
)

type agentGetter interface {
	GetByID(ctx context.Context, id string) (*agentstore.Agent, error)
}

// storedPullRequestReader is what the handler reads of what the sync has stored.
type storedPullRequestReader interface {
	ListPage(ctx context.Context, params pullrequeststore.ListPageParams) ([]pullrequeststore.PullRequest, error)
	ListForAgent(ctx context.Context, agentID string) ([]pullrequeststore.PullRequest, error)
}

// syncReader is what the handler reads of where the sync is for a repository.
type syncReader interface {
	GetSync(ctx context.Context, repositoryURL string) (*pullrequeststore.Sync, error)
}

type NewHandlerParams struct {
	Agents           agentGetter
	PullRequests     storedPullRequestReader
	PullRequestSyncs syncReader
}

type Handler struct {
	agents       agentGetter
	pullRequests storedPullRequestReader
	syncs        syncReader
}

// NewHandler constructs the handler over the stores.
func NewHandler(params NewHandlerParams) *Handler {
	return &Handler{
		agents:       params.Agents,
		pullRequests: params.PullRequests,
		syncs:        params.PullRequestSyncs,
	}
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
		cursor:      pullrequeststore.Cursor{Finished: stored.IsFinished(), OpenedAt: stored.OpenedAt, ID: stored.ID},
	}
}

// ListAgentPullRequests is every stored pull request opened from an agent's branch, newest
// opened first, with the one its row stands for marked.
func (s *Handler) ListAgentPullRequests(ctx context.Context, req *pullrequestv1.ListAgentPullRequestsRequest) (*pullrequestv1.ListAgentPullRequestsResponse, *fail.Error) {
	agent, err := s.agents.GetByID(ctx, req.GetAgentId())
	if err != nil {
		return nil, rpcerr.From(err, "failed to get agent")
	}

	response := &pullrequestv1.ListAgentPullRequestsResponse{}
	if agent.RepositoryURL == nil || *agent.RepositoryURL == "" {
		return response, nil
	}

	sync, err := s.syncs.GetSync(ctx, *agent.RepositoryURL)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, rpcerr.From(err, "failed to read where the sync is for the repository")
	}
	if sync != nil && sync.IsSwept() {
		response.RepositoryReadAt = dates.StampPtr(sync.ReadAt)
	}

	pullRequests, err := s.pullRequests.ListForAgent(ctx, agent.ID)
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the agent's pull requests")
	}

	current := indexOfCurrent(pullRequests)
	response.PullRequests = make([]*pullrequestv1.AgentPullRequest, 0, len(pullRequests))
	for i, pullRequest := range pullRequests {
		response.PullRequests = append(response.PullRequests, renderAgentPullRequest(pullRequest, i == current))
	}

	return response, nil
}

// indexOfCurrent is the position of the pull request an agent's row stands for in a listing
// newest opened first: the first not finished, else the first, and -1 in an empty listing.
func indexOfCurrent(pullRequests []pullrequeststore.PullRequest) int {
	for i, pullRequest := range pullRequests {
		if !pullRequest.IsFinished() {
			return i
		}
	}

	if len(pullRequests) == 0 {
		return -1
	}

	return 0
}

func renderAgentPullRequest(pullRequest pullrequeststore.PullRequest, current bool) *pullrequestv1.AgentPullRequest {
	return &pullrequestv1.AgentPullRequest{
		Number:       int32(pullRequest.Number),
		Url:          pullRequest.URL,
		Title:        pullRequest.Title,
		State:        render.PullRequestStateOf(pullRequest),
		Checks:       render.ChecksOf(pullRequest.Checks),
		Mergeability: render.MergeabilityOf(pullRequest.Mergeability),
		Current:      current,
		OpenedAt:     dates.Stamp(pullRequest.OpenedAt),
		MergedAt:     dates.StampPtr(pullRequest.MergedAt),
		ClosedAt:     dates.StampPtr(pullRequest.ClosedAt),
	}
}
