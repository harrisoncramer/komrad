package internaljobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"komrad/internal/agenterr"
	"komrad/internal/agents"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
)

// QueueArchiveAgent is the queue of agents to put away once their work is done.
const QueueArchiveAgent = "archive_agent"

const (
	archiveDelay = 5 * time.Second
	archiveLimit = 10 * time.Minute
)

// ArchiveAgentArgs archives a reviewer whose approval was released.
type ArchiveAgentArgs struct {
	AgentID uuid.UUID `json:"agent_id"`
}

// Kind is the River job kind.
func (ArchiveAgentArgs) Kind() string {
	return "archive_agent"
}

// InsertOpts holds an archive back for a moment so the approving turn is under way first.
func (ArchiveAgentArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       QueueArchiveAgent,
		ScheduledAt: time.Now().Add(archiveDelay),
	}
}

type AgentArchiver interface {
	ArchiveAgents(ctx context.Context, params agents.ArchiveAgentsParams) error
}

type AgentGetter interface {
	GetByID(ctx context.Context, id uuid.UUID) (*agentstore.Agent, error)
}

type NewArchiveAgentWorkerParams struct {
	Archiver AgentArchiver
	Agents   AgentGetter
}

type ArchiveAgentWorker struct {
	river.WorkerDefaults[ArchiveAgentArgs]
	archiver AgentArchiver
	agents   AgentGetter
}

// NewArchiveAgentWorker builds the worker over the service that archives agents.
func NewArchiveAgentWorker(params NewArchiveAgentWorkerParams) *ArchiveAgentWorker {
	return &ArchiveAgentWorker{archiver: params.Archiver, agents: params.Agents}
}

// Work archives the agent once its turn has ended, or once it has waited out archiveLimit, leaving
// one already deleted alone.
func (w *ArchiveAgentWorker) Work(ctx context.Context, job *river.Job[ArchiveAgentArgs]) error {
	agent, err := w.agents.GetByID(ctx, job.Args.AgentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to look up agent %s: %w", job.Args.AgentID, err)
	}

	if agentstore.IsTurnUnfinished(agent.State) && time.Since(job.CreatedAt) < archiveLimit {
		return river.JobSnooze(archiveDelay)
	}

	err = w.archiver.ArchiveAgents(ctx, agents.ArchiveAgentsParams{IDs: []uuid.UUID{job.Args.AgentID}})
	if errors.Is(err, agenterr.ErrAgentNotFound) {
		return nil
	}

	return err
}
