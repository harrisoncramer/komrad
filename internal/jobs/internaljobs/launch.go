package internaljobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"weave/internal/agents"
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
func (LaunchAgentArgs) Kind() string {
	return "launch_agent"
}

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
	runs      RunRecorder
	agents    AgentLauncher
	running   RunningAgentCounter
}

// NewLaunchAgentWorker builds the worker over the schedules it reads and the service that
// launches an agent.
func NewLaunchAgentWorker(schedules ScheduleGetter, runs RunRecorder, launcher AgentLauncher, running RunningAgentCounter) *LaunchAgentWorker {
	return &LaunchAgentWorker{
		schedules: schedules,
		runs:      runs,
		agents:    launcher,
		running:   running,
	}
}

// Work launches the agent the schedule describes, cancelling the job with its reason for every
// firing that deliberately launches nothing so River's record of the job says why.
func (w *LaunchAgentWorker) Work(ctx context.Context, job *river.Job[LaunchAgentArgs]) error {
	schedule, err := w.schedules.Get(ctx, job.Args.ScheduleID)
	if errors.Is(err, store.ErrNotFound) {
		return river.JobCancel(fmt.Errorf("schedule %q no longer exists", job.Args.ScheduleID))
	}
	if err != nil {
		return fmt.Errorf("failed to read schedule %q: %w", job.Args.ScheduleID, err)
	}

	occurrence := schedulestore.RunOccurrence{ScheduleID: schedule.ID, ScheduledFor: job.Args.ScheduledFor}
	if err := w.runs.StartRun(ctx, occurrence, job.Attempt, job.MaxAttempts); err != nil {
		return err
	}

	launched := w.launch(ctx, schedule, job.Args)
	if err := w.runs.FinishRun(ctx, occurrence, outcomeOf(launched, job.JobRow)); err != nil {
		return errors.Join(launched, err)
	}

	return launched
}

func (w *LaunchAgentWorker) launch(ctx context.Context, schedule *schedulestore.Schedule, args LaunchAgentArgs) error {
	if !schedule.Enabled {
		return river.JobCancel(fmt.Errorf("schedule %q is paused", schedule.Name))
	}

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

func outcomeOf(err error, job *rivertype.JobRow) schedulestore.RunOutcome {
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
