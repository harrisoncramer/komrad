package internaljobs

import (
	"context"
	"errors"
	"time"

	"komrad/internal/agenterr"
	"komrad/internal/agents"

	"github.com/riverqueue/river"
)

// QueueArchiveAgent is the queue of agents to put away once their work is done.
const QueueArchiveAgent = "archive_agent"

// archiveDelay leaves a reviewer the moment it needs to end the turn that approved.
const archiveDelay = 5 * time.Second

// ArchiveAgentArgs archives a reviewer whose approval was released.
type ArchiveAgentArgs struct {
	AgentID string `json:"agent_id"`
}

// Kind is the River job kind.
func (ArchiveAgentArgs) Kind() string {
	return "archive_agent"
}

// InsertOpts holds an archive back for a moment so the approving turn ends first.
func (ArchiveAgentArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       QueueArchiveAgent,
		ScheduledAt: time.Now().Add(archiveDelay),
	}
}

type AgentArchiver interface {
	ArchiveAgents(ctx context.Context, params agents.ArchiveAgentsParams) error
}

type ArchiveAgentWorker struct {
	river.WorkerDefaults[ArchiveAgentArgs]
	archiver AgentArchiver
}

// NewArchiveAgentWorker builds the worker over the service that archives agents.
func NewArchiveAgentWorker(archiver AgentArchiver) *ArchiveAgentWorker {
	return &ArchiveAgentWorker{archiver: archiver}
}

// Work archives the agent, leaving one already deleted alone.
func (w *ArchiveAgentWorker) Work(ctx context.Context, job *river.Job[ArchiveAgentArgs]) error {
	err := w.archiver.ArchiveAgents(ctx, agents.ArchiveAgentsParams{IDs: []string{job.Args.AgentID}})
	if errors.Is(err, agenterr.ErrAgentNotFound) {
		return nil
	}

	return err
}
