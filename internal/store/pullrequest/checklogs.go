package pullrequest

import (
	"cmp"
	"context"
	"fmt"
	"regexp"
	"slices"
	"time"

	"komrad/db"
	"komrad/db/models"
	"komrad/internal/checklog"
	"komrad/internal/fail"
	"komrad/internal/forge"
	"komrad/internal/store"
	"komrad/lib/xslice"
)

// CheckLogOutcome is where fetching a check's log stands.
type CheckLogOutcome string

const (
	// CheckLogNone is a check that ran in no job.
	CheckLogNone CheckLogOutcome = "none"
	// CheckLogPending is a log still to come.
	CheckLogPending CheckLogOutcome = "pending"
	// CheckLogFetched is a log the forge handed over and komrad kept.
	CheckLogFetched CheckLogOutcome = "fetched"
	// CheckLogTooLarge is a log that ran past the most komrad keeps.
	CheckLogTooLarge CheckLogOutcome = "too_large"
	// CheckLogMissing is a log the forge never had.
	CheckLogMissing CheckLogOutcome = "missing"
	// CheckLogExpired is a log the forge no longer keeps.
	CheckLogExpired CheckLogOutcome = "expired"
	// CheckLogFailed is a log komrad gave up asking for.
	CheckLogFailed CheckLogOutcome = "failed"
)

const (
	// maxSearchHits is the most hits one search answers with.
	maxSearchHits = 1000
	// maxHitRunes is the most characters of a hit's line one search answers with.
	maxHitRunes = 512
)

// ErrInvalidPattern is what a search returns for a pattern that does not compile.
var ErrInvalidPattern = fail.InvalidArgument("the pattern is not a valid regular expression")

// CheckLog is a check's log as komrad last fetched it.
type CheckLog struct {
	CheckID   string
	FetchedAt time.Time
	Outcome   CheckLogOutcome
	// Text is every line of a fetched log followed by a newline, empty when the log was listed.
	Text       string
	LineCount  int
	ErrorCount int
	Steps      []checklog.StepRange
}

// CheckLogLines is a window of a check's log.
type CheckLogLines struct {
	Lines     []string
	LineCount int
}

// CheckLogHit is one line of a check's log a search matched.
type CheckLogHit struct {
	CheckID string
	// Line counts from one.
	Line int
	// StepNumber is the step the line sits in, and 0 for a line in no step.
	StepNumber int
	Text       string
	// Cut is whether the line ran past the most characters a hit carries, and was cut there.
	Cut bool
}

// CheckLogSearch is what a search across logs found.
type CheckLogSearch struct {
	Hits []CheckLogHit
	// Limited is whether the search stopped at the most hits it answers with.
	Limited bool
}

// checkForLogFetch is what fetching a check's log needs to know about the check.
type checkForLogFetch struct {
	ID            string
	State         string
	JobID         *int64
	FinishedAt    *time.Time
	Host          string
	RepositoryURL string
	Steps         []checklog.Step
}

// GetLogOf is where fetching a check's log stands and the log kept for it, which is nil unless the check has
// finished and a log is kept.
func (r StatusCheckReading) GetLogOf(check forge.StatusCheck) (CheckLogOutcome, *CheckLog) {
	if check.JobID == nil {
		return CheckLogNone, nil
	}

	log, ok := r.Logs[r.CheckIDs[check.ProviderID]]
	if !ok || check.State != forge.StatusCheckStateCompleted {
		return CheckLogPending, nil
	}

	return log.Outcome, &log
}

// RecordCheckLog keeps a check's log, replacing the one kept before it with its steps, and must run on a
// store built inside InTx.
func (s *StatusCheckStore) RecordCheckLog(ctx context.Context, log CheckLog) error {
	logID, err := s.queries.RecordCheckLog(ctx, models.RecordCheckLogParams{
		CheckID:    log.CheckID,
		FetchedAt:  log.FetchedAt,
		Outcome:    string(log.Outcome),
		Text:       log.Text,
		LineCount:  int32(log.LineCount),
		ErrorCount: int32(log.ErrorCount),
	})
	if err != nil {
		return fmt.Errorf("failed to record the log of check %s: %w", log.CheckID, err)
	}

	if err := s.queries.DeleteCheckLogSteps(ctx, logID); err != nil {
		return fmt.Errorf("failed to delete the log steps of check %s: %w", log.CheckID, err)
	}

	if len(log.Steps) == 0 {
		return nil
	}

	rows := xslice.Map(log.Steps, func(step checklog.StepRange) models.InsertCheckLogStepsParams {
		return models.InsertCheckLogStepsParams{
			LogID:      logID,
			Number:     int32(step.Number),
			Name:       step.Name,
			Conclusion: step.Conclusion,
			FirstLine:  int32(step.FirstLine),
			LineCount:  int32(step.LineCount),
			ErrorCount: int32(step.ErrorCount),
		}
	})
	if err := s.queries.InsertCheckLogSteps(ctx, rows).Close(); err != nil {
		return fmt.Errorf("failed to insert the log steps of check %s: %w", log.CheckID, err)
	}

	return nil
}

