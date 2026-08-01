package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"weave/internal/agents"
	"weave/lib/xptr"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// LaunchAgentArgs is one firing of one schedule. ScheduledFor is the occurrence the job
// stands for rather than when it runs, so a retry, a restart, and a second insert for the
// same minute all carry the same value, which is what everything downstream keys on.
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

// LaunchAgentWorker starts the agent a schedule describes.
type LaunchAgentWorker struct {
	river.WorkerDefaults[LaunchAgentArgs]
	schedules ScheduleReader
	agents    AgentLauncher
	running   RunningAgents
}

// NewLaunchAgentWorker builds the worker over the schedules it reads and the service that
// launches an agent.
func NewLaunchAgentWorker(schedules ScheduleReader, launcher AgentLauncher, running RunningAgents) *LaunchAgentWorker {
	return &LaunchAgentWorker{schedules: schedules, agents: launcher, running: running}
}

// Work launches the agent the schedule describes. A schedule deleted or paused since the
// job was queued cancels rather than fails, since retrying would find the same answer.
// Anything else is returned as an error so River retries it, since a tmux server that is
// briefly gone is the common case and it comes back.
func (w *LaunchAgentWorker) Work(ctx context.Context, job *river.Job[LaunchAgentArgs]) error {
	schedule, err := w.schedules.Get(ctx, job.Args.ScheduleID)
	if err != nil {
		return fmt.Errorf("failed to read schedule %q: %w", job.Args.ScheduleID, err)
	}
	if schedule == nil {
		return river.JobCancel(fmt.Errorf("schedule %q no longer exists", job.Args.ScheduleID))
	}
	if !schedule.Enabled {
		return river.JobCancel(fmt.Errorf("schedule %q is paused", job.Args.ScheduleID))
	}

	// A schedule runs one agent at a time, or an hourly job that takes ninety minutes
	// would pile up a new agent beside the last one forever. The tick is skipped rather
	// than deferred, since the run already going picks up whatever this one would have.
	running, err := w.running.CountLiveForSchedule(ctx, schedule.ID)
	if err != nil {
		return fmt.Errorf("failed to check what schedule %q is already running: %w", schedule.Name, err)
	}
	if running > 0 {
		return river.JobCancel(fmt.Errorf("schedule %q still has %d agent(s) running", schedule.Name, running))
	}

	// Only the prompt, the provenance, and whether the run shows in the list come from the
	// schedule. Where and how the agent runs is the workflow's to say.
	_, err = w.agents.NewAgent(ctx, agents.NewAgentParams{
		Title:      schedule.Name,
		Prompt:     schedule.Prompt,
		WorkflowID: schedule.WorkflowID,
		ScheduleID: xptr.Ptr(schedule.ID),
		Hidden:     schedule.HideAgents,
		// The key is the occurrence rather than this attempt, so a retry after a launch
		// that half-succeeded finds the agent it already made.
		IdempotencyKey: LaunchKey(schedule.ID, job.Args.ScheduledFor),
	})

	// None of these fixes itself by trying again: an unopenable directory, a missing
	// workflow, and a refusing setup action all need the schedule or its workflow edited.
	// The setup action matters most, since five attempts at a script that provisions a
	// worktree and then refuses is five worktrees, where cancelling leaves one.
	//
	// A full panel joins them because only somebody archiving an agent empties it, which
	// makes the next occurrence the retry.

	if errors.Is(err, agents.ErrBadDirectory) ||
		errors.Is(err, agents.ErrWorkflowNotFound) ||
		errors.Is(err, agents.ErrSetupFailed) ||
		errors.Is(err, agents.ErrTooManyAgents) {
		return river.JobCancel(fmt.Errorf("schedule %q cannot launch: %w", schedule.Name, err))
	}
	if err != nil {
		return fmt.Errorf("failed to launch the agent for schedule %q: %w", schedule.Name, err)
	}

	return nil
}

// LaunchKey identifies one firing of one schedule: the schedule says which recurring
// launch, the occurrence says which run of it, so tomorrow's 9am is a different agent while
// a retry of today's is the same one.
func LaunchKey(scheduleID string, scheduledFor time.Time) string {
	return "schedule:" + scheduleID + ":" + scheduledFor.UTC().Format(time.RFC3339)
}
