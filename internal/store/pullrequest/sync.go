package pullrequest

import (
	"context"
	"fmt"

	"komrad/db/models"
	"komrad/internal/store"
	"komrad/lib/xslice"
)

// Sync is a repository the sync has read at least once.
type Sync struct {
	RepositoryURL string
}

// LiveBranch is one branch the pull request sync reads in a repository: a branch a live agent
// works on away from its workflow's base branch, or the head of a live child's parent pull
// request, with its last seen head commit.
type LiveBranch struct {
	RepositoryURL string
	Branch        string
	// HeadSHA is the branch's last seen head commit, empty before komrad read it.
	HeadSHA string
}

// PullRequestSyncStore is which repositories the sync has read and what it reads in them.
type PullRequestSyncStore struct {
	queries *models.Queries
}

// NewPullRequestSyncStore builds a sync store over a pool or transaction.
func NewPullRequestSyncStore(db store.DBTX) *PullRequestSyncStore {
	return &PullRequestSyncStore{queries: models.New(db)}
}

// ListLiveBranches is every branch the pull request sync reads, by repository, branch and commit.
func (s *PullRequestSyncStore) ListLiveBranches(ctx context.Context) ([]LiveBranch, error) {
	rows, err := s.queries.ListLiveBranches(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the live branches: %w", err)
	}

	return xslice.Map(rows, func(row *models.ListLiveBranchesRow) LiveBranch {
		return LiveBranch{
			RepositoryURL: row.RepositoryUrl,
			Branch:        row.Branch,
			HeadSHA:       row.HeadSha,
		}
	}), nil
}

// EnsureSync records that a repository has been read, leaving a row it has alone.
func (s *PullRequestSyncStore) EnsureSync(ctx context.Context, repositoryURL string) error {
	if err := s.queries.EnsurePullRequestSync(ctx, repositoryURL); err != nil {
		return fmt.Errorf("failed to ensure the sync row for %s: %w", repositoryURL, err)
	}

	return nil
}

func fromSyncRow(row *models.PullRequestSync) Sync {
	return Sync{RepositoryURL: row.RepositoryUrl}
}
