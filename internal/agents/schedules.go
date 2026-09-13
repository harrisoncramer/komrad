package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"weave/internal/fail"
	"weave/internal/store"
	schedulestore "weave/internal/store/schedule"

	"github.com/robfig/cron/v3"
)

type scheduleReadWriter interface {
	Get(ctx context.Context, id string) (*schedulestore.Schedule, error)
	Create(ctx context.Context, cfg schedulestore.Config) (*schedulestore.Schedule, error)
	Update(ctx context.Context, id string, cfg schedulestore.Config) error
	SetEnabled(ctx context.Context, id string, enabled bool) error
	Archive(ctx context.Context, id string) error
}

var ErrScheduleNotFound = fail.NotFound("schedule not found")

var ErrBadCron = fail.InvalidArgument("unreadable cron expression")

type ScheduleService struct {
	store     scheduleReadWriter
	workflows workflowReader
}

type NewScheduleServiceParams struct {
	Schedules scheduleReadWriter
	Workflows workflowReader
}

// NewScheduleService builds the service from its dependencies.
func NewScheduleService(params NewScheduleServiceParams) *ScheduleService {
	return &ScheduleService{store: params.Schedules, workflows: params.Workflows}
}

// GetSchedule returns a schedule by id, or nil when none has that id.
func (s *ScheduleService) GetSchedule(ctx context.Context, id string) (*schedulestore.Schedule, error) {
	found, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get schedule %q: %w", id, err)
	}

	return found, nil
}

// NewSchedule records a recurring launch, parsing the cron expression first so a schedule that
// could never fire is refused.
func (s *ScheduleService) NewSchedule(ctx context.Context, cfg schedulestore.Config) (*schedulestore.Schedule, error) {
	normalized, err := normalize(cfg)
	if err != nil {
		return nil, err
	}
	if err := s.requireWorkflow(ctx, normalized.WorkflowID); err != nil {
		return nil, err
	}

	created, err := s.store.Create(ctx, normalized)
	if err != nil {
		return nil, fmt.Errorf("failed to create schedule %q: %w", normalized.Name, err)
	}

	return created, nil
}

// UpdateSchedule replaces a schedule's editable fields, keeping the name it already has when a
// blank one is given.
func (s *ScheduleService) UpdateSchedule(ctx context.Context, id string, cfg schedulestore.Config) error {
	existing, err := s.GetSchedule(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return ErrScheduleNotFound.Saying("no schedule with id %q", id)
	}
	if err != nil {
		return err
	}

	normalized, err := normalize(cfg)
	if err != nil {
		return err
	}
	if normalized.Name == "" {
		normalized.Name = existing.Name
	}
	if err := s.requireWorkflow(ctx, normalized.WorkflowID); err != nil {
		return err
	}

	if err := s.store.Update(ctx, id, normalized); err != nil {
		return fmt.Errorf("failed to update schedule %q: %w", id, err)
	}

	return nil
}

// SetScheduleEnabled pauses or resumes a schedule, leaving everything else as it stands.
func (s *ScheduleService) SetScheduleEnabled(ctx context.Context, id string, enabled bool) error {
	_, err := s.GetSchedule(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return ErrScheduleNotFound.Saying("no schedule with id %q", id)
	}
	if err != nil {
		return err
	}

	if err := s.store.SetEnabled(ctx, id, enabled); err != nil {
		return fmt.Errorf("failed to set schedule %q enabled to %t: %w", id, enabled, err)
	}

	return nil
}

// ArchiveSchedule stops a schedule from firing again, leaving the agents it already launched
// alone.
func (s *ScheduleService) ArchiveSchedule(ctx context.Context, id string) error {
	_, err := s.GetSchedule(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return ErrScheduleNotFound.Saying("no schedule with id %q", id)
	}
	if err != nil {
		return err
	}

	if err := s.store.Archive(ctx, id); err != nil {
		return fmt.Errorf("failed to archive schedule %q: %w", id, err)
	}

	return nil
}

func (s *ScheduleService) requireWorkflow(ctx context.Context, workflowID string) error {
	_, err := s.workflows.Get(ctx, workflowID)
	if errors.Is(err, store.ErrNotFound) {
		return ErrWorkflowNotFound.Saying("no workflow with id %q", workflowID)
	}
	if err != nil {
		return fmt.Errorf("failed to look up workflow %q: %w", workflowID, err)
	}

	return nil
}

func normalize(cfg schedulestore.Config) (schedulestore.Config, error) {
	cfg.Name = strings.TrimSpace(cfg.Name)
	cfg.Cron = strings.TrimSpace(cfg.Cron)
	cfg.Timezone = strings.TrimSpace(cfg.Timezone)
	cfg.Prompt = strings.TrimSpace(cfg.Prompt)

	if _, err := ParseCron(cfg.Cron, cfg.Timezone); err != nil {
		return schedulestore.Config{}, err
	}

	return cfg, nil
}

// ParseCron reads a cron expression in a zone and returns the schedule it describes, taking an
// empty zone as the machine's local one.
func ParseCron(expression, timezone string) (cron.Schedule, error) {
	if timezone != "" {
		// The zone is checked before parsing so an unknown one is reported as such rather than as
		// a syntax error about the TZ prefix.
		if _, err := time.LoadLocation(timezone); err != nil {
			return nil, fmt.Errorf("%w: unknown timezone %q", ErrBadCron, timezone)
		}
		expression = "TZ=" + timezone + " " + expression
	}

	parsed, err := cron.ParseStandard(expression)
	if err != nil {
		return nil, fmt.Errorf("%w: %q: %w", ErrBadCron, expression, err)
	}

	// A date the calendar never reaches, such as February the thirtieth, parses cleanly and then
	// never comes round.
	if parsed.Next(time.Now()).IsZero() {
		return nil, fmt.Errorf("%w: %q never occurs", ErrBadCron, expression)
	}

	return parsed, nil
}

// CalculateNextRun returns when a schedule next fires after from, or the zero time when it is
// paused or its expression cannot be read.
func CalculateNextRun(s schedulestore.Schedule, from time.Time) time.Time {
	if !s.Enabled {
		return time.Time{}
	}

	parsed, err := ParseCron(s.Cron, s.Timezone)
	if err != nil {
		return time.Time{}
	}

	return parsed.Next(from)
}
