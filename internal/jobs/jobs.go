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
	"context"
	"fmt"
	"time"

	"claude-agents/internal/agents"
	agentstore "claude-agents/internal/store/agent"
	schedulestore "claude-agents/internal/store/schedule"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// schema is the Postgres schema River keeps its own tables in. It is separate
// from the application's so Atlas manages one schema and River manages the
// other, and neither migration ledger has to know about the other's tables.
const schema = "river"

// maxAttempts caps how many times a failed job is retried. River's default of 25
// lets a launch that can never work churn for hours. A handful of attempts is
// enough to ride out a tmux server that is briefly not there.
const maxAttempts = 5

// jobTimeout bounds one attempt. Launching an agent opens a tmux window and
// types into it, which is fast, so a job still running after this is wedged
// rather than slow.
const jobTimeout = 2 * time.Minute

// Migrate brings River's own tables up to date, in its own schema. It runs at
// startup alongside the application's migrations, so a checkout that has never
// seen River gets the tables without anyone installing the River CLI.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %q", schema)); err != nil {
		return fmt.Errorf("failed to create the river schema: %w", err)
	}

	migrator, err := rivermigrate.New(riverpgxv5.New(pool), &rivermigrate.Config{Schema: schema})
	if err != nil {
		return fmt.Errorf("failed to build the river migrator: %w", err)
	}

	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("failed to migrate river: %w", err)
	}

	return nil
}

// ClientParams are what the background worker needs. The pool River runs on, the
// schedules it reads, and the service that actually launches an agent.
type ClientParams struct {
	Pool      *pgxpool.Pool
	Schedules ScheduleReader
	Agents    AgentLauncher
}

// NewClient builds the River client with the launch worker registered. It starts
// no periodic jobs. Those are added by the reconciler from the schedules table,
// since which ones exist is a question only the database can answer.
func NewClient(p ClientParams) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, NewLaunchAgentWorker(p.Schedules, p.Agents))

	client, err := river.NewClient(riverpgxv5.New(p.Pool), &river.Config{
		Schema:      schema,
		Workers:     workers,
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
