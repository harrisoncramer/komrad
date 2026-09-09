package internaljobs

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"weave/internal/agents"
	"weave/internal/logging"
	"weave/internal/store"
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

// InsertOpts dedups a firing by its arguments, adding the cancelled and discarded states River
// leaves out so a stopped attempt cannot queue the same occurrence again.
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
	schedules ScheduleGetter
	agents    AgentLauncher
	running   RunningAgentCounter
	results   LaunchResultRecorder
	logger    *slog.Logger
}

// NewLaunchAgentWorker builds the worker over the schedules it reads and the service that
// launches an agent.
func NewLaunchAgentWorker(schedules ScheduleGetter, launcher AgentLauncher, running RunningAgentCounter, results LaunchResultRecorder, logger *slog.Logger) *LaunchAgentWorker {
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
	if errors.Is(err, store.ErrNotFound) {
		return w.cancel(ctx, job.Args.ScheduleID, schedulestore.LaunchResultScheduleNotFound,
			fmt.Errorf("schedule %q no longer exists", job.Args.ScheduleID))
	}
	if err != nil {
		return w.retry(ctx, job, fmt.Errorf("failed to read schedule %q: %w", job.Args.ScheduleID, err))
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
		Title:          schedule.Name,
		Prompt:         schedule.Prompt,
		WorkflowID:     schedule.WorkflowID,
		ScheduleID:     xptr.Ptr(schedule.ID),
		Hidden:         schedule.HideAgents,
		IdempotencyKey: launchKey(schedule.ID, job.Args.ScheduledFor),
	})

	if errors.Is(err, agents.ErrBadDirectory) ||
		errors.Is(err, agents.ErrWorkflowNotFound) ||
		errors.Is(err, agents.ErrProvisionFailed) ||
		errors.Is(err, agents.ErrTooManyAgents) {
		return w.cancel(ctx, schedule.ID, schedulestore.LaunchResultCannotLaunch,
			fmt.Errorf("schedule %q cannot launch: %w", schedule.Name, err))
	}
	// An occurrence whose agent is already up is recorded rather than retried: this attempt
	// launched nothing, but the firing it stands for is accounted for either way, and an
	// earlier attempt that launched and then died before recording is corrected by saying so
	// here.
	if err != nil && !errors.Is(err, agents.ErrAlreadyLaunched) {
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
		w.logger.LogAttrs(ctx, slog.LevelError, "failed to record a schedule's launch result",
			append([]slog.Attr{
				slog.String(logging.ScheduleID, scheduleID),
				slog.String(logging.LaunchResult, string(status)),
			}, logging.Err(err)...)...)
	}
}

// launchKey identifies one firing of one schedule, the schedule naming the recurring launch and
// the occurrence naming which run of it.
func launchKey(scheduleID string, scheduledFor time.Time) string {
	return "schedule:" + scheduleID + ":" + scheduledFor.UTC().Format(time.RFC3339)
}
