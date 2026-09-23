// Package jobs owns the queue weave's background work runs on, with the jobs themselves in
// internaljobs.
package jobs

import (
	"cmp"
	"fmt"
	"log/slog"
	"time"

	"weave/internal/jobs/internaljobs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

const maxAttempts = 5

const jobTimeout = 2 * time.Minute

// jobRetention is how long River keeps a finished job, which is how far back a schedule's runs
// can be read.
const jobRetention = 90 * 24 * time.Hour

type NewClientParams struct {
	Pool      *pgxpool.Pool
	Schema    string
	Schedules internaljobs.ScheduleGetter
	Launcher  internaljobs.AgentLauncher
	Deleter   internaljobs.AgentDeleter
	Archived  internaljobs.ArchivedAgentLister
	Running   internaljobs.RunningAgentCounter
	Worktrees internaljobs.WorktreeLister
	Remover   internaljobs.WorktreeRemover
	Logger    *slog.Logger
}

// NewClient builds the River client with the workers registered, leaving the periodic jobs to
// the reconciler that reads them out of the schedules table.
func NewClient(params NewClientParams) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, internaljobs.NewLaunchAgentWorker(params.Schedules, params.Launcher, params.Running))
	river.AddWorker(workers, internaljobs.NewEmptyArchiveWorker(params.Archived, params.Deleter))
	river.AddWorker(workers, internaljobs.NewDeleteWorktreesWorker(internaljobs.NewDeleteWorktreesWorkerParams{
		Worktrees: params.Worktrees,
		Deleter:   params.Deleter,
		Remover:   params.Remover,
		Logger:    params.Logger,
	}))

	client, err := river.NewClient(riverpgxv5.New(params.Pool), &river.Config{
		Schema:                      params.Schema,
		Workers:                     workers,
		Logger:                      cmp.Or(params.Logger, slog.Default()),
		MaxAttempts:                 maxAttempts,
		JobTimeout:                  jobTimeout,
		CompletedJobRetentionPeriod: jobRetention,
		CancelledJobRetentionPeriod: jobRetention,
		DiscardedJobRetentionPeriod: jobRetention,
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 1},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create the river client: %w", err)
	}

	return client, nil
}
