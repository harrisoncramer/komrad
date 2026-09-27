// Package jobs owns the queue komrad's background work runs on, with the jobs themselves in
// internaljobs.
package jobs

import (
	"cmp"
	"fmt"
	"log/slog"
	"time"

	"komrad/internal/jobs/internaljobs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

const maxAttempts = 5

const jobTimeout = 2 * time.Minute

const deleteAgentWorkers = 4

type NewClientParams struct {
	Pool          *pgxpool.Pool
	Schema        string
	Schedules     internaljobs.ScheduleGetter
	Runs          internaljobs.RunRecorder
	Launcher      internaljobs.AgentLauncher
	Deleter       internaljobs.AgentDeleter
	Running       internaljobs.RunningAgentCounter
	Orchestrators internaljobs.OrchestratorResolver
	Mailer        internaljobs.ScheduledMailer
	Worktrees     internaljobs.WorktreeLister
	Remover       internaljobs.WorktreeRemover
	Logger        *slog.Logger
}

// NewClient builds the River client with the workers registered, leaving the periodic jobs to
// the reconciler that reads them out of the schedules table.
func NewClient(params NewClientParams) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, internaljobs.NewLaunchAgentWorker(internaljobs.NewLaunchAgentWorkerParams{
		Schedules:     params.Schedules,
		Runs:          params.Runs,
		Launcher:      params.Launcher,
		Running:       params.Running,
		Orchestrators: params.Orchestrators,
		Mailer:        params.Mailer,
	}))
	river.AddWorker(workers, internaljobs.NewDeleteAgentWorker(params.Deleter))
	river.AddWorker(workers, internaljobs.NewDeleteWorktreesWorker(internaljobs.NewDeleteWorktreesWorkerParams{
		Worktrees: params.Worktrees,
		Remover:   params.Remover,
		Logger:    params.Logger,
	}))

	client, err := river.NewClient(riverpgxv5.New(params.Pool), &river.Config{
		Schema:      params.Schema,
		Workers:     workers,
		Logger:      cmp.Or(params.Logger, slog.Default()),
		MaxAttempts: maxAttempts,
		JobTimeout:  jobTimeout,
		Queues: map[string]river.QueueConfig{
			internaljobs.QueueLaunchAgent:     {MaxWorkers: 1},
			internaljobs.QueueDeleteAgent:     {MaxWorkers: deleteAgentWorkers},
			internaljobs.QueueDeleteWorktrees: {MaxWorkers: 1},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create the river client: %w", err)
	}

	return client, nil
}

// NewInsertOnlyClient builds a River client that queues jobs and works none.
func NewInsertOnlyClient(pool *pgxpool.Pool, schema string) (*river.Client[pgx.Tx], error) {
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{Schema: schema})
	if err != nil {
		return nil, fmt.Errorf("failed to create the river insert client: %w", err)
	}

	return client, nil
}
