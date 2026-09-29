package schedules

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"komrad/internal/fail"
	"komrad/internal/mail"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	schedulestore "komrad/internal/store/schedule"
	workflowstore "komrad/internal/store/workflow"
	"komrad/lib/clock"

	"github.com/robfig/cron/v3"
)

type scheduleReadWriter interface {
	Get(ctx context.Context, id string) (*schedulestore.Schedule, error)
	GetIncludingArchived(ctx context.Context, id string) (*schedulestore.Schedule, error)
	Create(ctx context.Context, cfg schedulestore.Config) (*schedulestore.Schedule, error)
	Update(ctx context.Context, id string, cfg schedulestore.Config) error
	SetEnabled(ctx context.Context, id string, enabled bool) error
	Archive(ctx context.Context, id string) error
	Restore(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
	ListPage(ctx context.Context, limit int64, cursor *schedulestore.Cursor, archived bool) ([]schedulestore.Schedule, error)
	ListRuns(ctx context.Context, scheduleID string, limit int) ([]schedulestore.Run, error)
}

type workflowReader interface {
	Get(ctx context.Context, id string) (*workflowstore.Workflow, error)
	GetIncludingArchived(ctx context.Context, id string) (*workflowstore.Workflow, error)
}

type agentGetter interface {
	GetByID(ctx context.Context, id string) (*agentstore.Agent, error)
}

var (
	ErrScheduleNotFound     = fail.NotFound("schedule not found")
	ErrBadCron              = fail.InvalidArgument("unreadable cron expression")
	ErrWorkflowNotFound     = fail.NotFound("workflow not found")
	ErrWorkflowArchived     = fail.Precondition("workflow is archived")
	ErrOrchestratorNotFound = fail.NotFound("orchestrator not found")
	ErrOrchestratorArchived = fail.Precondition("orchestrator is archived")
	ErrNotAnOrchestrator    = fail.InvalidArgument("agent is not an orchestrator")
	ErrPromptTooLong        = fail.InvalidArgument("prompt is too long to mail")
	ErrBadKind              = fail.InvalidArgument("unknown schedule kind")
)

type NewScheduleServiceParams struct {
	Schedules scheduleReadWriter
	Workflows workflowReader
	Agents    agentGetter
}

type ScheduleService struct {
	store     scheduleReadWriter
	workflows workflowReader
	agents    agentGetter
}

// NewScheduleService builds the service from its dependencies.
func NewScheduleService(params NewScheduleServiceParams) *ScheduleService {
	return &ScheduleService{store: params.Schedules, workflows: params.Workflows, agents: params.Agents}
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
	if normalized, err = s.resolveTarget(ctx, normalized); err != nil {
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
	if normalized, err = s.resolveTarget(ctx, normalized); err != nil {
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

// RestoreSchedule takes a schedule out of the archive, refusing while what it fires at is gone.
func (s *ScheduleService) RestoreSchedule(ctx context.Context, id string) error {
	existing, err := s.getArchived(ctx, id)
	if err != nil {
		return err
	}

	if err := s.requireTarget(ctx, *existing); err != nil {
		return err
	}

	if err := s.store.Restore(ctx, id); err != nil {
		return fmt.Errorf("failed to restore schedule %q: %w", existing.Name, err)
	}

	return nil
}

// DeleteSchedule permanently removes an archived schedule and its firings.
func (s *ScheduleService) DeleteSchedule(ctx context.Context, id string) error {
	existing, err := s.getArchived(ctx, id)
	if err != nil {
		return err
	}

	if err := s.store.Delete(ctx, id); err != nil {
		return fmt.Errorf("failed to delete schedule %q: %w", existing.Name, err)
	}

	return nil
}

func (s *ScheduleService) getArchived(ctx context.Context, id string) (*schedulestore.Schedule, error) {
	existing, err := s.store.GetIncludingArchived(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrScheduleNotFound.Saying("no schedule with id %q", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get schedule %q: %w", id, err)
	}
	if existing.ArchivedAt == nil {
		return nil, ErrScheduleNotFound.Saying("schedule %q is not archived", existing.Name)
	}

	return existing, nil
}

// requireTarget checks that what a stored schedule fires at is still live.
func (s *ScheduleService) requireTarget(ctx context.Context, schedule schedulestore.Schedule) error {
	if schedule.Kind == schedulestore.KindOrchestrator {
		_, err := s.RequireOrchestrator(ctx, *schedule.OrchestratorAgentID)

		return err
	}

	return s.requireWorkflow(ctx, schedule.WorkflowID)
}

type ListSchedulesParams struct {
	Limit    int64
	Cursor   *schedulestore.Cursor
	Archived bool
}

// ListSchedules returns one page of schedules, live or archived, newest first.
func (s *ScheduleService) ListSchedules(ctx context.Context, params ListSchedulesParams) ([]schedulestore.Schedule, error) {
	listed, err := s.store.ListPage(ctx, params.Limit, params.Cursor, params.Archived)
	if err != nil {
		return nil, fmt.Errorf("failed to list schedules: %w", err)
	}

	return listed, nil
}

type ListScheduleRunsParams struct {
	ScheduleID string
	Limit      int
}

// ListScheduleRuns returns a schedule's most recent firings, newest first.
func (s *ScheduleService) ListScheduleRuns(ctx context.Context, params ListScheduleRunsParams) ([]schedulestore.Run, error) {
	_, err := s.GetSchedule(ctx, params.ScheduleID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrScheduleNotFound.Saying("no schedule with id %q", params.ScheduleID)
	}
	if err != nil {
		return nil, err
	}

	runs, err := s.store.ListRuns(ctx, params.ScheduleID, params.Limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list the runs of schedule %q: %w", params.ScheduleID, err)
	}

	return runs, nil
}

// resolveTarget checks what a schedule fires at, filing an orchestrator schedule under its
// orchestrator's workflow.
func (s *ScheduleService) resolveTarget(ctx context.Context, cfg schedulestore.Config) (schedulestore.Config, error) {
	switch cfg.Kind {
	case schedulestore.KindImplementer:
		cfg.OrchestratorAgentID = nil
		if err := s.requireWorkflow(ctx, cfg.WorkflowID); err != nil {
			return schedulestore.Config{}, err
		}

		return cfg, nil
	case schedulestore.KindOrchestrator:
		if cfg.OrchestratorAgentID == nil {
			return schedulestore.Config{}, ErrOrchestratorNotFound.Saying("an orchestrator schedule names no orchestrator")
		}
		if utf8.RuneCountInString(cfg.Prompt) > mail.MaxMailBody {
			return schedulestore.Config{}, ErrPromptTooLong.Saying("an orchestrator schedule's prompt is mailed, which holds at most %d characters", mail.MaxMailBody)
		}

		orchestrator, err := s.RequireOrchestrator(ctx, *cfg.OrchestratorAgentID)
		if err != nil {
			return schedulestore.Config{}, err
		}
		cfg.WorkflowID = orchestrator.WorkflowID
		cfg.HideAgents = false

		return cfg, nil
	}

	return schedulestore.Config{}, ErrBadKind.Saying("no schedule kind %q", cfg.Kind)
}

// RequireOrchestrator returns the live orchestrator an id names, or says which way it is unusable.
func (s *ScheduleService) RequireOrchestrator(ctx context.Context, id string) (*agentstore.Agent, error) {
	agent, err := s.agents.GetByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrOrchestratorNotFound.Saying("no agent with id %q", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to look up orchestrator %q: %w", id, err)
	}
	if agent.ArchivedAt != nil {
		return nil, ErrOrchestratorArchived.Saying("orchestrator %q has been archived", agent.Name)
	}
	if agent.Role != agentstore.RoleOrchestrator {
		return nil, ErrNotAnOrchestrator.Saying("agent %q was launched as %s", agent.Name, agent.Role)
	}

	return agent, nil
}

func (s *ScheduleService) requireWorkflow(ctx context.Context, workflowID string) error {
	_, err := s.workflows.Get(ctx, workflowID)
	if errors.Is(err, store.ErrNotFound) {
		return s.workflowGone(ctx, workflowID)
	}
	if err != nil {
		return fmt.Errorf("failed to look up workflow %q: %w", workflowID, err)
	}

	return nil
}

// workflowGone says which way a workflow a schedule named is unusable.
func (s *ScheduleService) workflowGone(ctx context.Context, workflowID string) error {
	archived, err := s.workflows.GetIncludingArchived(ctx, workflowID)
	if err == nil && archived != nil {
		return ErrWorkflowArchived.Saying("workflow %q has been archived", archived.Name)
	}

	return ErrWorkflowNotFound.Saying("no workflow with id %q", workflowID)
}

func normalize(cfg schedulestore.Config) (schedulestore.Config, error) {
	cfg.Name = strings.TrimSpace(cfg.Name)
	cfg.Cron = strings.TrimSpace(cfg.Cron)
	cfg.Timezone = strings.TrimSpace(cfg.Timezone)
	cfg.Prompt = strings.TrimSpace(cfg.Prompt)
	if cfg.Kind == "" {
		cfg.Kind = schedulestore.KindImplementer
	}

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
	if parsed.Next(clock.Now()).IsZero() {
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
