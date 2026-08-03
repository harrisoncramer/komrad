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

// PurgeArchiveInterval is how often the archive is swept. The horizon is a number of hours,
// so an hourly pass is as fine as the setting it obeys.
const PurgeArchiveInterval = time.Hour

// ClientParams are what the background worker needs. The pool River runs on, the
// schedules it reads, and the service that actually launches an agent.
type ClientParams struct {
	Pool      *pgxpool.Pool
	Schedules ScheduleReader
	Agents    AgentLauncher
	Running   RunningAgentCounter
	Logger    *slog.Logger
	// Archive, Purger, and Tables are the read, the delete, and the vacuum a purge is made
	// of. PurgeAfter is how long an archived agent is kept, zero registering no purge.
	Archive    ArchiveReader
	Purger     AgentPurger
	Tables     TableMaintainer
	PurgeAfter time.Duration
}

// NewClient builds the River client with the launch and purge workers registered, and arms
// the purge when the config asks for one. Launches are not armed here, since which schedules
// exist is a question only the database can answer.
func NewClient(p ClientParams) (*river.Client[pgx.Tx], error) {
	logger := cmp.Or(p.Logger, slog.Default())

	workers := river.NewWorkers()
	river.AddWorker(workers, NewLaunchAgentWorker(p.Schedules, p.Agents, p.Running))
	river.AddWorker(workers, NewPurgeArchiveWorker(PurgeArchiveParams{
		Archive: p.Archive,
		Purger:  p.Purger,
		Tables:  p.Tables,
		Logger:  logger,
	}))

	client, err := river.NewClient(riverpgxv5.New(p.Pool), &river.Config{
		Schema:       db.RiverSchema,
		Workers:      workers,
		Logger:       logger,
		MaxAttempts:  maxAttempts,
		JobTimeout:   jobTimeout,
		PeriodicJobs: buildPurgeJobs(p.PurgeAfter),
		Queues: map[string]river.QueueConfig{
			// One at a time. A launch opens a tmux window, and a purge deletes the rows a
			// launch is writing beside.
			river.QueueDefault: {MaxWorkers: 1},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create the river client: %w", err)
	}

	return client, nil
}

// buildPurgeJobs is the purge as River's registration, and nothing at all when the horizon
// is zero. It runs on start as well as on the interval, since River does not backfill a
// firing the machine slept through and this is a laptop.
func buildPurgeJobs(after time.Duration) []*river.PeriodicJob {
	if after <= 0 {
		return nil
	}

	return []*river.PeriodicJob{
		river.NewPeriodicJob(
			river.PeriodicInterval(PurgeArchiveInterval),
			func() (river.JobArgs, *river.InsertOpts) {
				return PurgeArchiveArgs{After: after}, nil
			},
			&river.PeriodicJobOpts{RunOnStart: true},
		),
	}
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

// ArchiveReader is the two reads a purge makes: the batch it is about to take, and the same
// batch again to catch a restore that landed in between.
type ArchiveReader interface {
	ListArchivedBefore(ctx context.Context, cutoff time.Time, limit int32) ([]string, error)
	ListByID(ctx context.Context, ids []string) ([]agentstore.Agent, error)
}

// AgentPurger is how an agent is destroyed, which is the agent service rather than the
// store, so a purge takes one out through the call the archive's delete key makes.
type AgentPurger interface {
	DeleteAgents(ctx context.Context, ids []string) error
}

// TableMaintainer is what a purge needs of the database once it has deleted rows. Neither
// call is about any row, which is why they are the pool's rather than a store's.
type TableMaintainer interface {
	TotalRelationSize(ctx context.Context, tables ...string) (int64, error)
	Vacuum(ctx context.Context, tables ...string) error
}
