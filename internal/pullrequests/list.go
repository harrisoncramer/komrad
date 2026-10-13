package pullrequests

import (
	"context"
	"fmt"

	"komrad/internal/forge"
	"komrad/internal/git"
	pullrequeststore "komrad/internal/store/pullrequest"
)

type NewListerParams struct {
	PullRequests *pullrequeststore.PullRequestStore
	StatusChecks *pullrequeststore.StatusCheckStore
}

// Lister reads the stored pull requests of a repository.
type Lister struct {
	pullRequests *pullrequeststore.PullRequestStore
	statusChecks *pullrequeststore.StatusCheckStore
}

// NewLister builds a lister over the store.
func NewLister(params NewListerParams) *Lister {
	return &Lister{pullRequests: params.PullRequests, statusChecks: params.StatusChecks}
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

// ListStatusChecks is the reading of each head a pull request's checks were read on, oldest first, holding
// the newest run of each check.
func (l *Lister) ListStatusChecks(ctx context.Context, pullRequestID string) ([]pullrequeststore.StatusCheckReading, error) {
	readings, err := l.statusChecks.ListOfPullRequest(ctx, pullRequestID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the status checks of pull request %s: %w", pullRequestID, err)
	}

	for i := range readings {
		readings[i].StatusChecks = forge.NewestRuns(readings[i].StatusChecks)
	}

	return readings, nil
}

// ReadCheckLog is up to count lines of a check's fetched log from firstLine, counting from one.
func (l *Lister) ReadCheckLog(ctx context.Context, checkID string, firstLine, count int) (*pullrequeststore.CheckLogLines, error) {
	lines, err := l.statusChecks.GetCheckLogLines(ctx, checkID, firstLine, count)
	if err != nil {
		return nil, fmt.Errorf("failed to read the log of check %s: %w", checkID, err)
	}

	return lines, nil
}

// SearchCheckLogs is every line of the checks' fetched logs the RE2 pattern matches.
func (l *Lister) SearchCheckLogs(ctx context.Context, checkIDs []string, pattern string) (*pullrequeststore.CheckLogSearch, error) {
	search, err := l.statusChecks.ListCheckLogHits(ctx, checkIDs, pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to search the logs of %d checks: %w", len(checkIDs), err)
	}

	return search, nil
}
