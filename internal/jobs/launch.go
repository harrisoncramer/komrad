package jobs

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"weave/internal/agents"
	schedulestore "weave/internal/store/schedule"
	"weave/lib/xptr"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

type LaunchAgentArgs struct {
	ScheduleID   string    `json:"schedule_id"`
	ScheduledFor time.Time `json:"scheduled_for"`
}

// Kind is the River job kind.
func (LaunchAgentArgs) Kind() string { return "launch_agent" }

// InsertOpts dedups a firing by its arguments, so the same occurrence of the same schedule
// cannot be queued twice however it was inserted.
//
// The states are River's defaults plus the two ends it leaves out. Every way a firing
// declines to launch cancels the job, and one out of retries is discarded, so without them
// an occurrence could queue again the moment its first attempt stopped. River's cleaner
// eventually removes a finished job either way, which is why the idempotency key below
// rather than this is what makes a firing permanent.
func (LaunchAgentArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
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

type LaunchAgentWorker struct {
	river.WorkerDefaults[LaunchAgentArgs]
	schedules ScheduleReader
	agents    AgentLauncher
	running   RunningAgentCounter
	results   LaunchResultRecorder
	logger    *slog.Logger
}

// NewLaunchAgentWorker builds the worker over the schedules it reads and the service that
// launches an agent.
func NewLaunchAgentWorker(schedules ScheduleReader, launcher AgentLauncher, running RunningAgentCounter, results LaunchResultRecorder, logger *slog.Logger) *LaunchAgentWorker {
	return &LaunchAgentWorker{
		schedules: schedules,
		agents:    launcher,
		running:   running,
		results:   results,
		logger:    cmp.Or(logger, slog.Default()),
	}
}

// Work launches the agent the schedule describes and records what happened on every path.
func (w *LaunchAgentWorker) Work(ctx context.Context, job *river.Job[LaunchAgentArgs]) error {
	schedule, err := w.schedules.Get(ctx, job.Args.ScheduleID)
	if err != nil {
		return w.retry(ctx, job, fmt.Errorf("failed to read schedule %q: %w", job.Args.ScheduleID, err))
	}
	if schedule == nil {
		return w.cancel(ctx, job.Args.ScheduleID, schedulestore.LaunchResultScheduleNotFound,
			fmt.Errorf("schedule %q no longer exists", job.Args.ScheduleID))
	}
	if !schedule.Enabled {
		return w.cancel(ctx, schedule.ID, schedulestore.LaunchResultSchedulePaused,
			fmt.Errorf("schedule %q is paused", schedule.Name))
	}

	running, err := w.running.CountLiveForSchedule(ctx, schedule.ID)
	if err != nil {
		return w.retry(ctx, job, fmt.Errorf("failed to check what schedule %q is already running: %w", schedule.Name, err))
	}
	if running > 0 {
		return w.cancel(ctx, schedule.ID, schedulestore.LaunchResultAlreadyRunning,
			fmt.Errorf("schedule %q still has %d agent(s) running", schedule.Name, running))
	}

	_, err = w.agents.NewAgent(ctx, agents.NewAgentParams{
		Title:      schedule.Name,
		Prompt:     schedule.Prompt,
		WorkflowID: schedule.WorkflowID,
		ScheduleID: xptr.Ptr(schedule.ID),
		Hidden:     schedule.HideAgents,

		IdempotencyKey: LaunchKey(schedule.ID, job.Args.ScheduledFor),
	})

	if errors.Is(err, agents.ErrBadDirectory) ||
		errors.Is(err, agents.ErrWorkflowNotFound) ||
		errors.Is(err, agents.ErrSetupFailed) ||
		errors.Is(err, agents.ErrTooManyAgents) {
		return w.cancel(ctx, schedule.ID, schedulestore.LaunchResultCannotLaunch,
			fmt.Errorf("schedule %q cannot launch: %w", schedule.Name, err))
	}
	if err != nil {
		return w.retry(ctx, job, fmt.Errorf("failed to launch the agent for schedule %q: %w", schedule.Name, err))
	}

	w.record(ctx, schedule.ID, schedulestore.LaunchResultLaunched, "")

	return nil
}

// cancel records why a firing declined to launch and returns the cancel error for Work to
// return.
func (w *LaunchAgentWorker) cancel(ctx context.Context, scheduleID string, status schedulestore.LaunchResultStatus, err error) error {
	w.record(ctx, scheduleID, status, err.Error())

	return river.JobCancel(err)
}

// retry records a retryable failure, promoting it to a terminal status once no attempts remain.
func (w *LaunchAgentWorker) retry(ctx context.Context, job *river.Job[LaunchAgentArgs], err error) error {
	status := schedulestore.LaunchResultRetrying
	if job.Attempt >= job.MaxAttempts {
		status = schedulestore.LaunchResultCannotLaunch
	}

	w.record(ctx, job.Args.ScheduleID, status, err.Error())

	return err
}

func (w *LaunchAgentWorker) record(ctx context.Context, scheduleID string, status schedulestore.LaunchResultStatus, message string) {
	if err := w.results.RecordLaunchResult(ctx, scheduleID, status, message); err != nil {
		w.logger.Error("failed to record a schedule's launch result", "schedule_id", scheduleID, "status", status, "error", err)
	}
}

// LaunchKey identifies one firing of one schedule: the schedule says which recurring
// launch, the occurrence says which run of it, so tomorrow's 9am is a different agent while
// a retry of today's is the same one.
func LaunchKey(scheduleID string, scheduledFor time.Time) string {
	return "schedule:" + scheduleID + ":" + scheduledFor.UTC().Format(time.RFC3339)
}
