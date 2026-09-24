// Package schedule stores the recurring launches: the cron expression, the workflow and prompt
// each firing uses.
package schedule

import (
	"context"
	"fmt"
	"time"

	"weave/db"
	"weave/db/models"
	"weave/internal/store"
	"weave/lib/xslice"
)

type Schedule struct {
	ID         string
	Name       string
	Cron       string
	Timezone   string
	Enabled    bool
	HideAgents bool
	Prompt     string
	WorkflowID string
	UpdatedAt  time.Time
	LatestRun  *Run
	RecentRuns []Run
}

// RunState is where a firing stands.
type RunState string

const (
	RunStateRunning   RunState = "running"
	RunStateRetryable RunState = "retryable"
	RunStateCompleted RunState = "completed"
	RunStateCancelled RunState = "cancelled"
	RunStateDiscarded RunState = "discarded"
)

// Run is one firing of a schedule as the launch job recorded it.
type Run struct {
	ID           int64
	ScheduledFor time.Time
	State        RunState
	Attempt      int
	MaxAttempts  int
	FinalizedAt  *time.Time
	Message      string
}

type Config struct {
	Name       string
	Cron       string
	Timezone   string
	Enabled    bool
	HideAgents bool
	Prompt     string
	WorkflowID string
}

type ScheduleStore struct {
	queries *models.Queries
}

// NewScheduleStore builds a schedule store over db, which is either the pool or a transaction.
func NewScheduleStore(db store.DBTX) *ScheduleStore {
	return &ScheduleStore{queries: models.New(db)}
}

// Cursor pages this listing, resuming from one row's id.
type Cursor = store.IDCursor

// ListPage returns one page of live schedules, newest first, resuming after cursor when one
// is given.
func (s *ScheduleStore) ListPage(ctx context.Context, limit int64, cursor *Cursor) ([]Schedule, error) {
	params := models.ListSchedulesPageParams{Lim: int32(limit)}
	if cursor != nil {
		params.CursorID = &cursor.ID
	}

	rows, err := s.queries.ListSchedulesPage(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list a page of schedules: %w", err)
	}

	schedules := xslice.Map(rows, func(row *models.ListSchedulesPageRow) Schedule {
		return scheduleFromRow(row.Schedule)
	})

	return s.attachRuns(ctx, schedules, 1)
}

// Get returns a schedule by id, or ErrNotFound when none has that id.
func (s *ScheduleStore) Get(ctx context.Context, id string) (*Schedule, error) {
	if !store.IsID(id) {
		return nil, store.ErrNotFound.Saying("no schedule with id %q", id)
	}

	row, err := s.queries.GetSchedule(ctx, id)
	if db.IsNoRows(err) {
		return nil, store.ErrNotFound.Saying("no schedule with id %q", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get schedule %q: %w", id, err)
	}

	attached, err := s.attachRuns(ctx, []Schedule{scheduleFromRow(row.Schedule)}, 1)
	if err != nil {
		return nil, err
	}

	return &attached[0], nil
}

// Create inserts a schedule and returns it.
func (s *ScheduleStore) Create(ctx context.Context, cfg Config) (*Schedule, error) {
	row, err := s.queries.InsertSchedule(ctx, models.InsertScheduleParams{
		Name:       cfg.Name,
		Cron:       cfg.Cron,
		Timezone:   cfg.Timezone,
		Enabled:    cfg.Enabled,
		HideAgents: cfg.HideAgents,
		Prompt:     cfg.Prompt,
		WorkflowID: cfg.WorkflowID,
	})
	if db.IsUniqueViolation(err) {
		return nil, store.ErrNameTaken.Saying("a schedule named %q already exists", cfg.Name)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create schedule %q: %w", cfg.Name, err)
	}

	schedule := scheduleFromRow(*row)

	return &schedule, nil
}

// Update replaces a schedule's editable fields, keyed by id.
func (s *ScheduleStore) Update(ctx context.Context, id string, cfg Config) error {
	if !store.IsID(id) {
		return nil
	}

	err := s.queries.UpdateSchedule(ctx, models.UpdateScheduleParams{
		ID:         id,
		Name:       cfg.Name,
		Cron:       cfg.Cron,
		Timezone:   cfg.Timezone,
		Enabled:    cfg.Enabled,
		HideAgents: cfg.HideAgents,
		Prompt:     cfg.Prompt,
		WorkflowID: cfg.WorkflowID,
	})
	if db.IsUniqueViolation(err) {
		return store.ErrNameTaken.Saying("a schedule named %q already exists", cfg.Name)
	}
	if err != nil {
		return fmt.Errorf("failed to update schedule %q: %w", id, err)
	}

	return nil
}

// SetEnabled pauses or resumes a schedule without touching anything else.
func (s *ScheduleStore) SetEnabled(ctx context.Context, id string, enabled bool) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.SetScheduleEnabled(ctx, models.SetScheduleEnabledParams{ID: id, Enabled: enabled}); err != nil {
		return fmt.Errorf("failed to set schedule %q enabled to %t: %w", id, enabled, err)
	}

	return nil
}

// Archive hides a schedule from the panel without dropping the row, so the agents it launched
// still point at the schedule that made them.
func (s *ScheduleStore) Archive(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.MarkScheduleArchived(ctx, id); err != nil {
		return fmt.Errorf("failed to archive schedule %q: %w", id, err)
	}

	return nil
}

// ListInWorkflow returns every schedule pointing at a workflow, archived ones included.
func (s *ScheduleStore) ListInWorkflow(ctx context.Context, workflowID string) ([]Schedule, error) {
	if !store.IsID(workflowID) {
		return nil, nil
	}

	rows, err := s.queries.ListWorkflowSchedules(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the schedules of workflow %q: %w", workflowID, err)
	}

	schedules := xslice.Map(rows, func(row *models.ListWorkflowSchedulesRow) Schedule {
		return scheduleFromRow(row.Schedule)
	})

	return schedules, nil
}

// Delete permanently removes a schedule, which the caller clears off its launched agents
// first.
func (s *ScheduleStore) Delete(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.DeleteSchedule(ctx, id); err != nil {
		return fmt.Errorf("failed to delete schedule %q: %w", id, err)
	}

	return nil
}

// CountLive reports how many live schedules launch into a workflow.
func (s *ScheduleStore) CountLive(ctx context.Context, workflowID string) (int64, error) {
	if !store.IsID(workflowID) {
		return 0, nil
	}

	count, err := s.queries.CountLiveWorkflowSchedules(ctx, workflowID)
	if err != nil {
		return 0, fmt.Errorf("failed to count the schedules of workflow %q: %w", workflowID, err)
	}

	return count, nil
}

func scheduleFromRow(s models.Schedule) Schedule {
	return Schedule{
		ID:         s.ID,
		Name:       s.Name,
		Cron:       s.Cron,
		Timezone:   s.Timezone,
		Enabled:    s.Enabled,
		HideAgents: s.HideAgents,
		Prompt:     s.Prompt,
		WorkflowID: s.WorkflowID,
		UpdatedAt:  s.UpdatedAt,
	}
}
