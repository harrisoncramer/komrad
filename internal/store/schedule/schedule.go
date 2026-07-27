// Package schedule persists recurring agent launches, the schedules table and
// the config row holding the cwd and flags each launch starts from.
//
// A schedule is a blueprint. It holds no pointer to the agents it has produced,
// since it fires many times over its life. Each agent records the schedule it came
// from, and reading that back is the agent package's job.
package schedule

import (
	"context"
	"errors"
	"fmt"
	"time"

	"claude-agents/db/models"
	"claude-agents/internal/store"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrNameTaken reports that a create or an update collided with a live
// schedule's name. The unique index is scoped to live rows, so this only ever
// means a schedule currently listed answers to that name.
var ErrNameTaken = errors.New("name already taken")

// uniqueViolation is the SQLSTATE Postgres raises for a broken unique
// constraint, which is how a taken name is told apart from a real failure
// without reading the driver's message text.
const uniqueViolation = "23505"

// nameTaken reports whether err is the unique index on live names refusing a
// write.
func nameTaken(err error) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}

// Schedule is a recurring agent launch, what to run, where, and how often.
type Schedule struct {
	ID   string
	Name string
	// Cron is a standard five-field expression, stored as the user wrote it and
	// parsed above this layer.
	Cron string
	// Timezone is the IANA zone the expression is read in, empty for the
	// machine's local zone.
	Timezone string
	// Enabled false pauses the schedule. It keeps its row and its history and
	// simply stops firing.
	Enabled bool
	Prompt  string
	// WorkflowID is the workflow launched agents are filed under, nil to leave
	// them at the root.
	WorkflowID *string
	Cwd        string
	Flags      string
	// UpdatedAt is what the listing sorts by, and half of the paging cursor.
	UpdatedAt time.Time
}

// Config is the editable shape of a schedule, everything a caller may set.
type Config struct {
	Name       string
	Cron       string
	Timezone   string
	Enabled    bool
	Prompt     string
	WorkflowID *string
	Cwd        string
	Flags      string
}

// Store reads and writes schedules.
type Store struct {
	queries *models.Queries
}

// New builds a schedule store over db, which is either the pool or a
// transaction.
func New(db store.DBTX) *Store {
	return &Store{queries: models.New(db)}
}

// List returns every live schedule, most recently updated first.
func (s *Store) List(ctx context.Context) ([]Schedule, error) {
	rows, err := s.queries.ListSchedules(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list schedules: %w", err)
	}

	schedules := make([]Schedule, 0, len(rows))
	for _, row := range rows {
		schedules = append(schedules, scheduleFromRow(row.Schedule, row.Cwd, row.Flags))
	}

	return schedules, nil
}

// Cursor locates a schedule within a listing, the sort key plus the id that
// breaks a tie on it.
type Cursor struct {
	UpdatedAt time.Time `json:"u"`
	ID        string    `json:"i"`
}

// ListPage returns one page of live schedules, most recently updated first,
// resuming after cursor when one is given.
func (s *Store) ListPage(ctx context.Context, limit int64, cursor *Cursor) ([]Schedule, error) {
	params := models.ListSchedulesPageParams{Lim: int32(limit)}
	if cursor != nil {
		params.CursorUpdatedAt = &cursor.UpdatedAt
		params.CursorID = &cursor.ID
	}

	rows, err := s.queries.ListSchedulesPage(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list a page of schedules: %w", err)
	}

	schedules := make([]Schedule, 0, len(rows))
	for _, row := range rows {
		schedules = append(schedules, scheduleFromRow(row.Schedule, row.Cwd, row.Flags))
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
	if store.NoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get schedule %q: %w", id, err)
	}

	schedule := scheduleFromRow(row.Schedule, row.Cwd, row.Flags)
	return &schedule, nil
}

// Create inserts a schedule together with the config row holding the cwd and
// flags its launches start from, and returns it. Both rows must land together,
// so a caller outside a transaction gets one through InTx.
func (s *Store) Create(ctx context.Context, cfg Config) (*Schedule, error) {
	configID, err := s.queries.InsertScheduleConfig(ctx, models.InsertScheduleConfigParams{
		Cwd:   cfg.Cwd,
		Flags: cfg.Flags,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to insert config for schedule %q: %w", cfg.Name, err)
	}

	row, err := s.queries.InsertSchedule(ctx, models.InsertScheduleParams{
		Name:             cfg.Name,
		Cron:             cfg.Cron,
		Timezone:         cfg.Timezone,
		Enabled:          cfg.Enabled,
		Prompt:           cfg.Prompt,
		WorkflowID:       cfg.WorkflowID,
		ScheduleConfigID: configID,
	})
	if nameTaken(err) {
		return nil, fmt.Errorf("%w: %q", ErrNameTaken, cfg.Name)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create schedule %q: %w", cfg.Name, err)
	}

	schedule := scheduleFromRow(*row, cfg.Cwd, cfg.Flags)
	return &schedule, nil
}

// Update replaces a schedule's editable fields and its linked config, keyed by
// id. The two writes must land together, so a caller outside a transaction gets
// one through InTx.
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
		Prompt:     cfg.Prompt,
		WorkflowID: cfg.WorkflowID,
	})
	if nameTaken(err) {
		return fmt.Errorf("%w: %q", ErrNameTaken, cfg.Name)
	}
	if err != nil {
		return fmt.Errorf("failed to update schedule %q: %w", id, err)
	}

	if err := s.queries.UpdateScheduleConfig(ctx, models.UpdateScheduleConfigParams{
		ScheduleID: id,
		Cwd:        cfg.Cwd,
		Flags:      cfg.Flags,
	}); err != nil {
		return fmt.Errorf("failed to update config for schedule %q: %w", id, err)
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
// agents it launched still resolve to the schedule that made them.
func (s *Store) Archive(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.MarkScheduleDeleted(ctx, id); err != nil {
		return fmt.Errorf("failed to archive schedule %q: %w", id, err)
	}

	return nil
}

// scheduleFromRow builds a Schedule from a schedule row and its joined config.
func scheduleFromRow(s models.Schedule, cwd, flags string) Schedule {
	return Schedule{
		ID:         s.ID,
		Name:       s.Name,
		Cron:       s.Cron,
		Timezone:   s.Timezone,
		Enabled:    s.Enabled,
		Prompt:     s.Prompt,
		WorkflowID: s.WorkflowID,
		Cwd:        cwd,
		Flags:      flags,
		UpdatedAt:  s.UpdatedAt,
	}
}
