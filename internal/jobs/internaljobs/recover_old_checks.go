package internaljobs

import (
	"context"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// QueueRecoverOldChecks is the queue of superseded head commits whose checks are read again until they finish.
const QueueRecoverOldChecks = "recover_old_checks"

// RecoverOldChecksDelays is the wait before each read of a superseded head's checks, one per step.
var RecoverOldChecksDelays = []time.Duration{
	2 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	30 * time.Minute,
	2 * time.Hour,
}

// RecoverOldChecksCap is how long after a head was superseded its checks are read at all, every step's wait
// with room for one more.
var RecoverOldChecksCap = sumRecoverOldChecksCap()

func sumRecoverOldChecksCap() time.Duration {
	var total time.Duration
	for _, delay := range RecoverOldChecksDelays {
		total += delay
	}

	return total + RecoverOldChecksDelays[len(RecoverOldChecksDelays)-1]
}

// RecoverOldChecksArgs reads the checks on a head a pull request no longer points at, so they settle on what
// the forge says of them.
type RecoverOldChecksArgs struct {
	PullRequestID string    `json:"pull_request_id"`
	HeadSHA       string    `json:"head_sha"`
	Step          int       `json:"step"`
	SupersededAt  time.Time `json:"superseded_at"`
}

// Kind is the River job kind.
func (RecoverOldChecksArgs) Kind() string {
	return "recover_old_checks"
}

// InsertOpts holds the read back for its step's wait and folds it into one already waiting.
func (a RecoverOldChecksArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       QueueRecoverOldChecks,
		ScheduledAt: time.Now().Add(RecoverOldChecksDelays[min(max(a.Step, 0), len(RecoverOldChecksDelays)-1)]),
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

// RecoverOutcome is what came of reading a superseded head's checks.
type RecoverOutcome struct {
	// Snooze is how long to wait before the same step reads again, zero when the step is done.
	Snooze time.Duration
}

type OldCheckRecoverer interface {
	RecoverOldChecks(ctx context.Context, args RecoverOldChecksArgs) (RecoverOutcome, error)
}

type RecoverOldChecksWorker struct {
	river.WorkerDefaults[RecoverOldChecksArgs]
	recoverer OldCheckRecoverer
}

// NewRecoverOldChecksWorker builds the worker over what reads a superseded head's checks.
func NewRecoverOldChecksWorker(recoverer OldCheckRecoverer) *RecoverOldChecksWorker {
	return &RecoverOldChecksWorker{recoverer: recoverer}
}

// Work reads the head's checks, snoozing the step when the forge cannot be asked yet.
func (w *RecoverOldChecksWorker) Work(ctx context.Context, job *river.Job[RecoverOldChecksArgs]) error {
	outcome, err := w.recoverer.RecoverOldChecks(ctx, job.Args)
	if err != nil {
		return err
	}
	if outcome.Snooze > 0 {
		return river.JobSnooze(outcome.Snooze)
	}

	return nil
}
