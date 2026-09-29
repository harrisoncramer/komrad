package internaljobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"komrad/internal/agents"
	"komrad/internal/mail"
	"komrad/internal/schedules"
	"komrad/internal/store"
	schedulestore "komrad/internal/store/schedule"
	"komrad/lib/xptr"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

type LaunchAgentArgs struct {
	ScheduleID   string    `json:"schedule_id"`
	ScheduledFor time.Time `json:"scheduled_for"`
}

// Kind is the River job kind.
func (LaunchAgentArgs) Kind() string {
	return "launch_agent"
}

// InsertOpts dedups a firing by its arguments, adding the cancelled and discarded states River
// leaves out so a stopped attempt cannot queue the same occurrence again.
func (LaunchAgentArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: QueueLaunchAgent,
		UniqueOpts: river.UniqueOpts{
			ByArgs: true,
			ByState: append(
				rivertype.UniqueOptsByStateDefault(),
				rivertype.JobStateCancelled,
				rivertype.JobStateDiscarded,
			),
		},
	}
}

// NewLaunchAgentWorkerParams is what NewLaunchAgentWorker takes.
type NewLaunchAgentWorkerParams struct {
	Schedules     ScheduleGetter
	Runs          RunRecorder
	Launcher      AgentLauncher
	Running       RunningAgentCounter
	Orchestrators OrchestratorResolver
	Mailer        ScheduledMailer
}

type LaunchAgentWorker struct {
	river.WorkerDefaults[LaunchAgentArgs]
	schedules     ScheduleGetter
	runs          RunRecorder
	agents        AgentLauncher
	running       RunningAgentCounter
	orchestrators OrchestratorResolver
	mailer        ScheduledMailer
}

// NewLaunchAgentWorker builds the worker over the schedules it reads, the service that launches
// an agent, and the one that mails an orchestrator.
func NewLaunchAgentWorker(params NewLaunchAgentWorkerParams) *LaunchAgentWorker {
	return &LaunchAgentWorker{
		schedules:     params.Schedules,
		runs:          params.Runs,
		agents:        params.Launcher,
		running:       params.Running,
		orchestrators: params.Orchestrators,
		mailer:        params.Mailer,
	}
}

// Work fires the schedule, launching its implementer or mailing its orchestrator, cancelling the
// job with its reason for every firing that deliberately does nothing so River's record says why.
func (w *LaunchAgentWorker) Work(ctx context.Context, job *river.Job[LaunchAgentArgs]) error {
	schedule, err := w.schedules.Get(ctx, job.Args.ScheduleID)
	if errors.Is(err, store.ErrNotFound) {
		return river.JobCancel(fmt.Errorf("schedule %q no longer exists", job.Args.ScheduleID))
	}
	if err != nil {
		return fmt.Errorf("failed to read schedule %q: %w", job.Args.ScheduleID, err)
	}

	occurrence := schedulestore.RunOccurrence{ScheduleID: schedule.ID, ScheduledFor: job.Args.ScheduledFor}
	err = w.runs.StartRun(ctx, schedulestore.StartRunParams{
		Occurrence:  occurrence,
		Attempt:     job.Attempt,
		MaxAttempts: job.MaxAttempts,
	})
	if err != nil {
		return fmt.Errorf("failed to start the run of schedule %q: %w", schedule.Name, err)
	}

	launchErr := w.fire(ctx, schedule, job.Args)
	if err := w.runs.FinishRun(ctx, occurrence, buildRunOutcome(launchErr, job.JobRow)); err != nil {
		return errors.Join(launchErr, fmt.Errorf("failed to finish the run of schedule %q: %w", schedule.Name, err))
	}

	return launchErr
}

func (w *LaunchAgentWorker) fire(ctx context.Context, schedule *schedulestore.Schedule, args LaunchAgentArgs) error {
	if !schedule.Enabled {
		return river.JobCancel(fmt.Errorf("schedule %q is paused", schedule.Name))
	}

	if schedule.Kind == schedulestore.KindOrchestrator {
		return w.mailOrchestrator(ctx, schedule, args)
	}

	return w.launch(ctx, schedule, args)
}

