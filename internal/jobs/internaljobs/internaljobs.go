// Package internaljobs holds the jobs themselves: the arguments that identify a piece of
// work and the workers that do it. The package around it owns the queue they run on, so a
// new kind of job is a file here rather than a change to the client.
package internaljobs

import (
	"context"

	"weave/internal/agents"
	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"
)

// ScheduleGetter reads the one schedule a firing names, which is all the launch job knows
// about the table.
type ScheduleGetter interface {
	Get(ctx context.Context, id string) (*schedulestore.Schedule, error)
}

// ScheduleLister reads every schedule, which is what the reconciler compares River's
// registrations against.
type ScheduleLister interface {
	List(ctx context.Context) ([]schedulestore.Schedule, error)
}

type AgentLauncher interface {
	NewAgent(ctx context.Context, p agents.NewAgentParams) (*agentstore.Agent, error)
}

type AgentDeleter interface {
	DeleteAgents(ctx context.Context, ids []string) error
}

type ArchivedAgentLister interface {
	ListPage(ctx context.Context, limit int64, cursor *agentstore.Cursor, archived bool) ([]agentstore.Agent, error)
}

type RunningAgentCounter interface {
	CountLiveForSchedule(ctx context.Context, scheduleID string) (int64, error)
}

type LaunchResultRecorder interface {
	RecordLaunchResult(ctx context.Context, scheduleID string, status schedulestore.LaunchResultStatus, message string) error
}
