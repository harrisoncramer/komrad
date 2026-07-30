package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"claude-agents/internal/agents"
	"claude-agents/lib/xptr"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// LaunchAgentArgs is one firing of one schedule.
//
// ScheduledFor is the occurrence the job stands for, not the moment it happens
// to run. It is what makes a firing identifiable, since a retry, a restart, or a
// second insert for the same minute all carry the same value, and everything
// downstream keys on it rather than on wall-clock time.
type LaunchAgentArgs struct {
	ScheduleID   string    `json:"schedule_id"`
	ScheduledFor time.Time `json:"scheduled_for"`
}

// Kind is the River job kind.
func (LaunchAgentArgs) Kind() string { return "launch_agent" }

// InsertOpts dedups a firing by its arguments, so the same occurrence of the
// same schedule cannot be queued twice however it came to be inserted.
//
// The states are the ones River defaults to plus the two ends it leaves out.
// Every way a firing declines to launch cancels the job rather than failing it,
// and one that runs out of retries is discarded, so without them an occurrence
// would be free to queue again the moment its first attempt stopped. River's
// cleaner eventually removes a finished job either way, which is why the
// idempotency key below rather than this is what makes a firing permanent.
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

// NewLaunchAgentWorker builds the worker over the schedules it reads and the
// service that launches an agent.
func NewLaunchAgentWorker(schedules ScheduleReader, launcher AgentLauncher, running RunningAgents) *LaunchAgentWorker {
	return &LaunchAgentWorker{schedules: schedules, agents: launcher, running: running}
}

// Work launches the agent the schedule describes.
//
// A schedule that has been deleted or paused since the job was queued cancels
// rather than fails. Nothing is wrong, the work is simply no longer wanted, and
// retrying would only find the same answer. A launch that fails for any other
// reason is returned as an error so River retries it, since a tmux server that
// is briefly gone is the common case and it comes back.
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

	// A schedule runs one agent at a time. A run that outlives its interval would
	// otherwise be lapped by the next tick, and an hourly job that takes ninety
	// minutes would pile up a new agent beside the last one forever.
	//
	// The tick is skipped rather than deferred: the work is a sweep of whatever is
	// outstanding, so the run already going will pick up anything this one would
	// have, and queueing a duplicate to start the moment it finishes only moves
	// the pile-up.
	running, err := w.running.CountLiveForSchedule(ctx, schedule.ID)
	if err != nil {
		return fmt.Errorf("failed to check what schedule %q is already running: %w", schedule.Name, err)
	}
	if running > 0 {
		return river.JobCancel(fmt.Errorf("schedule %q still has %d agent(s) running", schedule.Name, running))
	}

	// Only the prompt, the provenance, and whether the run shows in the list come from
	// the schedule. Where and how the agent runs is the workflow's to say, and the
	// service reads it from there.
	_, err = w.agents.NewAgent(ctx, agents.NewAgentParams{
		Title:      schedule.Name,
		Prompt:     schedule.Prompt,
		WorkflowID: schedule.WorkflowID,
		ScheduleID: xptr.Ptr(schedule.ID),
		Hidden:     schedule.HideAgents,
		// The key is the occurrence rather than this attempt, so a retry after a
		// launch that half-succeeded finds the agent it already made instead of
		// starting a second agent on the same prompt.
		IdempotencyKey: LaunchKey(schedule.ID, job.Args.ScheduledFor),
	})

	// Neither a directory that cannot be opened nor a workflow that has gone will
	// fix itself by trying again. Both need the schedule or its workflow edited,
	// so this stops rather than burning retries.
	if errors.Is(err, agents.ErrBadDirectory) || errors.Is(err, agents.ErrWorkflowNotFound) {
		return river.JobCancel(fmt.Errorf("schedule %q cannot launch: %w", schedule.Name, err))
	}
	if err != nil {
		return fmt.Errorf("failed to launch the agent for schedule %q: %w", schedule.Name, err)
	}

	return nil
}

// LaunchKey identifies one firing of one schedule. Both halves matter. The
// schedule says which recurring launch it is, and the occurrence says which run
// of it, so tomorrow's 9am is a different agent while a retry of today's is the
// same one.
func LaunchKey(scheduleID string, scheduledFor time.Time) string {
	return "schedule:" + scheduleID + ":" + scheduledFor.UTC().Format(time.RFC3339)
}