// ListCheckLogsOfReadings is the log kept for each check of the readings that has one, with its steps and
// without its text.
func (s *StatusCheckStore) ListCheckLogsOfReadings(ctx context.Context, readingIDs []string) ([]CheckLog, error) {
	logs, err := s.queries.ListCheckLogsOfReadings(ctx, readingIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to list the check logs of %d readings: %w", len(readingIDs), err)
	}

	steps, err := s.queries.ListCheckLogStepsOfReadings(ctx, readingIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to list the check log steps of %d readings: %w", len(readingIDs), err)
	}

	stepsOf := map[string][]checklog.StepRange{}
	for _, step := range steps {
		stepsOf[step.CheckID] = append(stepsOf[step.CheckID], checklog.StepRange{
			Number:     int(step.Number),
			Name:       step.Name,
			Conclusion: step.Conclusion,
			FirstLine:  int(step.FirstLine),
			LineCount:  int(step.LineCount),
			ErrorCount: int(step.ErrorCount),
		})
	}

	return xslice.Map(logs, func(log *models.ListCheckLogsOfReadingsRow) CheckLog {
		return CheckLog{
			CheckID:    log.CheckID,
			FetchedAt:  log.FetchedAt,
			Outcome:    CheckLogOutcome(log.Outcome),
			LineCount:  int(log.LineCount),
			ErrorCount: int(log.ErrorCount),
			Steps:      stepsOf[log.CheckID],
		}
	}), nil
}

// GetCheckLogLines is up to count lines of a check's fetched log from firstLine, counting from one, or
// ErrNotFound when the check has no fetched log.
func (s *StatusCheckStore) GetCheckLogLines(ctx context.Context, checkID string, firstLine, count int) (*CheckLogLines, error) {
	if !store.IsID(checkID) {
		return nil, store.ErrNotFound.Saying("no fetched log for check %s", checkID)
	}

	row, err := s.queries.GetCheckLogText(ctx, checkID)
	if db.IsNoRows(err) {
		return nil, store.ErrNotFound.Saying("no fetched log for check %s", checkID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read the log of check %s: %w", checkID, err)
	}

	lines := checklog.Lines(row.Text)
	start := min(max(firstLine-1, 0), len(lines))
	end := min(start+max(count, 0), len(lines))

	return &CheckLogLines{Lines: lines[start:end], LineCount: int(row.LineCount)}, nil
}

// ListCheckLogHits is every line of the checks' fetched logs the RE2 pattern matches one line at a time, in
// check and line order, up to the most hits a search answers with.
func (s *StatusCheckStore) ListCheckLogHits(ctx context.Context, checkIDs []string, pattern string) (*CheckLogSearch, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, ErrInvalidPattern.Saying("the pattern %q is not a valid regular expression: %v", pattern, err)
	}

	checkIDs = store.KeepIDs(checkIDs)

	texts, err := s.queries.ListCheckLogTexts(ctx, checkIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to list the log texts of %d checks: %w", len(checkIDs), err)
	}

	steps, err := s.queries.ListCheckLogStepsOfChecks(ctx, checkIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to list the log steps of %d checks: %w", len(checkIDs), err)
	}

	stepsOf := map[string][]*models.ListCheckLogStepsOfChecksRow{}
	for _, step := range steps {
		stepsOf[step.CheckID] = append(stepsOf[step.CheckID], step)
	}

	search := &CheckLogSearch{}
	for _, text := range texts {
		for i, line := range checklog.Lines(text.Text) {
			if !re.MatchString(line) {
				continue
			}
			if len(search.Hits) == maxSearchHits {
				search.Limited = true

				return search, nil
			}

			cut, isCut := cutRunes(line, maxHitRunes)
			search.Hits = append(search.Hits, CheckLogHit{
				CheckID:    text.CheckID,
				Line:       i + 1,
				StepNumber: stepAt(stepsOf[text.CheckID], i+1),
				Text:       cut,
				Cut:        isCut,
			})
		}
	}

	return search, nil
}

// GetCheckForLogFetch is a check with the steps its log is mapped by, or ErrNotFound.
func (s *StatusCheckStore) GetCheckForLogFetch(ctx context.Context, checkID string) (*checkForLogFetch, error) {
	row, err := s.queries.GetCheckForLogFetch(ctx, checkID)
	if db.IsNoRows(err) {
		return nil, store.ErrNotFound.Saying("no check %s", checkID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get check %s: %w", checkID, err)
	}

	steps, err := s.queries.ListStepsOfCheck(ctx, checkID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the steps of check %s: %w", checkID, err)
	}

	return &checkForLogFetch{
		ID:            row.ID,
		State:         row.State,
		JobID:         row.JobID,
		FinishedAt:    row.FinishedAt,
		Host:          row.Host,
		RepositoryURL: row.RepositoryUrl,
		Steps: xslice.Map(steps, func(step *models.ListStepsOfCheckRow) checklog.Step {
			return checklog.Step{
				Number:     int(step.Number),
				Name:       step.Name,
				Conclusion: step.Conclusion,
				StartedAt:  step.StartedAt,
			}
		}),
	}, nil
}

// ListHeadChecksWantingLogs is the finished checks on a pull request's head commit that ran in a job and
// have no log kept, whatever the forge answered.
func (s *StatusCheckStore) ListHeadChecksWantingLogs(ctx context.Context, pullRequestID string) ([]string, error) {
	ids, err := s.queries.ListHeadChecksWantingLogs(ctx, pullRequestID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the checks wanting logs of pull request %s: %w", pullRequestID, err)
	}

	return ids, nil
}

// stepAt is the number of the step holding the line among steps ordered by their first line, and 0 when no
// step does.
func stepAt(steps []*models.ListCheckLogStepsOfChecksRow, line int) int {
	after, _ := slices.BinarySearchFunc(steps, line+1, func(step *models.ListCheckLogStepsOfChecksRow, line int) int {
		return cmp.Compare(int(step.FirstLine), line)
	})
	i := after - 1
	if i < 0 || line >= int(steps[i].FirstLine+steps[i].LineCount) {
		return 0
	}

	return int(steps[i].Number)
}

// cutRunes is the text cut to at most n characters, and whether it was cut.
func cutRunes(text string, n int) (string, bool) {
	if len(text) <= n {
		return text, false
	}

	count := 0
	for i := range text {
		if count == n {
			return text[:i], true
		}
		count++
	}

	return text, false
}
