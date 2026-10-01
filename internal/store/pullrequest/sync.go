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

// Target is one branch a live agent sits on in a repository, away from its workflow's base
// branch, and the commit the agent's checkout was last seen at.
type Target struct {
	RepositoryURL string
	Branch        string
	// HeadSHA is the agent's last seen head commit, empty before the checkout was read.
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

// ListTargets is every branch a live agent sits on away from its workflow's base branch, with
// each agent's head commit, by repository, branch and commit.
func (s *PullRequestSyncStore) ListTargets(ctx context.Context) ([]Target, error) {
	rows, err := s.queries.ListSyncTargets(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the branches live agents sit on: %w", err)
	}

	return xslice.Map(rows, func(row *models.ListSyncTargetsRow) Target {
		return Target{
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
