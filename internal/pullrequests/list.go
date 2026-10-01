package pullrequests

import (
	"context"
	"fmt"

	"komrad/internal/git"
	pullrequeststore "komrad/internal/store/pullrequest"
)

type NewListerParams struct {
	PullRequests *pullrequeststore.PullRequestStore
}

// Lister reads the stored pull requests of a repository.
type Lister struct {
	pullRequests *pullrequeststore.PullRequestStore
}

// NewLister builds a lister over the store.
func NewLister(params NewListerParams) *Lister {
	return &Lister{pullRequests: params.PullRequests}
}

type ListPullRequestsParams struct {
	RepositoryURL string
	HeadBranch    string
	Limit         int32
	Cursor        *pullrequeststore.Cursor
}

// ListPullRequests is one page of a repository's stored pull requests, however its remote is spelled.
func (l *Lister) ListPullRequests(ctx context.Context, params ListPullRequestsParams) ([]pullrequeststore.PullRequest, error) {
	repo, err := git.ParseRemoteURL(params.RepositoryURL)
	if err != nil {
		return nil, fmt.Errorf("failed to read repository %q: %w", params.RepositoryURL, err)
	}

	listed, err := l.pullRequests.ListPage(ctx, pullrequeststore.ListPageParams{
		RepositoryURL: repo.Canonical(),
		HeadBranch:    params.HeadBranch,
		Limit:         params.Limit,
		Cursor:        params.Cursor,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list the pull requests of %s: %w", repo.Canonical(), err)
	}

	return listed, nil
}
