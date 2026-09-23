package jobs

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"weave/internal/jobs/internaljobs"
	"weave/internal/logging"

	schedulestore "weave/internal/store/schedule"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"
)

// cancelPrefix is what River's cancel error writes ahead of the reason a skip gave.
const cancelPrefix = "JobCancelError: "

type scheduleReader interface {
	List(ctx context.Context) ([]schedulestore.Schedule, error)
	ListByKey(ctx context.Context, ids []string) ([]schedulestore.Schedule, error)
}

type scheduleNotifier interface {
	SendScheduleChange(ctx context.Context, scheduleID string) error
}

// RunLister reads a schedule's firings out of River's own record of its launch jobs.
type RunLister struct {
	client *river.Client[pgx.Tx]
}

// NewRunLister builds a lister over a River client that works no queues of its own, so it can
// be opened before the client that does.
func NewRunLister(pool *pgxpool.Pool, schema string) (*RunLister, error) {
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{Schema: schema})
	if err != nil {
		return nil, fmt.Errorf("failed to create the river client runs are read through: %w", err)
	}

	return &RunLister{client: client}, nil
}

// List returns a schedule's most recent firings, newest first.
func (l *RunLister) List(ctx context.Context, scheduleID string, limit int) ([]schedulestore.Run, error) {
	params := river.NewJobListParams().
		Kinds(internaljobs.LaunchAgentArgs{}.Kind()).
		Where("args->>'schedule_id' = @schedule_id", river.NamedArgs{"schedule_id": scheduleID}).
		OrderBy(river.JobListOrderByID, river.SortOrderDesc).
		First(limit)

	result, err := l.client.JobList(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list the runs of schedule %q: %w", scheduleID, err)
	}

	runs := make([]schedulestore.Run, 0, len(result.Jobs))
	for _, job := range result.Jobs {
		run, err := runFromJob(job)
		if err != nil {
			return nil, err
		}

		runs = append(runs, *run)
	}

	return runs, nil
}

// AttachLatest sets each schedule's newest firing on it.
func (l *RunLister) AttachLatest(ctx context.Context, schedules []schedulestore.Schedule) ([]schedulestore.Schedule, error) {
	for i := range schedules {
		latest, err := l.List(ctx, schedules[i].ID, 1)
		if err != nil {
			return nil, err
		}
		if len(latest) > 0 {
			schedules[i].LatestRun = &latest[0]
		}
	}

	return schedules, nil
}

// LatestRunReader reads schedules with their newest firing on them, so a watched schedule row
// changes when a firing does.
type LatestRunReader struct {
	schedules scheduleReader
	runs      *RunLister
}

// NewLatestRunReader builds the reader over the schedules and the runs of them.
func NewLatestRunReader(schedules scheduleReader, runs *RunLister) *LatestRunReader {
	return &LatestRunReader{schedules: schedules, runs: runs}
}

// List returns every live schedule with its newest firing.
func (r *LatestRunReader) List(ctx context.Context) ([]schedulestore.Schedule, error) {
	schedules, err := r.schedules.List(ctx)
	if err != nil {
		return nil, err
	}

	return r.runs.AttachLatest(ctx, schedules)
}

// ListByKey returns the live schedules among ids with their newest firing.
func (r *LatestRunReader) ListByKey(ctx context.Context, ids []string) ([]schedulestore.Schedule, error) {
	schedules, err := r.schedules.ListByKey(ctx, ids)
	if err != nil {
		return nil, err
	}

	return r.runs.AttachLatest(ctx, schedules)
}

// NotifyFinishedRuns tells the watch about each launch job River finishes until the client stops,
// since finishing one writes no row of weave's own.
func NotifyFinishedRuns(ctx context.Context, client *river.Client[pgx.Tx], notifier scheduleNotifier, logger *slog.Logger) {
	logger = cmp.Or(logger, slog.Default())
	events, cancel := client.Subscribe(river.EventKindJobCompleted, river.EventKindJobCancelled, river.EventKindJobFailed)

	go func() {
		defer cancel()

		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					return
				}

				notifyFinishedRun(ctx, event, notifier, logger)
			}
		}
	}()
}

func notifyFinishedRun(ctx context.Context, event *river.Event, notifier scheduleNotifier, logger *slog.Logger) {
	if event.Job == nil || event.Job.Kind != (internaljobs.LaunchAgentArgs{}).Kind() {
		return
	}

	var args internaljobs.LaunchAgentArgs
	if err := json.Unmarshal(event.Job.EncodedArgs, &args); err != nil {
		logger.LogAttrs(ctx, slog.LevelWarn, "failed to read a finished launch job's arguments", logging.Err(err)...)

		return
	}

	if err := notifier.SendScheduleChange(ctx, args.ScheduleID); err != nil {
		logger.LogAttrs(ctx, slog.LevelWarn, "failed to notify the watch of a finished run",
			append([]slog.Attr{slog.String(logging.ScheduleID, args.ScheduleID)}, logging.Err(err)...)...)
	}
}

func runFromJob(job *rivertype.JobRow) (*schedulestore.Run, error) {
	var args internaljobs.LaunchAgentArgs
	if err := json.Unmarshal(job.EncodedArgs, &args); err != nil {
		return nil, fmt.Errorf("failed to read the arguments of launch job %d: %w", job.ID, err)
	}

	run := &schedulestore.Run{
		ID:           job.ID,
		ScheduledFor: args.ScheduledFor,
		State:        schedulestore.RunState(job.State),
		Attempt:      job.Attempt,
		MaxAttempts:  job.MaxAttempts,
		FinalizedAt:  job.FinalizedAt,
	}

	if job.State != rivertype.JobStateCompleted && len(job.Errors) > 0 {
		run.Message = strings.TrimPrefix(job.Errors[len(job.Errors)-1].Error, cancelPrefix)
	}

	return run, nil
}
