package pullrequest

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"komrad/db/models"
	"komrad/internal/forge"
	"komrad/internal/store"
	"komrad/lib/xslice"
)

// StatusCheckReading is what the forge last listed for the checks on one head commit of a pull request.
type StatusCheckReading struct {
	ID      string
	HeadSHA string
	// Blank is whether the last read listed no checks, which keeps the checks listed before it.
	Blank        bool
	FirstReadAt  time.Time
	ReadAt       time.Time
	StatusChecks []forge.StatusCheck
}

// StatusCheckStore holds the checks the forge listed on each head commit of a pull request.
type StatusCheckStore struct {
	queries *models.Queries
}

// NewStatusCheckStore builds the store over a pool or transaction.
func NewStatusCheckStore(db store.DBTX) *StatusCheckStore {
	return &StatusCheckStore{queries: models.New(db)}
}

// SetStatusChecksParams is one read of the checks on a pull request's head commit.
type SetStatusChecksParams struct {
	PullRequestID string
	HeadSHA       string
	StatusChecks  []forge.StatusCheck
	// Truncated is whether the forge listed only some of the checks, which keeps the ones it left out.
	Truncated bool
	ReadAt    time.Time
}

// SetStatusChecks records a read of a head commit's checks, writing only the checks that changed.
func (s *StatusCheckStore) SetStatusChecks(ctx context.Context, params SetStatusChecksParams) error {
	return setStatusChecks(ctx, s.queries, params)
}

func setStatusChecks(ctx context.Context, queries *models.Queries, params SetStatusChecksParams) error {
	readingID, err := queries.RecordStatusCheckReading(ctx, models.RecordStatusCheckReadingParams{
		PullRequestID: params.PullRequestID,
		HeadSha:       params.HeadSHA,
		Blank:         len(params.StatusChecks) == 0,
		ReadAt:        params.ReadAt,
	})
	if err != nil {
		return fmt.Errorf("failed to record the check reading of %s on pull request %s: %w", params.HeadSHA, params.PullRequestID, err)
	}

	if len(params.StatusChecks) == 0 {
		return nil
	}

	listed := statusCheckRows(params.StatusChecks)
	encoded, err := json.Marshal(listed)
	if err != nil {
		return fmt.Errorf("failed to encode the checks listed on %s: %w", params.HeadSHA, err)
	}
	if err := queries.UpsertStatusChecks(ctx, models.UpsertStatusChecksParams{ReadingID: readingID, Checks: encoded}); err != nil {
		return fmt.Errorf("failed to write the checks listed on %s: %w", params.HeadSHA, err)
	}

	if params.Truncated {
		return nil
	}

	if err := queries.DeleteStatusChecksExcept(ctx, models.DeleteStatusChecksExceptParams{
		ReadingID: readingID,
		Kept:      xslice.Map(listed, func(row statusCheckRow) string { return row.ProviderID }),
	}); err != nil {
		return fmt.Errorf("failed to remove the checks no longer listed on %s: %w", params.HeadSHA, err)
	}

	return nil
}

// ListOfPullRequest is every check reading on a pull request with its checks, oldest head first.
func (s *StatusCheckStore) ListOfPullRequest(ctx context.Context, pullRequestID string) ([]StatusCheckReading, error) {
	if !store.IsID(pullRequestID) {
		return nil, nil
	}

	rows, err := s.queries.ListStatusCheckReadings(ctx, pullRequestID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the check readings of pull request %s: %w", pullRequestID, err)
	}

	return withStatusChecks(ctx, s.queries, rows)
}

// GetLatestOfPullRequest is the check reading on a pull request's latest read head with its checks,
// or ErrNotFound when its checks have never been read.
func (s *StatusCheckStore) GetLatestOfPullRequest(ctx context.Context, pullRequestID string) (*StatusCheckReading, error) {
	readings, err := latestReadings(ctx, s.queries, []string{pullRequestID})
	if err != nil {
		return nil, err
	}

	reading, ok := readings[pullRequestID]
	if !ok {
		return nil, store.ErrNotFound.Saying("no check reading on pull request %s", pullRequestID)
	}

	return &reading, nil
}

