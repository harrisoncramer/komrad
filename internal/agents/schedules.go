package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"claude-agents/internal/store/schedule"

	"github.com/robfig/cron/v3"
)

// scheduleStore is the part of the schedule store this service reads and
// writes. A schedule is a record until something fires it, so this touches no
// tmux.
type scheduleStore interface {
	Get(ctx context.Context, id string) (*schedule.Schedule, error)
	Create(ctx context.Context, cfg schedule.Config) (*schedule.Schedule, error)
	Update(ctx context.Context, id string, cfg schedule.Config) error
	SetEnabled(ctx context.Context, id string, enabled bool) error
	Archive(ctx context.Context, id string) error
}

// ErrScheduleNotFound reports that no schedule answers to the given id.
var ErrScheduleNotFound = errors.New("schedule not found")

// ErrBadCron reports a cron expression or timezone the scheduler cannot read. The
// proto catches a malformed string before this, so reaching here means the shape
// was fine and the parse still refused it.
var ErrBadCron = errors.New("unreadable cron expression")

// ScheduleService manages recurring agent launches. A schedule is a blueprint of
// an agent plus a cron expression, and nothing here starts anything. The firing
// half reads these records and launches through the agent service.
type ScheduleService struct {
	store scheduleStore
}

// ScheduleParams carries the schedule service's dependencies.
type ScheduleParams struct {
	Schedules scheduleStore
}

// NewScheduleService builds the service from its dependencies.
func NewScheduleService(p ScheduleParams) *ScheduleService {
	return &ScheduleService{store: p.Schedules}
}

// GetSchedule returns a schedule by id, or nil when none has that id.
func (s *ScheduleService) GetSchedule(ctx context.Context, id string) (*schedule.Schedule, error) {
	found, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get schedule %q: %w", id, err)
	}

	return found, nil
}

// NewSchedule records a recurring launch. The cron expression is parsed before
// the write, so a schedule that could never fire is refused at creation rather
// than discovered when it fails to run.
func (s *ScheduleService) NewSchedule(ctx context.Context, cfg schedule.Config) (*schedule.Schedule, error) {
	normalized, err := normalize(cfg)
	if err != nil {
		return nil, err
	}

	created, err := s.store.Create(ctx, normalized)
	if err != nil {
		return nil, fmt.Errorf("failed to create schedule %q: %w", normalized.Name, err)
	}

	return created, nil
}

// UpdateSchedule replaces a schedule's editable fields. A blank name falls back
// to the one it already has, so the list never renders an unlabelled row.
func (s *ScheduleService) UpdateSchedule(ctx context.Context, id string, cfg schedule.Config) error {
	existing, err := s.GetSchedule(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("%w: %q", ErrScheduleNotFound, id)
	}

	normalized, err := normalize(cfg)
	if err != nil {
		return err
	}
	if normalized.Name == "" {
		normalized.Name = existing.Name
	}

	if err := s.store.Update(ctx, id, normalized); err != nil {
		return fmt.Errorf("failed to update schedule %q: %w", id, err)
	}

	return nil
}

// SetScheduleEnabled pauses or resumes a schedule, leaving everything else as
// it stands.
func (s *ScheduleService) SetScheduleEnabled(ctx context.Context, id string, enabled bool) error {
	existing, err := s.GetSchedule(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("%w: %q", ErrScheduleNotFound, id)
	}

	if err := s.store.SetEnabled(ctx, id, enabled); err != nil {
		return fmt.Errorf("failed to set schedule %q enabled to %t: %w", id, enabled, err)
	}

	return nil
}

// DeleteSchedule stops a schedule from firing again. The agents it has already
// launched are left alone, since they are real conversations, and each still
// records the schedule that made it.
func (s *ScheduleService) DeleteSchedule(ctx context.Context, id string) error {
	existing, err := s.GetSchedule(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("%w: %q", ErrScheduleNotFound, id)
	}

	if err := s.store.Archive(ctx, id); err != nil {
		return fmt.Errorf("failed to delete schedule %q: %w", id, err)
	}

	return nil
}

// normalize trims the config, expands the launch directory the way an agent
// launch would, and parses the cron expression so an unreadable one never
// reaches the database.
func normalize(cfg schedule.Config) (schedule.Config, error) {
	cfg.Name = strings.TrimSpace(cfg.Name)
	cfg.Cron = strings.TrimSpace(cfg.Cron)
	cfg.Timezone = strings.TrimSpace(cfg.Timezone)
	cfg.Prompt = strings.TrimSpace(cfg.Prompt)
	cfg.Cwd = expandHome(strings.TrimSpace(cfg.Cwd))
	cfg.Flags = strings.TrimSpace(cfg.Flags)

	if _, err := ParseCron(cfg.Cron, cfg.Timezone); err != nil {
		return schedule.Config{}, err
	}

	return cfg, nil
}

// ParseCron reads a cron expression in a zone and returns the schedule it
// describes. An empty zone is the machine's local one, which is what a person
// writing "9am" on their laptop means.
//
// The zone is checked before parsing so an unknown one is reported as such,
// rather than as a syntax error about the TZ prefix the parser reads it
// through.
func ParseCron(expression, timezone string) (cron.Schedule, error) {
	if timezone != "" {
		if _, err := time.LoadLocation(timezone); err != nil {
			return nil, fmt.Errorf("%w: unknown timezone %q", ErrBadCron, timezone)
		}
		expression = "TZ=" + timezone + " " + expression
	}

	parsed, err := cron.ParseStandard(expression)
	if err != nil {
		return nil, fmt.Errorf("%w: %q: %w", ErrBadCron, expression, err)
	}

	return parsed, nil
}

// NextRun returns when a schedule next fires after from, or the zero time when
// it is paused or its expression cannot be read. A paused schedule has no next
// run, which is what the panel renders as a dash.
func NextRun(s schedule.Schedule, from time.Time) time.Time {
	if !s.Enabled {
		return time.Time{}
	}

	parsed, err := ParseCron(s.Cron, s.Timezone)
	if err != nil {
		return time.Time{}
	}

	return parsed.Next(from)
}
