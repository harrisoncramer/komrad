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

	checks := xslice.LastBy(params.StatusChecks, providerIDOf)
	if err := queries.UpsertStatusChecks(ctx, statusCheckRows(readingID, checks)).Close(); err != nil {
		return fmt.Errorf("failed to write the checks listed on %s: %w", params.HeadSHA, err)
	}

	params.StatusChecks = checks
	if err := setStatusCheckSteps(ctx, queries, readingID, params); err != nil {
		return err
	}

	if params.Truncated {
		return nil
	}

	if err := queries.DeleteStatusChecksExcept(ctx, models.DeleteStatusChecksExceptParams{
		ReadingID: readingID,
		Kept:      xslice.Map(checks, providerIDOf),
	}); err != nil {
		return fmt.Errorf("failed to remove the checks no longer listed on %s: %w", params.HeadSHA, err)
	}

	return nil
}

// setStatusCheckSteps writes the steps listed on each check of a reading, writing only the steps that changed,
// and removes the steps no longer listed on checks whose steps were listed whole.
func setStatusCheckSteps(ctx context.Context, queries *models.Queries, readingID string, params SetStatusChecksParams) error {
	headSHA := params.HeadSHA
	listed := statusCheckStepRows(readingID, params.StatusChecks)
	if err := queries.UpsertStatusCheckSteps(ctx, listed).Close(); err != nil {
		return fmt.Errorf("failed to write the check steps listed on %s: %w", headSHA, err)
	}

	complete := xslice.Map(
		xslice.Filter(params.StatusChecks, func(check forge.StatusCheck) bool { return !check.StepsTruncated }),
		providerIDOf,
	)
	if err := queries.DeleteStatusCheckStepsExcept(ctx, models.DeleteStatusCheckStepsExceptParams{
		ReadingID:       readingID,
		Complete:        complete,
		KeptProviderIds: xslice.Map(listed, func(row models.UpsertStatusCheckStepsParams) string { return row.ProviderID }),
		KeptNumbers:     xslice.Map(listed, func(row models.UpsertStatusCheckStepsParams) int32 { return row.Number }),
	}); err != nil {
		return fmt.Errorf("failed to remove the check steps no longer listed on %s: %w", headSHA, err)
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
		return map[string]StatusCheckReading{}, nil
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

	steps, err := queries.ListStatusCheckStepsOfReadings(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to list the check steps of %d reading(s): %w", len(ids), err)
	}

	byCheck := map[string][]forge.StatusCheckStep{}
	for _, row := range steps {
		byCheck[row.CheckID] = append(byCheck[row.CheckID], forge.StatusCheckStep{
			Number:     int(row.Number),
			Name:       row.Name,
			State:      forge.StatusCheckState(row.State),
			Conclusion: row.Conclusion,
			StartedAt:  row.StartedAt,
			FinishedAt: row.FinishedAt,
		})
	}

	byReading := map[string][]forge.StatusCheck{}
	for _, row := range checks {
		byReading[row.ReadingID] = append(byReading[row.ReadingID], forge.StatusCheck{
			ProviderID:   row.ProviderID,
			Kind:         forge.StatusCheckKind(row.Kind),
			Source:       row.Source,
			Name:         row.Name,
			State:        forge.StatusCheckState(row.State),
			Conclusion:   row.Conclusion,
			StartedAt:    row.StartedAt,
			FinishedAt:   row.FinishedAt,
			Required:     row.Required,
			URL:          row.Url,
			Steps:        byCheck[row.ID],
			RunID:        row.RunID,
			RunEvent:     row.RunEvent,
			RunStartedAt: row.RunStartedAt,
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

// statusCheckRows is each check as a row of the reading.
func statusCheckRows(readingID string, checks []forge.StatusCheck) []models.UpsertStatusChecksParams {
	return xslice.Map(checks, func(check forge.StatusCheck) models.UpsertStatusChecksParams {
		return models.UpsertStatusChecksParams{
			ReadingID:    readingID,
			ProviderID:   check.ProviderID,
			Kind:         string(check.Kind),
			Source:       check.Source,
			Name:         check.Name,
			State:        string(check.State),
			Conclusion:   check.Conclusion,
			StartedAt:    check.StartedAt,
			FinishedAt:   check.FinishedAt,
			Required:     check.Required,
			Url:          check.URL,
			RunID:        check.RunID,
			RunEvent:     check.RunEvent,
			RunStartedAt: check.RunStartedAt,
		}
	})
}

// providerIDOf is the id the forge gives the check.
func providerIDOf(check forge.StatusCheck) string {
	return check.ProviderID
}

// statusCheckStepRows is every step of the checks as a row of the reading, each step of a check listed once,
// the last listing of a number winning.
func statusCheckStepRows(readingID string, checks []forge.StatusCheck) []models.UpsertStatusCheckStepsParams {
	rows := []models.UpsertStatusCheckStepsParams{}
	for _, check := range checks {
		rows = append(rows, xslice.Map(xslice.LastBy(check.Steps, numberOf), func(step forge.StatusCheckStep) models.UpsertStatusCheckStepsParams {
			return models.UpsertStatusCheckStepsParams{
				ReadingID:  readingID,
				ProviderID: check.ProviderID,
				Number:     int32(step.Number),
				Name:       step.Name,
				State:      string(step.State),
				Conclusion: step.Conclusion,
				StartedAt:  step.StartedAt,
				FinishedAt: step.FinishedAt,
			}
		})...)
	}

	return rows
}

// numberOf is the step's number on its check run.
func numberOf(step forge.StatusCheckStep) int {
	return step.Number
}
