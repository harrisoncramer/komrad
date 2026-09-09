// Package schedule stores the recurring launches: the cron expression, the workflow and prompt
// each firing uses, and what happened the last time one fired.
package schedule

import (
	"context"
	"fmt"
	"time"
	"unicode/utf8"

	"weave/db"
	"weave/db/models"
	"weave/internal/store"
)

type Schedule struct {
	ID                string
	Name              string
	Cron              string
	Timezone          string
	Enabled           bool
	HideAgents        bool
	Prompt            string
	WorkflowID        string
	LastResultStatus  LaunchResultStatus
	LastResultMessage string
	LastResultAt      *time.Time
	UpdatedAt         time.Time
}

// LaunchResultStatus is what happened the last time a schedule fired, empty when it never has.
type LaunchResultStatus string

const (
	// LaunchResultScheduleNotFound: the schedule was deleted between the firing being queued
	// and it running.
	LaunchResultScheduleNotFound LaunchResultStatus = "schedule_not_found"
	// LaunchResultSchedulePaused: the schedule was disabled by the time it ran.
	LaunchResultSchedulePaused LaunchResultStatus = "schedule_paused"
	// LaunchResultAlreadyRunning: the schedule's last agent was still live, so this firing
	// was skipped rather than launching a second one.
	LaunchResultAlreadyRunning LaunchResultStatus = "already_running"
	// LaunchResultRetrying: the launch failed on an attempt that River will still retry.
	LaunchResultRetrying LaunchResultStatus = "retrying"
	// LaunchResultCannotLaunch: launching the agent failed for a reason retrying will not fix.
	LaunchResultCannotLaunch LaunchResultStatus = "cannot_launch"
	// LaunchResultLaunched: the agent launched.
	LaunchResultLaunched LaunchResultStatus = "launched"
)

type Config struct {
	Name       string
	Cron       string
	Timezone   string
	Enabled    bool
	HideAgents bool
	Prompt     string
	WorkflowID string
}

type Store struct {
	queries *models.Queries
}

// New builds a schedule store over db, which is either the pool or a transaction.
func New(db store.DBTX) *Store {
	return &Store{queries: models.New(db)}
}

// List returns every live schedule, newest first.
func (s *Store) List(ctx context.Context) ([]Schedule, error) {
	rows, err := s.queries.ListSchedules(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list schedules: %w", err)
	}

	schedules := make([]Schedule, 0, len(rows))
	for _, row := range rows {
		schedules = append(schedules, scheduleFromRow(row.Schedule))
	}

	return schedules, nil
}

// Cursor pages this listing, resuming from one row's id.
type Cursor = store.IDCursor

// ListPage returns one page of live schedules, newest first, resuming after cursor when one
// is given.
func (s *Store) ListPage(ctx context.Context, limit int64, cursor *Cursor) ([]Schedule, error) {
	params := models.ListSchedulesPageParams{Lim: int32(limit)}
	if cursor != nil {
		params.CursorID = &cursor.ID
	}

	rows, err := s.queries.ListSchedulesPage(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list a page of schedules: %w", err)
	}

	schedules := make([]Schedule, 0, len(rows))
	for _, row := range rows {
		schedules = append(schedules, scheduleFromRow(row.Schedule))
	}

	return schedules, nil
}

// Get returns a schedule by id, or ErrNotFound when none has that id.
func (s *Store) Get(ctx context.Context, id string) (*Schedule, error) {
	if !store.IsID(id) {
		return nil, fmt.Errorf("%w: schedule %q", store.ErrNotFound, id)
	}

	row, err := s.queries.GetSchedule(ctx, id)
	if db.IsNoRows(err) {
		return nil, fmt.Errorf("%w: schedule %q", store.ErrNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get schedule %q: %w", id, err)
	}

	schedule := scheduleFromRow(row.Schedule)

	return &schedule, nil
}

// Create inserts a schedule and returns it.
func (s *Store) Create(ctx context.Context, cfg Config) (*Schedule, error) {
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
		return nil, fmt.Errorf("%w: %q", store.ErrNameTaken, cfg.Name)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create schedule %q: %w", cfg.Name, err)
	}

	schedule := scheduleFromRow(*row)

	return &schedule, nil
}

