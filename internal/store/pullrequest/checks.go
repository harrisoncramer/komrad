package pullrequest

import (
	"context"
	"fmt"
	"time"

	"komrad/db/models"
	"komrad/internal/forge"
	"komrad/internal/store"
	"komrad/lib/xslice"
)

// CheckReading is what the forge last listed for the checks on one head commit of a pull request.
type CheckReading struct {
	ID      string
	HeadSHA string
	// Blank is whether the last read listed no checks, which keeps the checks listed before it.
	Blank       bool
	FirstReadAt time.Time
	ReadAt      time.Time
	Checks      []forge.Check
}

// CheckStore holds the checks the forge listed on each head commit of a pull request.
type CheckStore struct {
	queries *models.Queries
}

// NewCheckStore builds the store over a pool or transaction.
func NewCheckStore(db store.DBTX) *CheckStore {
	return &CheckStore{queries: models.New(db)}
}

// SetChecksParams is one read of the checks on a pull request's head commit.
type SetChecksParams struct {
	PullRequestID string
	HeadSHA       string
	Checks        []forge.Check
	ReadAt        time.Time
}

// SetChecks records a read of a head commit's checks, writing only the checks that changed.
func (s *CheckStore) SetChecks(ctx context.Context, params SetChecksParams) error {
	readingID, err := s.queries.RecordCheckReading(ctx, models.RecordCheckReadingParams{
		PullRequestID: params.PullRequestID,
		HeadSha:       params.HeadSHA,
		Blank:         len(params.Checks) == 0,
		ReadAt:        params.ReadAt,
	})
	if err != nil {
		return fmt.Errorf("failed to record the check reading of %s on pull request %s: %w", params.HeadSHA, params.PullRequestID, err)
	}

	if len(params.Checks) == 0 {
		return nil
	}

	for _, check := range params.Checks {
		if err := s.queries.UpsertCheck(ctx, models.UpsertCheckParams{
			ReadingID:  readingID,
			ProviderID: check.ProviderID,
			Kind:       string(check.Kind),
			Source:     check.Source,
			Name:       check.Name,
			State:      string(check.State),
			Conclusion: check.Conclusion,
			StartedAt:  check.StartedAt,
			FinishedAt: check.FinishedAt,
			Required:   check.Required,
			Url:        check.URL,
		}); err != nil {
			return fmt.Errorf("failed to write check %s: %w", check.ProviderID, err)
		}
	}

	if err := s.queries.DeleteChecksExcept(ctx, models.DeleteChecksExceptParams{
		ReadingID: readingID,
		Kept:      xslice.Map(params.Checks, func(check forge.Check) string { return check.ProviderID }),
	}); err != nil {
		return fmt.Errorf("failed to remove the checks no longer listed on %s: %w", params.HeadSHA, err)
	}

	return nil
}

// ListOfPullRequest is every check reading on a pull request with its checks, oldest head first.
func (s *CheckStore) ListOfPullRequest(ctx context.Context, pullRequestID string) ([]CheckReading, error) {
	if !store.IsID(pullRequestID) {
		return nil, nil
	}

	rows, err := s.queries.ListCheckReadings(ctx, pullRequestID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the check readings of pull request %s: %w", pullRequestID, err)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	ids := xslice.Map(rows, func(row *models.PullRequestCheckReading) string { return row.ID })
	checks, err := s.queries.ListChecksOfReadings(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to list the checks of %d reading(s): %w", len(ids), err)
	}

	byReading := map[string][]forge.Check{}
	for _, row := range checks {
		byReading[row.ReadingID] = append(byReading[row.ReadingID], forge.Check{
			ProviderID: row.ProviderID,
			Kind:       forge.CheckKind(row.Kind),
			Source:     row.Source,
			Name:       row.Name,
			State:      forge.CheckState(row.State),
			Conclusion: row.Conclusion,
			StartedAt:  row.StartedAt,
			FinishedAt: row.FinishedAt,
			Required:   row.Required,
			URL:        row.Url,
		})
	}

	return xslice.Map(rows, func(row *models.PullRequestCheckReading) CheckReading {
		return CheckReading{
			ID:          row.ID,
			HeadSHA:     row.HeadSha,
			Blank:       row.Blank,
			FirstReadAt: row.FirstReadAt,
			ReadAt:      row.ReadAt,
			Checks:      byReading[row.ID],
		}
	}), nil
}
