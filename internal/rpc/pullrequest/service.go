// Package pullrequest serves the pull requests weave reads through its forge connections.
package pullrequest

import (
	"context"
	"errors"
	"fmt"

	"weave/internal/fail"
	"weave/internal/forge"
	"weave/internal/git"
	"weave/internal/rpc/render"
	"weave/internal/rpcerr"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	forgestore "weave/internal/store/forge"
	pullrequeststore "weave/internal/store/pullrequest"
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

type agentGetter interface {
	GetByID(ctx context.Context, id string) (*agentstore.Agent, error)
}

// storedPullRequestReader is what the handler reads of what the sync has stored.
type storedPullRequestReader interface {
	ListForAgent(ctx context.Context, agentID string) ([]pullrequeststore.PullRequest, error)
	GetSync(ctx context.Context, repositoryURL string) (*pullrequeststore.Sync, error)
}

type PullRequestService struct {
	forges       forgeFinder
	agents       agentGetter
	pullRequests storedPullRequestReader
}

type NewPullRequestServiceParams struct {
	Forges       forgeFinder
	Agents       agentGetter
	PullRequests storedPullRequestReader
}

// NewPullRequestService constructs the handler over the forge service and the stores.
func NewPullRequestService(params NewPullRequestServiceParams) *PullRequestService {
	return &PullRequestService{
		forges:       params.Forges,
		agents:       params.Agents,
		pullRequests: params.PullRequests,
	}
}

// pageCursor is where a listing left off, which for a forge that pages by number is the
// number of the page to read next and the size the pages before it were cut at.
type pageCursor struct {
	Page    int `json:"page"`
	PerPage int `json:"per_page"`
}

// Validate refuses a page nobody could have been handed.
func (c pageCursor) Validate() error {
	if c.Page < 1 {
		return fmt.Errorf("page %d is before the first", c.Page)
	}
	if c.PerPage < 1 {
		return fmt.Errorf("%d to a page is no page", c.PerPage)
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

	window := pageCursor{Page: 1, PerPage: int(pagination.Bound(req.GetPagination().GetLimit()))}
	if cursor, err := pagination.Decode[pageCursor](req.GetPagination().GetCursor()); err != nil {
		return nil, rpcerr.From(err, "failed to read the page token")
	} else if cursor != nil {
		window = *cursor
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
		Page:    window.Page,
		PerPage: window.PerPage,
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
		next, err := pagination.Token(pageCursor{Page: window.Page + 1, PerPage: window.PerPage})
		if err != nil {
			return nil, rpcerr.From(err, "failed to build the next page token")
		}

		response.NextPageToken = next
	}

	return response, nil
}

// ListAgentPullRequests is every stored pull request opened from an agent's branch, newest
// opened first, with the one its row stands for marked.
func (s *PullRequestService) ListAgentPullRequests(ctx context.Context, req *pullrequestv1.ListAgentPullRequestsRequest) (*pullrequestv1.ListAgentPullRequestsResponse, *fail.Error) {
	agent, err := s.agents.GetByID(ctx, req.GetAgentId())
	if err != nil {
		return nil, rpcerr.From(err, "failed to get agent")
	}

	response := &pullrequestv1.ListAgentPullRequestsResponse{}
	if agent.RepositoryURL == nil || *agent.RepositoryURL == "" {
		return response, nil
	}

	sync, err := s.pullRequests.GetSync(ctx, *agent.RepositoryURL)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, rpcerr.From(err, "failed to read where the sync is for the repository")
	}
	if sync != nil {
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
		Number:   int32(pullRequest.Number),
		Url:      pullRequest.URL,
		Title:    pullRequest.Title,
		State:    render.PullRequestStateOf(pullRequest),
		Current:  current,
		OpenedAt: dates.Stamp(pullRequest.OpenedAt),
		MergedAt: dates.StampPtr(pullRequest.MergedAt),
		ClosedAt: dates.StampPtr(pullRequest.ClosedAt),
	}
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
