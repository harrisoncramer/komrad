package pullrequest

import (
	"context"
	"fmt"
	"time"

	"komrad/db/models"
	"komrad/internal/forge"
	"komrad/internal/store"
	"komrad/lib/xslice"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	"github.com/google/uuid"
)

// StatusCheckReading is what the forge last listed for the checks on one head commit of a pull request.
type StatusCheckReading struct {
	ID      uuid.UUID
	HeadSHA string
	// Blank is whether the last read listed no checks, which keeps the checks listed before it.
	Blank        bool
	FirstReadAt  time.Time
	ReadAt       time.Time
	StatusChecks []forge.StatusCheck
	// CheckIDs is the id komrad keeps each check under, by the id the forge gives it.
	CheckIDs map[string]uuid.UUID
	// Logs is the log kept for each check that has one, by the id komrad keeps the check under, filled only
	// for the readings of one pull request.
	Logs map[uuid.UUID]CheckLog
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
	PullRequestID uuid.UUID
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
	checkRows, err := statusCheckRows(readingID, checks)
	if err != nil {
		return err
	}
	if err := queries.UpsertStatusChecks(ctx, checkRows).Close(); err != nil {
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
func setStatusCheckSteps(ctx context.Context, queries *models.Queries, readingID uuid.UUID, params SetStatusChecksParams) error {
	headSHA := params.HeadSHA
	listed, err := statusCheckStepRows(readingID, params.StatusChecks)
	if err != nil {
		return err
	}
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
func (s *StatusCheckStore) ListOfPullRequest(ctx context.Context, pullRequestID uuid.UUID) ([]StatusCheckReading, error) {
	rows, err := s.queries.ListStatusCheckReadings(ctx, pullRequestID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the check readings of pull request %s: %w", pullRequestID, err)
	}

	readings, err := withStatusChecks(ctx, s.queries, rows)
	if err != nil {
		return nil, err
	}

	logs, err := s.ListCheckLogsOfReadings(ctx, xslice.Map(readings, func(reading StatusCheckReading) uuid.UUID { return reading.ID }))
	if err != nil {
		return nil, err
	}

	byCheck := make(map[uuid.UUID]CheckLog, len(logs))
	for _, log := range logs {
		byCheck[log.CheckID] = log
	}
	for i, reading := range readings {
		readings[i].Logs = map[uuid.UUID]CheckLog{}
		for _, checkID := range reading.CheckIDs {
			if log, ok := byCheck[checkID]; ok {
				readings[i].Logs[checkID] = log
			}
		}
	}

	return readings, nil
}

// headReadings is each pull request's reading of its head, or its latest reading when the head was never read.
func headReadings(ctx context.Context, queries *models.Queries, pullRequestIDs []uuid.UUID) (map[uuid.UUID]StatusCheckReading, error) {
	if len(pullRequestIDs) == 0 {
		return map[uuid.UUID]StatusCheckReading{}, nil
	}

	rows, err := queries.ListHeadStatusCheckReadings(ctx, pullRequestIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to list the latest check readings of %d pull request(s): %w", len(pullRequestIDs), err)
	}
	readings, err := withStatusChecks(ctx, queries, rows)
	if err != nil {
		return nil, err
	}

	byPullRequest := make(map[uuid.UUID]StatusCheckReading, len(readings))
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

	ids := xslice.Map(rows, func(row *models.PullRequestCheckReading) uuid.UUID { return row.ID })
	checks, err := queries.ListStatusChecksOfReadings(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to list the checks of %d reading(s): %w", len(ids), err)
	}

	steps, err := queries.ListStatusCheckStepsOfReadings(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to list the check steps of %d reading(s): %w", len(ids), err)
	}

	byCheck := map[uuid.UUID][]forge.StatusCheckStep{}
	for _, row := range steps {
		state, err := fromDBToServiceStatusCheckState(row.State)
		if err != nil {
			return nil, err
		}
		byCheck[row.CheckID] = append(byCheck[row.CheckID], forge.StatusCheckStep{
			Number:     int(row.Number),
			Name:       row.Name,
			State:      state,
			Conclusion: row.Conclusion,
			StartedAt:  row.StartedAt,
			FinishedAt: row.FinishedAt,
		})
	}

	byReading := map[uuid.UUID][]forge.StatusCheck{}
	checkIDs := map[uuid.UUID]map[string]uuid.UUID{}
	for _, row := range checks {
		if checkIDs[row.ReadingID] == nil {
			checkIDs[row.ReadingID] = map[string]uuid.UUID{}
		}
		kind, err := fromDBToServiceStatusCheckKind(row.Kind)
		if err != nil {
			return nil, err
		}
		state, err := fromDBToServiceStatusCheckState(row.State)
		if err != nil {
			return nil, err
		}
		checkIDs[row.ReadingID][row.ProviderID] = row.ID
		byReading[row.ReadingID] = append(byReading[row.ReadingID], forge.StatusCheck{
			ProviderID:   row.ProviderID,
			Kind:         kind,
			Source:       row.Source,
			Name:         row.Name,
			State:        state,
			Conclusion:   row.Conclusion,
			StartedAt:    row.StartedAt,
			FinishedAt:   row.FinishedAt,
			Required:     row.Required,
			URL:          row.Url,
			Steps:        byCheck[row.ID],
			RunID:        row.RunID,
			RunEvent:     row.RunEvent,
			RunStartedAt: row.RunStartedAt,
			JobID:        row.JobID,
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
			CheckIDs:     checkIDs[row.ID],
		}
	}), nil
}

// rememberedChecksOf is where the checks on the latest read head stand, rolled up from its rows, and unspecified before any read.
func rememberedChecksOf(reading StatusCheckReading) forge.RememberedAnswer[pullrequestv1.Checks] {
	if reading.HeadSHA == "" {
		return forge.RememberedAnswer[pullrequestv1.Checks]{}
	}

	return forge.RememberedAnswer[pullrequestv1.Checks]{
		Value:              forge.RollupOf(reading.StatusChecks),
		HeadSHA:            reading.HeadSHA,
		WasLatestReadBlank: reading.Blank,
	}
}

// statusCheckRows is each check as a row of the reading.
func statusCheckRows(readingID uuid.UUID, checks []forge.StatusCheck) ([]models.UpsertStatusChecksParams, error) {
	return xslice.TryMap(checks, func(check forge.StatusCheck) (models.UpsertStatusChecksParams, error) {
		kind, err := fromServiceToDBStatusCheckKind(check.Kind)
		if err != nil {
			return models.UpsertStatusChecksParams{}, err
		}
		state, err := fromServiceToDBStatusCheckState(check.State)
		if err != nil {
			return models.UpsertStatusChecksParams{}, err
		}

		return models.UpsertStatusChecksParams{
			ReadingID:    readingID,
			ProviderID:   check.ProviderID,
			Kind:         kind,
			Source:       check.Source,
			Name:         check.Name,
			State:        state,
			Conclusion:   check.Conclusion,
			StartedAt:    check.StartedAt,
			FinishedAt:   check.FinishedAt,
			Required:     check.Required,
			Url:          check.URL,
			RunID:        check.RunID,
			RunEvent:     check.RunEvent,
			RunStartedAt: check.RunStartedAt,
			JobID:        check.JobID,
		}, nil
	})
}

// providerIDOf is the id the forge gives the check.
func providerIDOf(check forge.StatusCheck) string {
	return check.ProviderID
}

// statusCheckStepRows is every step of the checks as a row of the reading, each step of a check listed once,
// the last listing of a number winning.
func statusCheckStepRows(readingID uuid.UUID, checks []forge.StatusCheck) ([]models.UpsertStatusCheckStepsParams, error) {
	rows := []models.UpsertStatusCheckStepsParams{}
	for _, check := range checks {
		stepRows, err := xslice.TryMap(xslice.LastBy(check.Steps, numberOf), func(step forge.StatusCheckStep) (models.UpsertStatusCheckStepsParams, error) {
			state, err := fromServiceToDBStatusCheckState(step.State)
			if err != nil {
				return models.UpsertStatusCheckStepsParams{}, err
			}

			return models.UpsertStatusCheckStepsParams{
				ReadingID:  readingID,
				ProviderID: check.ProviderID,
				Number:     int32(step.Number),
				Name:       step.Name,
				State:      state,
				Conclusion: step.Conclusion,
				StartedAt:  step.StartedAt,
				FinishedAt: step.FinishedAt,
			}, nil
		})
		if err != nil {
			return nil, err
		}
		rows = append(rows, stepRows...)
	}

	return rows, nil
}

// numberOf is the step's number on its check run.
func numberOf(step forge.StatusCheckStep) int {
	return step.Number
}
