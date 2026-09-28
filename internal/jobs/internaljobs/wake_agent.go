package internaljobs

import (
	"context"
	"errors"
	"time"

	"komrad/internal/agents"
	"komrad/internal/scriptrun"
	agentstore "komrad/internal/store/agent"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// QueueWakeAgent is the queue of agents to put back in a pane.
const QueueWakeAgent = "wake_agent"

const (
	wakeDelay       = 5 * time.Second
	wakeMaxAttempts = 3
)

// WakeAgentArgs puts an agent back in a pane so the mail waiting for it is read.
type WakeAgentArgs struct {
	AgentID string `json:"agent_id"`
}

// Kind is the River job kind.
func (WakeAgentArgs) Kind() string {
	return "wake_agent"
}

// InsertOpts holds a wake back for a moment and folds it into one already waiting.
func (WakeAgentArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       QueueWakeAgent,
		MaxAttempts: wakeMaxAttempts,
		ScheduledAt: time.Now().Add(wakeDelay),
		UniqueOpts: river.UniqueOpts{
			ByArgs: true,
			ByState: []rivertype.JobState{
				rivertype.JobStateAvailable,
				rivertype.JobStatePending,
				rivertype.JobStateRetryable,
				rivertype.JobStateRunning,
				rivertype.JobStateScheduled,
			},
		},
	}
}

type AgentResumer interface {
	ResumeAgent(ctx context.Context, id string) (*agentstore.Agent, error)
}

type WakeAgentWorker struct {
	river.WorkerDefaults[WakeAgentArgs]
	resumer AgentResumer
}

// NewWakeAgentWorker builds the worker over the service that resumes agents.
func NewWakeAgentWorker(resumer AgentResumer) *WakeAgentWorker {
	return &WakeAgentWorker{resumer: resumer}
}

// Timeout outlasts the workflow's post-resume hook.
func (w *WakeAgentWorker) Timeout(*river.Job[WakeAgentArgs]) time.Duration {
	return scriptrun.HookTimeout + 5*time.Minute
}

// Work resumes the agent, leaving one that is gone, held or already being resumed alone.
func (w *WakeAgentWorker) Work(ctx context.Context, job *river.Job[WakeAgentArgs]) error {
	_, err := w.resumer.ResumeAgent(ctx, job.Args.AgentID)
	switch {
	case errors.Is(err, agents.ErrResumeInFlight),
		errors.Is(err, agents.ErrAgentArchived),
		errors.Is(err, agents.ErrAgentHeld),
		errors.Is(err, agents.ErrAgentNotFound):
		return nil
	default:
		return err
	}
}
