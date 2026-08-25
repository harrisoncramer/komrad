// Package jobs owns the queue weave's background work runs on: the River client, the
// settings every job shares, and the ways of putting work on it. The jobs themselves live
// in internaljobs.
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

type ClientParams struct {
	Pool      *pgxpool.Pool
	Schedules internaljobs.ScheduleGetter
	Results   internaljobs.LaunchResultRecorder
	Launcher  internaljobs.AgentLauncher
	Deleter   internaljobs.AgentDeleter
	Archived  internaljobs.ArchivedAgentLister
	Running   internaljobs.RunningAgentCounter
	Logger    *slog.Logger
}

// NewClient builds the River client with the workers registered. It starts
// no periodic jobs. Those are added by the reconciler from the schedules table,
// since which ones exist is a question only the database can answer.
func NewClient(p ClientParams) (*river.Client[pgx.Tx], error) {
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
