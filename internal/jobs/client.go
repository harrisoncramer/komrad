// Package jobs owns the queue weave's background work runs on, with the jobs themselves in
// internaljobs.
package jobs

import (
	"cmp"
	"fmt"
	"log/slog"
	"time"

	"weave/db"
	"weave/internal/jobs/internaljobs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

const maxAttempts = 5

const jobTimeout = 2 * time.Minute

type NewClientParams struct {
	Pool      *pgxpool.Pool
	Schedules internaljobs.ScheduleGetter
	Results   internaljobs.LaunchResultRecorder
	Launcher  internaljobs.AgentLauncher
	Deleter   internaljobs.AgentDeleter
	Archived  internaljobs.ArchivedAgentLister
	Running   internaljobs.RunningAgentCounter
	Logger    *slog.Logger
}

// NewClient builds the River client with the workers registered, leaving the periodic jobs
// to the reconciler that reads them out of the schedules table.
func NewClient(p NewClientParams) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, internaljobs.NewLaunchAgentWorker(p.Schedules, p.Launcher, p.Running, p.Results, p.Logger))
	river.AddWorker(workers, internaljobs.NewEmptyArchiveWorker(p.Archived, p.Deleter))

	client, err := river.NewClient(riverpgxv5.New(p.Pool), &river.Config{
		Schema:      db.RiverSchema,
		Workers:     workers,
		Logger:      cmp.Or(p.Logger, slog.Default()),
		MaxAttempts: maxAttempts,
		JobTimeout:  jobTimeout,
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 1},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create the river client: %w", err)
	}

	return client, nil
}
