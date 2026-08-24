package schedule

import (
	"context"
	"fmt"
	"time"

	"weave/db"
	"weave/db/models"
	"weave/internal/store"
)

type Schedule struct {
	ID   string
	Name string

	Cron string

	Timezone string

	Enabled bool

	HideAgents bool
	Prompt     string

	WorkflowID string

	UpdatedAt time.Time
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

type Store struct {
	queries *models.Queries
}

// New builds a schedule store over db, which is either the pool or a
// transaction.
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

type Cursor struct {
	ID string `json:"i"`
}

// Validate refuses a cursor holding anything but an id, which is what the listing pages by.
func (c Cursor) Validate() error {
	if !store.IsID(c.ID) {
		return fmt.Errorf("cursor id %q is not an id", c.ID)
	}

	return nil
}

// ListPage returns one page of live schedules, newest first, resuming after
// cursor when one is given.
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

// Get returns a schedule by id, nil when none exists, or an error for a real
// failure. No-rows is the nil case. Every other error is surfaced.
func (s *Store) Get(ctx context.Context, id string) (*Schedule, error) {
	if !store.IsID(id) {
		return nil, nil
	}

	row, err := s.queries.GetSchedule(ctx, id)
	if db.IsNoRows(err) {
		return nil, nil
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

// SetEnabled pauses or resumes a schedule without touching anything else, which
// is the one edit the panel makes with a single keystroke.
func (s *Store) SetEnabled(ctx context.Context, id string, enabled bool) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.SetScheduleEnabled(ctx, models.SetScheduleEnabledParams{ID: id, Enabled: enabled}); err != nil {
		return fmt.Errorf("failed to set schedule %q enabled to %t: %w", id, enabled, err)
	}

	return nil
}

// Archive hides a schedule from the panel without dropping the row, so the
// agents it launched still point at the schedule that made them.
func (s *Store) Archive(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.MarkScheduleArchived(ctx, id); err != nil {
		return fmt.Errorf("failed to archive schedule %q: %w", id, err)
	}

	return nil
}

// CountLive reports how many live schedules launch into a workflow. Deleting a
// workflow while any do would leave them with nowhere to run, so the caller
// checks here first rather than discovering it when one next fires.
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
