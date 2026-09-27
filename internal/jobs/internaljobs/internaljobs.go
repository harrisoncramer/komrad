// Package internaljobs holds the job arguments and the workers that do the work, so a new
// kind of job is a file here rather than a change to the client.
package internaljobs

import (
	"context"

	"komrad/internal/agents"
	"komrad/internal/mail"
	agentstore "komrad/internal/store/agent"
	mailstore "komrad/internal/store/mail"
	schedulestore "komrad/internal/store/schedule"
)

// QueueLaunchAgent, QueueDeleteAgent and QueueDeleteWorktrees are the queues, one per kind of job.
const (
	QueueLaunchAgent     = "launch_agent"
	QueueDeleteAgent     = "delete_agent"
	QueueDeleteWorktrees = "delete_worktrees"
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

// OrchestratorResolver answers the live orchestrator an orchestrator schedule mails.
type OrchestratorResolver interface {
	RequireOrchestrator(ctx context.Context, id string) (*agentstore.Agent, error)
}

type ScheduledMailer interface {
	SendScheduledMail(ctx context.Context, params mail.SendScheduledMailParams) (*mailstore.Mail, error)
}

type AgentDeleter interface {
	DeleteAgent(ctx context.Context, id string) error
	FailDelete(ctx context.Context, id string, cause error) error
}

type RunningAgentCounter interface {
	CountLiveForSchedule(ctx context.Context, scheduleID string) (int64, error)
}

// RunRecorder records when a firing starts and how it ends.
type RunRecorder interface {
	StartRun(ctx context.Context, params schedulestore.StartRunParams) error
	FinishRun(ctx context.Context, occurrence schedulestore.RunOccurrence, outcome schedulestore.RunOutcome) error
}
