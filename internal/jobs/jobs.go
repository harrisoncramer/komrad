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

const maxAttempts = 5

const jobTimeout = 2 * time.Minute

type ClientParams struct {
	Pool      *pgxpool.Pool
	Schedules ScheduleReader
	Results   LaunchResultRecorder
	Agents    AgentLauncher
	Running   RunningAgentCounter
	Logger    *slog.Logger
}

// NewClient builds the River client with the launch worker registered. It starts
// no periodic jobs. Those are added by the reconciler from the schedules table,
// since which ones exist is a question only the database can answer.
func NewClient(p ClientParams) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, NewLaunchAgentWorker(p.Schedules, p.Agents, p.Running, p.Results, p.Logger))

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

type ScheduleReader interface {
	List(ctx context.Context) ([]schedulestore.Schedule, error)
	Get(ctx context.Context, id string) (*schedulestore.Schedule, error)
}

type AgentLauncher interface {
	NewAgent(ctx context.Context, p agents.NewAgentParams) (*agentstore.Agent, error)
}

type RunningAgentCounter interface {
	CountLiveForSchedule(ctx context.Context, scheduleID string) (int64, error)
}

type LaunchResultRecorder interface {
	RecordLaunchResult(ctx context.Context, scheduleID string, status schedulestore.LaunchResultStatus, message string) error
}
