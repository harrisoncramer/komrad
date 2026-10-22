package internaljobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"komrad/internal/checklog"
	"komrad/internal/forge"
	"komrad/internal/git"
	"komrad/internal/store"
	pullrequeststore "komrad/internal/store/pullrequest"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// QueueFetchCheckLog is the queue of check logs to fetch from the forge.
const QueueFetchCheckLog = "fetch_check_log"

const (
	fetchCheckLogMaxAttempts = 8
	fetchCheckLogTimeout     = 10 * time.Minute
	// logReadyGrace is how long after a check finishes the forge may still answer that it has no log.
	logReadyGrace     = 10 * time.Minute
	minNotReadySnooze = 30 * time.Second
	minThrottleSnooze = time.Minute
)

// FetchCheckLogArgs fetches the log of one finished check and keeps it.
type FetchCheckLogArgs struct {
	CheckID uuid.UUID `json:"check_id"`
}

// Kind is the River job kind.
func (FetchCheckLogArgs) Kind() string {
	return "fetch_check_log"
}

// InsertOpts folds a fetch into one already waiting for the same check.
func (FetchCheckLogArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       QueueFetchCheckLog,
		MaxAttempts: fetchCheckLogMaxAttempts,
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

type ForgeFinder interface {
	ForHost(ctx context.Context, host string) (forge.Forge, error)
}

type NewFetchCheckLogWorkerParams struct {
	Store  *store.Pool
	Forges ForgeFinder
	// MaxBytes is the most of a log komrad keeps, read for each fetch, past which a log is kept as too large.
	MaxBytes func() (int64, error)
	Now      func() time.Time
}

type FetchCheckLogWorker struct {
	river.WorkerDefaults[FetchCheckLogArgs]
	store    *store.Pool
	forges   ForgeFinder
	maxBytes func() (int64, error)
	now      func() time.Time
	mu       sync.Mutex
	limits   map[string]forge.RateLimit
}

// NewFetchCheckLogWorker builds the worker that fetches check logs.
func NewFetchCheckLogWorker(params NewFetchCheckLogWorkerParams) *FetchCheckLogWorker {
	now := params.Now
	if now == nil {
		now = time.Now
	}

	return &FetchCheckLogWorker{
		store:    params.Store,
		forges:   params.Forges,
		maxBytes: params.MaxBytes,
		now:      now,
		limits:   map[string]forge.RateLimit{},
	}
}

// Timeout outlasts reading the largest log komrad keeps.
func (w *FetchCheckLogWorker) Timeout(*river.Job[FetchCheckLogArgs]) time.Duration {
	return fetchCheckLogTimeout
}

// Work fetches the check's log and keeps what the forge answered, holding off while the host's rate
// limit runs low or the forge has no log ready yet.
func (w *FetchCheckLogWorker) Work(ctx context.Context, job *river.Job[FetchCheckLogArgs]) error {
	check, err := pullrequeststore.NewStatusCheckStore(w.store.GetDB()).GetCheckForLogFetch(ctx, job.Args.CheckID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if check.State != string(forge.StatusCheckStateCompleted) || check.JobID == nil {
		return nil
	}

	now := w.now()
	if wait := w.waitFor(check.Host, now); wait > 0 {
		return river.JobSnooze(wait)
	}

	hostForge, err := w.forges.ForHost(ctx, check.Host)
	if err != nil {
		return w.failOrRetry(ctx, job, check.ID, err)
	}
	reader, ok := hostForge.(forge.JobLogReader)
	if !ok {
		return w.record(ctx, pullrequeststore.CheckLog{CheckID: check.ID, Outcome: pullrequeststore.CheckLogMissing})
	}

	repo, err := git.ParseRemoteURL(check.RepositoryURL)
	if err != nil {
		return w.failOrRetry(ctx, job, check.ID, err)
	}

	maxBytes, err := w.maxBytes()
	if err != nil {
		return w.failOrRetry(ctx, job, check.ID, err)
	}

	jobLog, err := reader.JobLog(ctx, repo, *check.JobID, maxBytes)
	var throttled *forge.ThrottledError
	switch {
	case errors.Is(err, forge.ErrExpired):
		return w.record(ctx, pullrequeststore.CheckLog{CheckID: check.ID, Outcome: pullrequeststore.CheckLogExpired})
	case errors.Is(err, forge.ErrNotFound):
		if check.FinishedAt != nil && now.Before(check.FinishedAt.Add(logReadyGrace)) {
			return river.JobSnooze(max(check.FinishedAt.Add(logReadyGrace).Sub(now), minNotReadySnooze))
		}

		return w.record(ctx, pullrequeststore.CheckLog{CheckID: check.ID, Outcome: pullrequeststore.CheckLogMissing})
	case errors.As(err, &throttled):
		return river.JobSnooze(max(throttled.RetryAt.Sub(now), minThrottleSnooze))
	case err != nil:
		return w.failOrRetry(ctx, job, check.ID, err)
	}

	w.setLimit(check.Host, jobLog.RateLimit)

	if jobLog.TooLarge {
		return w.record(ctx, pullrequeststore.CheckLog{CheckID: check.ID, Outcome: pullrequeststore.CheckLogTooLarge})
	}

	parsed, err := checklog.Parse(strings.NewReader(jobLog.Text), check.Steps)
	if err != nil {
		return w.failOrRetry(ctx, job, check.ID, err)
	}

	return w.record(ctx, pullrequeststore.CheckLog{
		CheckID:    check.ID,
		Outcome:    pullrequeststore.CheckLogFetched,
		Text:       parsed.Text,
		LineCount:  parsed.LineCount,
		ErrorCount: parsed.ErrorCount,
		Steps:      parsed.Steps,
	})
}

// waitFor is how long to hold off a host whose last answer left its budget low, and zero when it can be
// asked now.
func (w *FetchCheckLogWorker) waitFor(host string, now time.Time) time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()

	limit := w.limits[host]
	if !limit.IsLow() || limit.ResetAt.IsZero() || !limit.ResetAt.After(now) {
		return 0
	}

	return max(limit.ResetAt.Sub(now), minThrottleSnooze)
}

func (w *FetchCheckLogWorker) setLimit(host string, limit forge.RateLimit) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.limits[host] = limit
}

// failOrRetry keeps the log as failed on the last attempt, and otherwise hands the error to River to retry.
func (w *FetchCheckLogWorker) failOrRetry(ctx context.Context, job *river.Job[FetchCheckLogArgs], checkID uuid.UUID, err error) error {
	if job.Attempt < job.MaxAttempts {
		return err
	}

	if recordErr := w.record(context.WithoutCancel(ctx), pullrequeststore.CheckLog{CheckID: checkID, Outcome: pullrequeststore.CheckLogFailed}); recordErr != nil {
		return errors.Join(err, recordErr)
	}

	return nil
}

func (w *FetchCheckLogWorker) record(ctx context.Context, log pullrequeststore.CheckLog) error {
	log.FetchedAt = w.now()

	err := w.store.InTx(ctx, func(tx store.DBTX) error {
		return pullrequeststore.NewStatusCheckStore(tx).RecordCheckLog(ctx, log)
	})
	if err != nil {
		return fmt.Errorf("failed to keep the log of check %s: %w", log.CheckID, err)
	}

	return nil
}
