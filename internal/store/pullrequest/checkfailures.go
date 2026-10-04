package pullrequest

import (
	"context"
	"fmt"
	"slices"
	"time"

	"komrad/db/models"
	"komrad/internal/forge"
	"komrad/lib/xslice"
)

// CheckFailure is a failed check and when an agent was told of it.
type CheckFailure struct {
	Check forge.StatusCheck
	// NotedAt is when the agent was told of the failure, nil when it has not been.
	NotedAt *time.Time
}

// ListCheckFailuresParams is the failed checks on one head of a pull request, as one agent hears of them.
type ListCheckFailuresParams struct {
	PullRequestID string
	HeadSHA       string
	AgentID       string
	Failed        []forge.StatusCheck
}

// ListCheckFailures is each failed check with when the agent was told of it, a check that failed again
// since counting as not told.
func (s *StatusCheckStore) ListCheckFailures(ctx context.Context, params ListCheckFailuresParams) ([]CheckFailure, error) {
	notes, err := s.queries.ListCheckFailureNotes(ctx, models.ListCheckFailureNotesParams{
		AgentID:       params.AgentID,
		PullRequestID: params.PullRequestID,
		HeadSha:       params.HeadSHA,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list the check failures agent %s was told of on %s: %w", params.AgentID, params.HeadSHA, err)
	}

	return xslice.Map(params.Failed, func(check forge.StatusCheck) CheckFailure {
		for _, note := range notes {
			if isSameFailure(forge.StatusCheck{ProviderID: note.ProviderID, FinishedAt: note.FinishedAt}, check) {
				return CheckFailure{Check: check, NotedAt: &note.NotedAt}
			}
		}

		return CheckFailure{Check: check}
	}), nil
}

// RecordCheckFailuresParams is the failed checks on one head of a pull request an agent was just told of.
type RecordCheckFailuresParams struct {
	PullRequestID string
	HeadSHA       string
	AgentID       string
	Told          []forge.StatusCheck
}

// RecordCheckFailures records that the agent was told of each failed check, keeping when it was first told.
func (s *StatusCheckStore) RecordCheckFailures(ctx context.Context, params RecordCheckFailuresParams) error {
	if len(params.Told) == 0 {
		return nil
	}

	rows := xslice.Map(params.Told, func(check forge.StatusCheck) models.NoteCheckFailureParams {
		return models.NoteCheckFailureParams{
			AgentID:       params.AgentID,
			FinishedAt:    check.FinishedAt,
			PullRequestID: params.PullRequestID,
			HeadSha:       params.HeadSHA,
			ProviderID:    check.ProviderID,
		}
	})
	if err := s.queries.NoteCheckFailure(ctx, rows).Close(); err != nil {
		return fmt.Errorf("failed to record the check failures agent %s was told of on %s: %w", params.AgentID, params.HeadSHA, err)
	}

	return nil
}

func isSameFailure(known, current forge.StatusCheck) bool {
	if known.ProviderID != current.ProviderID {
		return false
	}
	if known.FinishedAt == nil || current.FinishedAt == nil {
		return known.FinishedAt == current.FinishedAt
	}

	return known.FinishedAt.Equal(*current.FinishedAt)
}

func hasNewFailure(before, after StatusCheckReading) bool {
	failedAfter := forge.FailedStatusChecks(after.StatusChecks)
	if before.HeadSHA != after.HeadSHA {
		return len(failedAfter) > 0
	}

	failedBefore := forge.FailedStatusChecks(before.StatusChecks)

	return slices.ContainsFunc(failedAfter, func(check forge.StatusCheck) bool {
		return !slices.ContainsFunc(failedBefore, func(was forge.StatusCheck) bool {
			return isSameFailure(was, check)
		})
	})
}
