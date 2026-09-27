package internaljobs

import (
	"context"
	"time"

	"github.com/riverqueue/river"
)

// deleteAgentTimeout covers an agent's teardown hooks and deprovisioner, and its children's.
const deleteAgentTimeout = 30 * time.Minute

type DeleteAgentArgs struct {
	AgentID string `json:"agent_id"`
}

// Kind is the River job kind.
func (DeleteAgentArgs) Kind() string {
	return "delete_agent"
}

// InsertOpts puts the job on its own queue.
func (DeleteAgentArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueDeleteAgent}
}

type DeleteAgentWorker struct {
	river.WorkerDefaults[DeleteAgentArgs]
	agents AgentDeleter
}

// NewDeleteAgentWorker builds the worker over the service that deletes agents.
func NewDeleteAgentWorker(deleter AgentDeleter) *DeleteAgentWorker {
	return &DeleteAgentWorker{agents: deleter}
}

// Timeout lets the teardown run longer than the queue's default.
func (w *DeleteAgentWorker) Timeout(*river.Job[DeleteAgentArgs]) time.Duration {
	return deleteAgentTimeout
}

// Work deletes one agent and its children, failing the agent's cleanup on the last attempt.
func (w *DeleteAgentWorker) Work(ctx context.Context, job *river.Job[DeleteAgentArgs]) error {
	err := w.agents.DeleteAgent(ctx, job.Args.AgentID)
	if err == nil || job.Attempt < job.MaxAttempts {
		return err
	}

	if failErr := w.agents.FailDelete(context.WithoutCancel(ctx), job.Args.AgentID, err); failErr != nil {
		return failErr
	}

	return err
}
