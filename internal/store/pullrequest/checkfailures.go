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

// CheckFailuresParams is the failed checks on one head of a pull request, as one agent hears of them.
type CheckFailuresParams struct {
	PullRequestID string
	HeadSHA       string
	AgentID       string
	Failed        []forge.StatusCheck
}

// ListCheckFailures is each failed check with when the agent was told of it, a check that failed again
// since counting as not told.
func (s *StatusCheckStore) ListCheckFailures(ctx context.Context, params CheckFailuresParams) ([]CheckFailure, error) {
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
			if note.ProviderID == check.ProviderID && sameTime(note.FinishedAt, check.FinishedAt) {
				return CheckFailure{Check: check, NotedAt: &note.NotedAt}
			}
		}

		return CheckFailure{Check: check}
	}), nil
}

// RecordCheckFailures records that the agent was told of each failed check, keeping when it was first told.
func (s *StatusCheckStore) RecordCheckFailures(ctx context.Context, params CheckFailuresParams) error {
	if len(params.Failed) == 0 {
		return nil
	}

	rows := xslice.Map(params.Failed, func(check forge.StatusCheck) models.NoteCheckFailureParams {
		return models.NoteCheckFailureParams{
			AgentID:       params.AgentID,
			FinishedAt:    check.FinishedAt,
			PullRequestID: params.PullRequestID,
			HeadSha:       params.HeadSHA,
			ProviderID:    check.ProviderID,
		}
	})
	if err := s.queries.NoteCheckFailure(ctx, rows).Close(); err != nil {
		return fmt.Errorf("failed to note the check failures agent %s was told of on %s: %w", params.AgentID, params.HeadSHA, err)
	}

	return nil
}

// sameTime says whether two optional times are both unset or the same instant.
func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}

	return a.Equal(*b)
}

// hasNewFailure says whether the latest reading after a read holds a failed check the latest reading before it
// did not, every failure on another head counting as new.
func hasNewFailure(before, after StatusCheckReading) bool {
	failedBefore := forge.FailedStatusChecks(before.StatusChecks)

	return slices.ContainsFunc(forge.FailedStatusChecks(after.StatusChecks), func(check forge.StatusCheck) bool {
		return before.HeadSHA != after.HeadSHA || !slices.ContainsFunc(failedBefore, func(was forge.StatusCheck) bool {
			return was.ProviderID == check.ProviderID && sameTime(was.FinishedAt, check.FinishedAt)
		})
	})
}
