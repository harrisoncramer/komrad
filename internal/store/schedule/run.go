package schedule

import (
	"context"
	"fmt"
	"time"

	"weave/db/models"
	"weave/internal/store"
	"weave/lib/xslice"
)

const (
	recentRunsWatched = 20
	runsKept          = 200
)

// RunOccurrence names one firing of a schedule by the time it was due.
type RunOccurrence struct {
	ScheduleID   string
	ScheduledFor time.Time
}

// RunOutcome is how an attempt at a firing ended.
type RunOutcome struct {
	State   RunState
	Message string
}

type StartRunParams struct {
	Occurrence  RunOccurrence
	Attempt     int
	MaxAttempts int
}

// StartRun records that an attempt at a firing has begun.
func (s *ScheduleStore) StartRun(ctx context.Context, params StartRunParams) error {
	err := s.queries.StartScheduleRun(ctx, models.StartScheduleRunParams{
		ScheduleID:   params.Occurrence.ScheduleID,
		ScheduledFor: params.Occurrence.ScheduledFor,
		Attempt:      int32(params.Attempt),
		MaxAttempts:  int32(params.MaxAttempts),
	})
	if err != nil {
		return fmt.Errorf("failed to record the start of a run of schedule %q: %w", params.Occurrence.ScheduleID, err)
	}

	return nil
}

// FinishRun records how an attempt at a firing ended and drops the schedule's oldest runs.
func (s *ScheduleStore) FinishRun(ctx context.Context, occurrence RunOccurrence, outcome RunOutcome) error {
	err := s.queries.FinishScheduleRun(ctx, models.FinishScheduleRunParams{
		State:        string(outcome.State),
		Message:      outcome.Message,
		ScheduleID:   occurrence.ScheduleID,
		ScheduledFor: occurrence.ScheduledFor,
	})
	if err != nil {
		return fmt.Errorf("failed to record the end of a run of schedule %q: %w", occurrence.ScheduleID, err)
	}

	err = s.queries.PruneScheduleRuns(ctx, models.PruneScheduleRunsParams{ScheduleID: occurrence.ScheduleID, Keep: runsKept})
	if err != nil {
		return fmt.Errorf("failed to prune the runs of schedule %q: %w", occurrence.ScheduleID, err)
	}

	return nil
}

// ListRuns returns a schedule's most recent firings, newest first.
func (s *ScheduleStore) ListRuns(ctx context.Context, scheduleID string, limit int) ([]Run, error) {
	if !store.IsID(scheduleID) {
		return nil, nil
	}

	rows, err := s.queries.ListScheduleRuns(ctx, models.ListScheduleRunsParams{ScheduleID: scheduleID, Lim: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("failed to list the runs of schedule %q: %w", scheduleID, err)
	}

	return xslice.Map(rows, func(row *models.ListScheduleRunsRow) Run {
		return runFromRow(row.ScheduleRun)
	}), nil
}

func (s *ScheduleStore) attachRuns(ctx context.Context, schedules []Schedule, perSchedule int) ([]Schedule, error) {
	if len(schedules) == 0 {
		return schedules, nil
	}

	rows, err := s.queries.ListRecentScheduleRuns(ctx, models.ListRecentScheduleRunsParams{
		ScheduleIds: xslice.Map(schedules, func(schedule Schedule) string {
			return schedule.ID
		}),
		PerSchedule: int32(perSchedule),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list the recent runs of %d schedules: %w", len(schedules), err)
	}

	recent := map[string][]Run{}
	for _, row := range rows {
		recent[row.ScheduleRun.ScheduleID] = append(recent[row.ScheduleRun.ScheduleID], runFromRow(row.ScheduleRun))
	}

	for i := range schedules {
		runs := recent[schedules[i].ID]
		if len(runs) > 0 {
			schedules[i].LatestRun = &runs[0]
		}
		if perSchedule > 1 {
			schedules[i].RecentRuns = runs
		}
	}

	return schedules, nil
}

func runFromRow(row models.ScheduleRun) Run {
	return Run{
		ID:           row.ID,
		ScheduledFor: row.ScheduledFor,
		State:        RunState(row.State),
		Attempt:      int(row.Attempt),
		MaxAttempts:  int(row.MaxAttempts),
		FinalizedAt:  row.FinalizedAt,
		Message:      row.Message,
	}
}
