// Package jobs runs the background work behind schedules. It holds the River
// client, the worker that launches an agent, and the reconciler that keeps
// River's periodic jobs matching the schedules table.
//
// River owns the queue, the retries, and the clock. It does not own the
// schedules themselves. Those are rows a person creates through the API, and the
// reconciler here is what turns them into periodic jobs, so a schedule written
// by the panel, the CLI, or an MCP tool is picked up the same way.
package jobs

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"time"

	"weave/db"
	"weave/internal/agents"
	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// maxAttempts caps how many times a failed job is retried. River's default of 25
// lets a launch that can never work churn for hours. A handful of attempts is
// enough to ride out a tmux server that is briefly not there.
const maxAttempts = 5

// jobTimeout bounds one attempt. Launching an agent opens a tmux window and
// types into it, which is fast, so a job still running after this is wedged
// rather than slow.
const jobTimeout = 2 * time.Minute

// ClientParams are what the background worker needs. The pool River runs on, the
// schedules it reads, and the service that actually launches an agent.
type ClientParams struct {
	Pool      *pgxpool.Pool
	Schedules ScheduleReader
	Agents    AgentLauncher
	Running   RunningAgentCounter
	Logger    *slog.Logger
}

// NewClient builds the River client with the launch worker registered. It starts
// no periodic jobs. Those are added by the reconciler from the schedules table,
// since which ones exist is a question only the database can answer.
func NewClient(p ClientParams) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, NewLaunchAgentWorker(p.Schedules, p.Agents, p.Running))

	client, err := river.NewClient(riverpgxv5.New(p.Pool), &river.Config{
		Schema:      db.RiverSchema,
		Workers:     workers,
		Logger:      cmp.Or(p.Logger, slog.Default()),
		MaxAttempts: maxAttempts,
		JobTimeout:  jobTimeout,
		Queues: map[string]river.QueueConfig{
			// One at a time. Every job here opens a tmux window, and a burst of
			// them racing would fight over the same manager session.
			river.QueueDefault: {MaxWorkers: 1},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create the river client: %w", err)
	}

	return client, nil
}

// ScheduleReader is the read side of the schedule store the background work
// needs. Nothing here writes a schedule, since firing one does not change it.
type ScheduleReader interface {
	List(ctx context.Context) ([]schedulestore.Schedule, error)
	Get(ctx context.Context, id string) (*schedulestore.Schedule, error)
}

// AgentLauncher is the one thing a fired schedule does. It is the same service
// the API calls, so a scheduled agent and one a person started are created by
// the same code under the same rules.
type AgentLauncher interface {
	NewAgent(ctx context.Context, p agents.NewAgentParams) (*agentstore.Agent, error)
}

// RunningAgentCounter answers whether a schedule already has work in flight. It is
// the agent store rather than the agent service, since this is a count of rows
// rather than anything about launching.
type RunningAgentCounter interface {
	CountLiveForSchedule(ctx context.Context, scheduleID string) (int64, error)
}