// latestReadings is the latest check reading of each of the pull requests that has one, by pull request.
func latestReadings(ctx context.Context, queries *models.Queries, pullRequestIDs []string) (map[string]StatusCheckReading, error) {
	storable := store.KeepIDs(pullRequestIDs)
	if len(storable) == 0 {
		return nil, nil
	}

	rows, err := queries.ListLatestStatusCheckReadings(ctx, storable)
	if err != nil {
		return nil, fmt.Errorf("failed to list the latest check readings of %d pull request(s): %w", len(storable), err)
	}
	readings, err := withStatusChecks(ctx, queries, rows)
	if err != nil {
		return nil, err
	}

	byPullRequest := make(map[string]StatusCheckReading, len(readings))
	for i, row := range rows {
		byPullRequest[row.PullRequestID] = readings[i]
	}

	return byPullRequest, nil
}

// withStatusChecks is each reading with the checks listed on it, in the order given.
func withStatusChecks(ctx context.Context, queries *models.Queries, rows []*models.PullRequestCheckReading) ([]StatusCheckReading, error) {
	if len(rows) == 0 {
		return nil, nil
	}

	ids := xslice.Map(rows, func(row *models.PullRequestCheckReading) string { return row.ID })
	checks, err := queries.ListStatusChecksOfReadings(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to list the checks of %d reading(s): %w", len(ids), err)
	}

	byReading := map[string][]forge.StatusCheck{}
	for _, row := range checks {
		byReading[row.ReadingID] = append(byReading[row.ReadingID], forge.StatusCheck{
			ProviderID: row.ProviderID,
			Kind:       forge.StatusCheckKind(row.Kind),
			Source:     row.Source,
			Name:       row.Name,
			State:      forge.StatusCheckState(row.State),
			Conclusion: row.Conclusion,
			StartedAt:  row.StartedAt,
			FinishedAt: row.FinishedAt,
			Required:   row.Required,
			URL:        row.Url,
		})
	}

	return xslice.Map(rows, func(row *models.PullRequestCheckReading) StatusCheckReading {
		return StatusCheckReading{
			ID:           row.ID,
			HeadSHA:      row.HeadSha,
			Blank:        row.Blank,
			FirstReadAt:  row.FirstReadAt,
			ReadAt:       row.ReadAt,
			StatusChecks: byReading[row.ID],
		}
	}), nil
}

// rememberedChecksOf is where the checks on the latest read head stand, rolled up from its rows.
func rememberedChecksOf(reading StatusCheckReading) forge.RememberedAnswer[forge.Checks] {
	return forge.RememberedAnswer[forge.Checks]{
		Value:              forge.RollupOf(reading.StatusChecks),
		HeadSHA:            reading.HeadSHA,
		WasLatestReadBlank: reading.Blank,
	}
}

// statusCheckRow is one check as the set-based upsert reads it from JSON.
type statusCheckRow struct {
	ProviderID string     `json:"provider_id"`
	Kind       string     `json:"kind"`
	Source     string     `json:"source"`
	Name       string     `json:"name"`
	State      string     `json:"state"`
	Conclusion string     `json:"conclusion"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Required   bool       `json:"required"`
	URL        string     `json:"url"`
}

// statusCheckRows is each check listed once, the last listing of a provider id winning.
func statusCheckRows(checks []forge.StatusCheck) []statusCheckRow {
	at := map[string]int{}
	var rows []statusCheckRow
	for _, check := range checks {
		row := statusCheckRow{
			ProviderID: check.ProviderID,
			Kind:       string(check.Kind),
			Source:     check.Source,
			Name:       check.Name,
			State:      string(check.State),
			Conclusion: check.Conclusion,
			StartedAt:  check.StartedAt,
			FinishedAt: check.FinishedAt,
			Required:   check.Required,
			URL:        check.URL,
		}
		if i, ok := at[check.ProviderID]; ok {
			rows[i] = row
			continue
		}
		at[check.ProviderID] = len(rows)
		rows = append(rows, row)
	}

	return rows
}
