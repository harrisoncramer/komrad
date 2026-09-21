package pullrequest

import (
	"context"
	"fmt"
	"time"

	"weave/db"
	"weave/db/models"
	"weave/internal/store"
	"weave/lib/xptr"
	"weave/lib/xslice"
)

// Sync is where the sync is for one repository.
type Sync struct {
	RepositoryURL string
	// UpdatedThrough is the newest forge changed_at the changes have been read through, nil
	// until the first sweep has completed.
	UpdatedThrough *time.Time
	// ListedAt is the last time page one of the repository's own listing was read and written.
	ListedAt *time.Time
	// SweepCursor is where the first sweep in progress reads its next page from, empty for
	// page one and nil between sweeps.
	SweepCursor *string
	// SweepNewest is the moment the sweep in progress covers up to, which becomes the mark when
	// it completes.
	SweepNewest *time.Time
	// ReadAt is the last time any request was made for the repository.
	ReadAt *time.Time
	// Calls is how many requests the most recent pass made for the repository.
	Calls int
}

// IsSweeping says whether a first sweep is in progress.
func (s Sync) IsSweeping() bool {
	return s.SweepCursor != nil
}

// IsSwept says whether a first sweep has completed.
func (s Sync) IsSwept() bool {
	return s.UpdatedThrough != nil
}

// BranchScan is where the one-time listing of a branch's pull requests is.
type BranchScan struct {
	RepositoryURL string
	Branch        string
	// NextCursor is where the listing reads its next page from, empty for the first.
	NextCursor string
	// CompletedAt is when the listing answered its last page, nil while it has not.
	CompletedAt *time.Time
}

// SyncStore is where the sync is for every repository and branch it has listed.
type SyncStore struct {
	queries *models.Queries
}

// NewSyncStore builds a sync store over a pool or transaction.
func NewSyncStore(db store.DBTX) *SyncStore {
	return &SyncStore{queries: models.New(db)}
}

// ListStaleStandingsParams says how long a read of the checks and mergeability is trusted: a
// pending run is read again after PendingAfter, so is a head with no checks or no computed
// mergeability while the row changed within SettleWithin, and any row after SettledAfter.
type ListStaleStandingsParams struct {
	RepositoryURL string
	Now           time.Time
	PendingAfter  time.Duration
	SettleWithin  time.Duration
	SettledAfter  time.Duration
}