// Update replaces a schedule's editable fields, keyed by id.
func (s *Store) Update(ctx context.Context, id string, cfg Config) error {
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
		return fmt.Errorf("%w: %q", store.ErrNameTaken, cfg.Name)
	}
	if err != nil {
		return fmt.Errorf("failed to update schedule %q: %w", id, err)
	}

	return nil
}

// SetEnabled pauses or resumes a schedule without touching anything else.
func (s *Store) SetEnabled(ctx context.Context, id string, enabled bool) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.SetScheduleEnabled(ctx, models.SetScheduleEnabledParams{ID: id, Enabled: enabled}); err != nil {
		return fmt.Errorf("failed to set schedule %q enabled to %t: %w", id, enabled, err)
	}

	return nil
}

// maxLaunchResultMessage caps how much of a launch failure's text is kept.
const maxLaunchResultMessage = 500

// RecordLaunchResult writes what happened the last time this schedule fired, overwriting
// whatever the firing before it recorded.
func (s *Store) RecordLaunchResult(ctx context.Context, id string, status LaunchResultStatus, message string) error {
	if !store.IsID(id) {
		return nil
	}

	statusValue := string(status)
	messageValue := truncateResultMessage(message)

	if err := s.queries.RecordScheduleLaunchResult(ctx, models.RecordScheduleLaunchResultParams{
		ID:                id,
		LastResultStatus:  &statusValue,
		LastResultMessage: &messageValue,
	}); err != nil {
		return fmt.Errorf("failed to record the launch result for schedule %q: %w", id, err)
	}

	return nil
}

func truncateResultMessage(message string) string {
	if len(message) <= maxLaunchResultMessage {
		return message
	}

	cut := message[:maxLaunchResultMessage]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}

	return cut + "…"
}

// Archive hides a schedule from the panel without dropping the row, so the agents it launched
// still point at the schedule that made them.
func (s *Store) Archive(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.MarkScheduleArchived(ctx, id); err != nil {
		return fmt.Errorf("failed to archive schedule %q: %w", id, err)
	}

	return nil
}

// ListInWorkflow returns every schedule pointing at a workflow, archived ones included.
func (s *Store) ListInWorkflow(ctx context.Context, workflowID string) ([]Schedule, error) {
	if !store.IsID(workflowID) {
		return nil, nil
	}

	rows, err := s.queries.ListWorkflowSchedules(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the schedules of workflow %q: %w", workflowID, err)
	}

	schedules := make([]Schedule, 0, len(rows))
	for _, row := range rows {
		schedules = append(schedules, scheduleFromRow(row.Schedule))
	}

	return schedules, nil
}

// Delete permanently removes a schedule, which the caller clears off its launched agents
// first.
func (s *Store) Delete(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.DeleteSchedule(ctx, id); err != nil {
		return fmt.Errorf("failed to delete schedule %q: %w", id, err)
	}

	return nil
}

// CountLive reports how many live schedules launch into a workflow.
func (s *Store) CountLive(ctx context.Context, workflowID string) (int64, error) {
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
	var status LaunchResultStatus
	if s.LastResultStatus != nil {
		status = LaunchResultStatus(*s.LastResultStatus)
	}

	var message string
	if s.LastResultMessage != nil {
		message = *s.LastResultMessage
	}

	return Schedule{
		ID:                s.ID,
		Name:              s.Name,
		Cron:              s.Cron,
		Timezone:          s.Timezone,
		Enabled:           s.Enabled,
		HideAgents:        s.HideAgents,
		Prompt:            s.Prompt,
		WorkflowID:        s.WorkflowID,
		LastResultStatus:  status,
		LastResultMessage: message,
		LastResultAt:      s.LastResultAt,
		UpdatedAt:         s.UpdatedAt,
	}
}
