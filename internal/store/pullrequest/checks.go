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

// checkReading is what the forge last listed for the checks on one head commit of a pull request.
type checkReading struct {
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
	// Truncated is whether the forge listed only some of the checks, which keeps the ones it left out.
	Truncated bool
	ReadAt    time.Time
}

// SetChecks records a read of a head commit's checks, writing only the checks that changed.
func (s *CheckStore) SetChecks(ctx context.Context, params SetChecksParams) error {
	return setChecks(ctx, s.queries, params)
}

func setChecks(ctx context.Context, queries *models.Queries, params SetChecksParams) error {
	readingID, err := queries.RecordCheckReading(ctx, models.RecordCheckReadingParams{
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

	listed := checkRows(params.Checks)
	encoded, err := json.Marshal(listed)
	if err != nil {
		return fmt.Errorf("failed to encode the checks listed on %s: %w", params.HeadSHA, err)
	}
	if err := queries.UpsertChecks(ctx, models.UpsertChecksParams{ReadingID: readingID, PullRequestID: params.PullRequestID, Checks: encoded}); err != nil {
		return fmt.Errorf("failed to write the checks listed on %s: %w", params.HeadSHA, err)
	}

	if params.Truncated {
		return nil
	}

	if err := queries.DeleteChecksExcept(ctx, models.DeleteChecksExceptParams{
		ReadingID: readingID,
		Kept:      xslice.Map(listed, func(row checkRow) string { return row.ProviderID }),
	}); err != nil {
		return fmt.Errorf("failed to remove the checks no longer listed on %s: %w", params.HeadSHA, err)
	}

	return nil
}

// ListOfPullRequest is every check reading on a pull request with its checks, oldest head first.
func (s *CheckStore) ListOfPullRequest(ctx context.Context, pullRequestID string) ([]checkReading, error) {
	if !store.IsID(pullRequestID) {
		return nil, nil
	}

	rows, err := s.queries.ListCheckReadings(ctx, pullRequestID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the check readings of pull request %s: %w", pullRequestID, err)
	}

	return withChecks(ctx, s.queries, rows)
}

// latestReadings is the latest check reading of each of the pull requests that has one, by pull request.
func latestReadings(ctx context.Context, queries *models.Queries, pullRequestIDs []string) (map[string]checkReading, error) {
	storable := store.KeepIDs(pullRequestIDs)
	if len(storable) == 0 {
		return nil, nil
	}

	rows, err := queries.ListLatestCheckReadings(ctx, storable)
	if err != nil {
		return nil, fmt.Errorf("failed to list the latest check readings of %d pull request(s): %w", len(storable), err)
	}
	readings, err := withChecks(ctx, queries, rows)
	if err != nil {
		return nil, err
	}

	byPullRequest := make(map[string]checkReading, len(readings))
	for i, row := range rows {
		byPullRequest[row.PullRequestID] = readings[i]
	}

	return byPullRequest, nil
}

// withChecks is each reading with the checks listed on it, in the order given.
func withChecks(ctx context.Context, queries *models.Queries, rows []*models.PullRequestCheckReading) ([]checkReading, error) {
	if len(rows) == 0 {
		return nil, nil
	}

	ids := xslice.Map(rows, func(row *models.PullRequestCheckReading) string { return row.ID })
	checks, err := queries.ListChecksOfReadings(ctx, ids)
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

	return xslice.Map(rows, func(row *models.PullRequestCheckReading) checkReading {
		return checkReading{
			ID:          row.ID,
			HeadSHA:     row.HeadSha,
			Blank:       row.Blank,
			FirstReadAt: row.FirstReadAt,
			ReadAt:      row.ReadAt,
			Checks:      byReading[row.ID],
		}
	}), nil
}

// rememberedChecksOf is where the checks on the latest read head stand, rolled up from its rows.
func rememberedChecksOf(reading checkReading) forge.RememberedAnswer[forge.Checks] {
	return forge.RememberedAnswer[forge.Checks]{
		Value:              forge.RollupOf(reading.Checks),
		HeadSHA:            reading.HeadSHA,
		WasLatestReadBlank: reading.Blank,
	}
}

// checkRow is one check as the set-based upsert reads it from JSON.
type checkRow struct {
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

// checkRows is each check listed once, the last listing of a provider id winning.
func checkRows(checks []forge.Check) []checkRow {
	at := map[string]int{}
	var rows []checkRow
	for _, check := range checks {
		row := checkRow{
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