// ListStaleStandings is every open pull request on a branch a live agent sits on in a
// repository whose review decision has not been read since it last changed, or whose checks
// were read for another head or have aged past what the params trust, newest change first.
func (s *SyncStore) ListStaleStandings(ctx context.Context, params ListStaleStandingsParams) ([]PullRequest, error) {
	rows, err := s.queries.ListStaleStandings(ctx, models.ListStaleStandingsParams{
		RepositoryUrl: params.RepositoryURL,
		PendingBefore: params.Now.Add(-params.PendingAfter),
		ChangedSince:  params.Now.Add(-params.SettleWithin),
		SettledBefore: params.Now.Add(-params.SettledAfter),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list the stale standings of %s: %w", params.RepositoryURL, err)
	}

	return xslice.Map(rows, fromRow), nil
}

// ListUnscannedBranches is every branch a live agent sits on in a repository whose one-time
// listing has not completed.
func (s *SyncStore) ListUnscannedBranches(ctx context.Context, repositoryURL string) ([]string, error) {
	rows, err := s.queries.ListUnscannedBranches(ctx, &repositoryURL)
	if err != nil {
		return nil, fmt.Errorf("failed to list the unscanned branches of %s: %w", repositoryURL, err)
	}

	branches := make([]string, 0, len(rows))
	for _, row := range rows {
		if row != nil {
			branches = append(branches, *row)
		}
	}

	return branches, nil
}

// GetBranchScan is where the listing of a branch is, or ErrNotFound when it has not started.
func (s *SyncStore) GetBranchScan(ctx context.Context, repositoryURL, branch string) (*BranchScan, error) {
	row, err := s.queries.GetBranchScan(ctx, models.GetBranchScanParams{RepositoryUrl: repositoryURL, Branch: branch})
	if db.IsNoRows(err) {
		return nil, store.ErrNotFound.Saying("%s of %s has not been scanned", branch, repositoryURL)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get the scan of %s of %s: %w", branch, repositoryURL, err)
	}

	return &BranchScan{
		RepositoryURL: row.RepositoryUrl,
		Branch:        row.Branch,
		NextCursor:    row.NextCursor,
		CompletedAt:   row.CompletedAt,
	}, nil
}

// SetBranchScanCursor records where a branch's listing reads its next page from, starting the
// scan when it has not.
func (s *SyncStore) SetBranchScanCursor(ctx context.Context, repositoryURL, branch, cursor string) error {
	err := s.queries.SetBranchScanCursor(ctx, models.SetBranchScanCursorParams{
		RepositoryUrl: repositoryURL,
		Branch:        branch,
		NextCursor:    cursor,
	})
	if err != nil {
		return fmt.Errorf("failed to advance the scan of %s of %s: %w", branch, repositoryURL, err)
	}

	return nil
}

// CompleteBranchScan records that a branch's listing answered its last page.
func (s *SyncStore) CompleteBranchScan(ctx context.Context, repositoryURL, branch, cursor string) error {
	err := s.queries.CompleteBranchScan(ctx, models.CompleteBranchScanParams{
		RepositoryUrl: repositoryURL,
		Branch:        branch,
		NextCursor:    cursor,
	})
	if err != nil {
		return fmt.Errorf("failed to complete the scan of %s of %s: %w", branch, repositoryURL, err)
	}

	return nil
}

// EnsureSync makes sure a repository has a sync row, leaving one it has alone.
func (s *SyncStore) EnsureSync(ctx context.Context, repositoryURL string) error {
	if err := s.queries.EnsurePullRequestSync(ctx, repositoryURL); err != nil {
		return fmt.Errorf("failed to ensure the sync row for %s: %w", repositoryURL, err)
	}

	return nil
}

// GetSync is where the sync is for a repository, or ErrNotFound.
func (s *SyncStore) GetSync(ctx context.Context, repositoryURL string) (*Sync, error) {
	row, err := s.queries.GetPullRequestSync(ctx, repositoryURL)
	if db.IsNoRows(err) {
		return nil, store.ErrNotFound.Saying("%s has no sync row", repositoryURL)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get the sync row for %s: %w", repositoryURL, err)
	}

	return &Sync{
		RepositoryURL:  row.RepositoryUrl,
		UpdatedThrough: row.UpdatedThrough,
		ListedAt:       row.ListedAt,
		SweepCursor:    row.SweepCursor,
		SweepNewest:    row.SweepNewest,
		ReadAt:         row.ReadAt,
		Calls:          int(row.Calls),
	}, nil
}

// ResetSyncCalls starts a pass's count of requests for a repository at zero.
func (s *SyncStore) ResetSyncCalls(ctx context.Context, repositoryURL string) error {
	if err := s.queries.ResetPullRequestSyncCalls(ctx, repositoryURL); err != nil {
		return fmt.Errorf("failed to reset the call count for %s: %w", repositoryURL, err)
	}

	return nil
}

// RecordSyncRequest counts one request made for a repository and stamps when.
func (s *SyncStore) RecordSyncRequest(ctx context.Context, repositoryURL string) error {
	if err := s.queries.RecordPullRequestSyncRequest(ctx, repositoryURL); err != nil {
		return fmt.Errorf("failed to count a request for %s: %w", repositoryURL, err)
	}

	return nil
}

// StartSweepParams is what page one of a first sweep answered with, and when.
type StartSweepParams struct {
	RepositoryURL string
	// Newest is the moment the sweep covers up to, when page one was read or its first row's
	// stamp when the forge's clock is ahead.
	Newest time.Time
	// ListedAt is when page one was read.
	ListedAt time.Time
}

// StartSweep records a first sweep beginning at page one.
func (s *SyncStore) StartSweep(ctx context.Context, params StartSweepParams) error {
	err := s.queries.StartPullRequestSweep(ctx, models.StartPullRequestSweepParams{
		RepositoryUrl: params.RepositoryURL,
		SweepNewest:   xptr.Ptr(params.Newest),
		ListedAt:      xptr.Ptr(params.ListedAt),
	})
	if err != nil {
		return fmt.Errorf("failed to start a sweep of %s: %w", params.RepositoryURL, err)
	}

	return nil
}

// AdvanceSweep records where the sweep in progress reads its next page from.
func (s *SyncStore) AdvanceSweep(ctx context.Context, repositoryURL, cursor string) error {
	err := s.queries.AdvancePullRequestSweep(ctx, models.AdvancePullRequestSweepParams{
		RepositoryUrl: repositoryURL,
		SweepCursor:   xptr.Ptr(cursor),
	})
	if err != nil {
		return fmt.Errorf("failed to advance the sweep of %s: %w", repositoryURL, err)
	}

	return nil
}

// CompleteSweep promotes the sweep in progress: the high-water mark moves to its newest stamp
// unless the mark is already newer, and the sweep columns clear.
func (s *SyncStore) CompleteSweep(ctx context.Context, repositoryURL string) error {
	if err := s.queries.CompletePullRequestSweep(ctx, repositoryURL); err != nil {
		return fmt.Errorf("failed to complete the sweep of %s: %w", repositoryURL, err)
	}

	return nil
}

// ResetSweeps hands repositories back to a first sweep, clearing their high-water marks and
// any sweep in progress.
func (s *SyncStore) ResetSweeps(ctx context.Context, repositoryURLs []string) error {
	if err := s.queries.ResetPullRequestSweep(ctx, repositoryURLs); err != nil {
		return fmt.Errorf("failed to reset the sweeps of %v: %w", repositoryURLs, err)
	}

	return nil
}

// AdvanceMark moves a repository's high-water mark to a moment the changes have been read
// through, unless the mark is already newer.
func (s *SyncStore) AdvanceMark(ctx context.Context, repositoryURL string, through time.Time) error {
	err := s.queries.AdvancePullRequestSyncMark(ctx, models.AdvancePullRequestSyncMarkParams{
		RepositoryUrl:  repositoryURL,
		UpdatedThrough: through,
	})
	if err != nil {
		return fmt.Errorf("failed to advance the mark of %s: %w", repositoryURL, err)
	}

	return nil
}

// MarkListed records when page one of a repository's own listing was last read and written.
func (s *SyncStore) MarkListed(ctx context.Context, repositoryURL string, at time.Time) error {
	err := s.queries.MarkPullRequestSyncListed(ctx, models.MarkPullRequestSyncListedParams{
		RepositoryUrl: repositoryURL,
		ListedAt:      xptr.Ptr(at),
	})
	if err != nil {
		return fmt.Errorf("failed to record the listing of %s: %w", repositoryURL, err)
	}

	return nil
}