// mailOrchestrator sends the prompt to the schedule's orchestrator, skipping the firing when that
// orchestrator has been archived or deleted.
func (w *LaunchAgentWorker) mailOrchestrator(ctx context.Context, schedule *schedulestore.Schedule, args LaunchAgentArgs) error {
	orchestrator, err := w.orchestrators.RequireOrchestrator(ctx, *schedule.OrchestratorAgentID)
	if errors.Is(err, schedules.ErrOrchestratorNotFound) ||
		errors.Is(err, schedules.ErrOrchestratorArchived) ||
		errors.Is(err, schedules.ErrNotAnOrchestrator) {
		return river.JobCancel(fmt.Errorf("failed to mail for schedule %q: %w", schedule.Name, err))
	}
	if err != nil {
		return fmt.Errorf("failed to look up the orchestrator of schedule %q: %w", schedule.Name, err)
	}

	_, err = w.mailer.SendScheduledMail(ctx, mail.SendScheduledMailParams{
		ToAgentID:      orchestrator.ID,
		ScheduleName:   schedule.Name,
		Body:           schedule.Prompt,
		IdempotencyKey: launchKey(schedule.ID, args.ScheduledFor),
	})
	if errors.Is(err, mail.ErrAlreadySent) {
		return nil
	}
	if errors.Is(err, mail.ErrAgentArchived) || errors.Is(err, mail.ErrRecipientNotFound) {
		return river.JobCancel(fmt.Errorf("failed to mail for schedule %q: %w", schedule.Name, err))
	}
	if err != nil {
		return fmt.Errorf("failed to mail orchestrator %q for schedule %q: %w", orchestrator.Name, schedule.Name, err)
	}

	return nil
}

func (w *LaunchAgentWorker) launch(ctx context.Context, schedule *schedulestore.Schedule, args LaunchAgentArgs) error {
	running, err := w.running.CountLiveForSchedule(ctx, schedule.ID)
	if err != nil {
		return fmt.Errorf("failed to check what schedule %q is already running: %w", schedule.Name, err)
	}
	if running > 0 {
		return river.JobCancel(fmt.Errorf("schedule %q still has %d agent(s) running", schedule.Name, running))
	}

	_, err = w.agents.NewAgent(ctx, agents.NewAgentParams{
		Title:          schedule.Name,
		Prompt:         schedule.Prompt,
		WorkflowID:     schedule.WorkflowID,
		ScheduleID:     xptr.Ptr(schedule.ID),
		Hidden:         schedule.HideAgents,
		IdempotencyKey: launchKey(schedule.ID, args.ScheduledFor),
	})

	if errors.Is(err, agents.ErrBadDirectory) ||
		errors.Is(err, agents.ErrWorkflowNotFound) ||
		errors.Is(err, agents.ErrProvisionFailed) ||
		errors.Is(err, agents.ErrTooManyAgents) {
		return river.JobCancel(fmt.Errorf("failed to launch schedule %q: %w", schedule.Name, err))
	}
	// An occurrence whose agent is already up completes rather than retrying: this attempt
	// launched nothing, but the firing it stands for is accounted for either way.
	if err != nil && !errors.Is(err, agents.ErrAlreadyLaunched) {
		return fmt.Errorf("failed to launch the agent for schedule %q: %w", schedule.Name, err)
	}

	return nil
}

// launchKey identifies one firing of one schedule, the schedule naming the recurring launch and
// the occurrence naming which run of it.
func launchKey(scheduleID string, scheduledFor time.Time) string {
	return "schedule:" + scheduleID + ":" + scheduledFor.UTC().Format(time.RFC3339)
}

func buildRunOutcome(err error, job *rivertype.JobRow) schedulestore.RunOutcome {
	var cancelled *river.JobCancelError

	switch {
	case err == nil:
		return schedulestore.RunOutcome{State: schedulestore.RunStateCompleted}
	case errors.As(err, &cancelled):
		return schedulestore.RunOutcome{State: schedulestore.RunStateCancelled, Message: errors.Unwrap(cancelled).Error()}
	case job.Attempt >= job.MaxAttempts:
		return schedulestore.RunOutcome{State: schedulestore.RunStateDiscarded, Message: err.Error()}
	}

	return schedulestore.RunOutcome{State: schedulestore.RunStateRetryable, Message: err.Error()}
}
