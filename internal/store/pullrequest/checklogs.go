package pullrequest

import (
	"context"
	"fmt"
	"time"

	"komrad/db"
	"komrad/db/models"
	"komrad/internal/store"
	"komrad/lib/xslice"
)

// checkLogOutcome is what the forge answered when komrad last asked for a check's log.
type checkLogOutcome string

const (
	// checkLogFetched is a log the forge handed over.
	checkLogFetched checkLogOutcome = "fetched"
	// CheckLogMissing is a log the forge never had ready.
	CheckLogMissing checkLogOutcome = "missing"
	// checkLogExpired is a log the forge no longer keeps.
	checkLogExpired checkLogOutcome = "expired"
	// CheckLogFailed is a log komrad failed to ask for.
	CheckLogFailed checkLogOutcome = "failed"
)

// checkLogPart is the lines of one step of a check's log, or of the whole log as step 0 when it was not
// split.
type checkLogPart struct {
	StepNumber int
	// Text is empty when the part was listed rather than read.
	Text       string
	LineCount  int
	ErrorCount int
}

// checkLog is a check's log as komrad last fetched it, its parts listed without their text.
type checkLog struct {
	CheckID   string
	FetchedAt time.Time
	Outcome   checkLogOutcome
	// Split is whether the log was split into one part per step, rather than kept whole as step 0.
	Split bool
	// Truncated is whether the log ran past the most komrad keeps, and was cut there.
	Truncated bool
	LineCount int
	Parts     []checkLogPart
}

// RecordCheckLog keeps a check's log, replacing the one kept before it with all its parts, and must run on
// a store built inside InTx.
func (s *StatusCheckStore) RecordCheckLog(ctx context.Context, log checkLog) error {
	logID, err := s.queries.RecordCheckLog(ctx, models.RecordCheckLogParams{
		CheckID:   log.CheckID,
		FetchedAt: log.FetchedAt,
		Outcome:   string(log.Outcome),
		Split:     log.Split,
		Truncated: log.Truncated,
		LineCount: int32(log.LineCount),
	})
	if err != nil {
		return fmt.Errorf("failed to record the log of check %s: %w", log.CheckID, err)
	}

	if err := s.queries.DeleteCheckLogParts(ctx, logID); err != nil {
		return fmt.Errorf("failed to delete the log parts of check %s: %w", log.CheckID, err)
	}

	if len(log.Parts) == 0 {
		return nil
	}

	rows := xslice.Map(log.Parts, func(part checkLogPart) models.InsertCheckLogPartsParams {
		return models.InsertCheckLogPartsParams{
			LogID:      logID,
			StepNumber: int32(part.StepNumber),
			Text:       part.Text,
			LineCount:  int32(part.LineCount),
			ErrorCount: int32(part.ErrorCount),
		}
	})
	if err := s.queries.InsertCheckLogParts(ctx, rows).Close(); err != nil {
		return fmt.Errorf("failed to insert the log parts of check %s: %w", log.CheckID, err)
	}

	return nil
}

// ListCheckLogsOfReadings is the log kept for each check of the readings that has one, its parts listed
// without their text.
func (s *StatusCheckStore) ListCheckLogsOfReadings(ctx context.Context, readingIDs []string) ([]checkLog, error) {
	logs, err := s.queries.ListCheckLogsOfReadings(ctx, readingIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to list the check logs of %d readings: %w", len(readingIDs), err)
	}

	parts, err := s.queries.ListCheckLogPartsOfReadings(ctx, readingIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to list the check log parts of %d readings: %w", len(readingIDs), err)
	}

	partsOf := map[string][]checkLogPart{}
	for _, part := range parts {
		partsOf[part.CheckID] = append(partsOf[part.CheckID], checkLogPart{
			StepNumber: int(part.StepNumber),
			LineCount:  int(part.LineCount),
			ErrorCount: int(part.ErrorCount),
		})
	}

	return xslice.Map(logs, func(log *models.PullRequestCheckLog) checkLog {
		return checkLog{
			CheckID:   log.CheckID,
			FetchedAt: log.FetchedAt,
			Outcome:   checkLogOutcome(log.Outcome),
			Split:     log.Split,
			Truncated: log.Truncated,
			LineCount: int(log.LineCount),
			Parts:     partsOf[log.CheckID],
		}
	}), nil
}

// GetCheckLogPart is one part of a check's log with its text, or ErrNotFound.
func (s *StatusCheckStore) GetCheckLogPart(ctx context.Context, checkID string, stepNumber int) (*checkLogPart, error) {
	row, err := s.queries.GetCheckLogPart(ctx, models.GetCheckLogPartParams{CheckID: checkID, StepNumber: int32(stepNumber)})
	if db.IsNoRows(err) {
		return nil, store.ErrNotFound.Saying("no log part for step %d of check %s", stepNumber, checkID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get the log part for step %d of check %s: %w", stepNumber, checkID, err)
	}

	return &checkLogPart{
		StepNumber: int(row.StepNumber),
		Text:       row.Text,
		LineCount:  int(row.LineCount),
		ErrorCount: int(row.ErrorCount),
	}, nil
}
