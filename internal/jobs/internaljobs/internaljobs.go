// Package internaljobs holds the job arguments and the workers that do the work, so a new
// kind of job is a file here rather than a change to the client.
package internaljobs

import (
	"context"

	"weave/internal/agents"
	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"
)

// ScheduleGetter reads the one schedule a firing names.
type ScheduleGetter interface {
	Get(ctx context.Context, id string) (*schedulestore.Schedule, error)
}

// ScheduleLister reads every schedule.
type ScheduleLister interface {
	ListWatched(ctx context.Context, keys []string) ([]schedulestore.Schedule, error)
}

type AgentLauncher interface {
	NewAgent(ctx context.Context, p agents.NewAgentParams) (*agentstore.Agent, error)
}

type AgentDeleter interface {
	DeleteAgents(ctx context.Context, ids []string) error
}

type ArchivedAgentLister interface {
	ListArchiveSweepPage(ctx context.Context, limit int64, cursor *agentstore.Cursor) ([]agentstore.Agent, error)
	ListByID(ctx context.Context, ids []string) ([]agentstore.Agent, error)
}

type RunningAgentCounter interface {
	CountLiveForSchedule(ctx context.Context, scheduleID string) (int64, error)
}

type RunRecorder interface {
	StartRun(ctx context.Context, occurrence schedulestore.RunOccurrence, attempt, maxAttempts int) error
	FinishRun(ctx context.Context, occurrence schedulestore.RunOccurrence, outcome schedulestore.RunOutcome) error
}
