package internaljobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"komrad/internal/forge"
	"komrad/internal/git"
	pullrequeststore "komrad/internal/store/pullrequest"
	"komrad/internal/storetest"
	"komrad/lib/xptr"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var checkFinishedAt = time.Date(2026, time.September, 20, 12, 0, 0, 0, time.UTC)

type plainForge struct {
	forge.Forge
}

type logForge struct {
	forge.Forge
	log   *forge.JobLog
	err   error
	calls []int64
}

func (f *logForge) JobLog(_ context.Context, _ git.RemoteURL, jobID int64, _ int64) (*forge.JobLog, error) {
	f.calls = append(f.calls, jobID)
	if f.err != nil {
		return nil, f.err
	}

	return f.log, nil
}

type fakeForgeFinder struct {
	forge forge.Forge
	err   error
}

func (f fakeForgeFinder) ForHost(context.Context, string) (forge.Forge, error) {
	return f.forge, f.err
}

func seedCheckForLog(t *testing.T, stores *storetest.Stores, check forge.StatusCheck) string {
	t.Helper()
	ctx := t.Context()

	_, err := stores.PullRequests.Upsert(ctx, pullrequeststore.UpsertParams{
		Host:              "github.com",
		Forge:             "github",
		ProviderID:        "PR_1",
		URL:               "https://github.com/harrisoncramer/komrad/pull/1",
		RepositoryURL:     "https://github.com/harrisoncramer/komrad",
		HeadRepositoryURL: "https://github.com/harrisoncramer/komrad",
		Number:            1,
		Title:             "one",
		HeadBranch:        "agent/one",
		HeadSHA:           "abc",
		BaseBranch:        "main",
		BaseSHA:           "def",
		State:             "OPEN",
		OpenedAt:          checkFinishedAt.Add(-time.Hour),
		ChangedAt:         checkFinishedAt.Add(-time.Hour),
	})
	require.NoError(t, err)
	_, err = stores.PullRequests.Observe(ctx)
	require.NoError(t, err)

	pullRequest, err := stores.PullRequests.GetByIdentity(ctx, "github.com", "PR_1")
	require.NoError(t, err)

	require.NoError(t, pullrequeststore.NewStatusCheckStore(stores.Pool.GetDB()).SetStatusChecks(ctx, pullrequeststore.SetStatusChecksParams{
		PullRequestID: pullRequest.ID,
		HeadSHA:       "abc",
		StatusChecks:  []forge.StatusCheck{check},
		ReadAt:        checkFinishedAt,
	}))

	var checkID string
	require.NoError(t, stores.Pool.GetDB().QueryRow(ctx, "SELECT id FROM pull_request_checks WHERE provider_id = '1'").Scan(&checkID))

	return checkID
}

func finishedCheck() forge.StatusCheck {
	return forge.StatusCheck{
		ProviderID: "1",
		Kind:       forge.StatusCheckKindRun,
		Source:     "CI",
		Name:       "test",
		State:      forge.StatusCheckStateCompleted,
		Conclusion: "failure",
		StartedAt:  xptr.Ptr(checkFinishedAt.Add(-time.Minute)),
		FinishedAt: xptr.Ptr(checkFinishedAt),
		URL:        "https://github.com/harrisoncramer/komrad/runs/1",
		Steps: []forge.StatusCheckStep{
			{Number: 1, Name: "Set up job", State: forge.StatusCheckStateCompleted, Conclusion: "success", StartedAt: xptr.Ptr(checkFinishedAt.Add(-time.Minute))},
			{Number: 2, Name: "Run go test", State: forge.StatusCheckStateCompleted, Conclusion: "failure", StartedAt: xptr.Ptr(checkFinishedAt.Add(-30 * time.Second))},
		},
		RunID:    "CS_1",
		RunEvent: "pull_request",
		JobID:    xptr.Ptr(int64(42)),
	}
}

func keptLog(t *testing.T, stores *storetest.Stores, checkID string) *pullrequeststore.CheckLog {
	t.Helper()

	var log pullrequeststore.CheckLog
	var outcome string
	err := stores.Pool.GetDB().QueryRow(t.Context(), "SELECT outcome, text, line_count, error_count FROM pull_request_check_logs WHERE check_id = $1", checkID).
		Scan(&outcome, &log.Text, &log.LineCount, &log.ErrorCount)
	if err != nil && err.Error() == "no rows in result set" {
		return nil
	}
	require.NoError(t, err)
	log.Outcome = pullrequeststore.CheckLogOutcome(outcome)

	return &log
}

func fetchJob(checkID string, attempt int) *river.Job[FetchCheckLogArgs] {
	return &river.Job[FetchCheckLogArgs]{
		JobRow: &rivertype.JobRow{Attempt: attempt, MaxAttempts: fetchCheckLogMaxAttempts},
		Args:   FetchCheckLogArgs{CheckID: checkID},
	}
}

// TestFetchCheckLogWorker pins what each answer the forge gives keeps, and when the fetch holds off or
// retries instead.
func TestFetchCheckLogWorker(t *testing.T) {
	rawLog := "2026-09-20T11:59:00Z ##[group]Set up job\n2026-09-20T11:59:00Z ##[endgroup]\n2026-09-20T11:59:30Z ##[group]Run go test\n2026-09-20T11:59:31Z ##[error]boom\n"

	tests := []struct {
		name        string
		check       func() forge.StatusCheck
		forge       func() forge.Forge
		finderErr   error
		attempt     int
		now         time.Time
		wantErr     bool
		wantSnooze  time.Duration
		wantOutcome pullrequeststore.CheckLogOutcome
		wantText    string
		wantLines   int
		wantErrors  int
		wantNoCall  bool
	}{
		{
			name:        "a log the forge hands over is kept cleaned with its counts",
			forge:       func() forge.Forge { return &logForge{log: &forge.JobLog{Text: rawLog}} },
			now:         checkFinishedAt.Add(time.Minute),
			wantOutcome: pullrequeststore.CheckLogFetched,
			wantText:    "##[group]Set up job\n##[endgroup]\n##[group]Run go test\n##[error]boom\n",
			wantLines:   4,
			wantErrors:  1,
		},
		{
			name:        "a log past the most komrad keeps is kept as too large with no text",
			forge:       func() forge.Forge { return &logForge{log: &forge.JobLog{TooLarge: true}} },
			now:         checkFinishedAt.Add(time.Minute),
			wantOutcome: pullrequeststore.CheckLogTooLarge,
		},
		{
			name:        "a log the forge no longer keeps is kept as expired",
			forge:       func() forge.Forge { return &logForge{err: forge.ErrExpired} },
			now:         checkFinishedAt.Add(time.Minute),
			wantOutcome: pullrequeststore.CheckLogExpired,
		},
		{
			name:       "a log not ready soon after the check finished is fetched again once the grace runs out",
			forge:      func() forge.Forge { return &logForge{err: forge.ErrNotFound} },
			now:        checkFinishedAt.Add(time.Minute),
			wantSnooze: 9 * time.Minute,
		},
		{
			name:       "a log not ready at the end of the grace waits the least snooze",
			forge:      func() forge.Forge { return &logForge{err: forge.ErrNotFound} },
			now:        checkFinishedAt.Add(logReadyGrace - time.Second),
			wantSnooze: minNotReadySnooze,
		},
		{
			name:        "a log not ready past the grace is kept as missing",
			forge:       func() forge.Forge { return &logForge{err: forge.ErrNotFound} },
			now:         checkFinishedAt.Add(logReadyGrace),
			wantOutcome: pullrequeststore.CheckLogMissing,
		},
		{
			name:        "a forge that serves no logs keeps the log as missing",
			forge:       func() forge.Forge { return plainForge{} },
			now:         checkFinishedAt.Add(time.Minute),
			wantOutcome: pullrequeststore.CheckLogMissing,
		},
		{
			name: "a throttled fetch waits until the forge says",
			forge: func() forge.Forge {
				return &logForge{err: &forge.ThrottledError{RetryAt: checkFinishedAt.Add(5 * time.Minute)}}
			},
			now:        checkFinishedAt.Add(time.Minute),
			wantSnooze: 4 * time.Minute,
		},
		{
			name:    "a failed fetch before the last attempt is retried",
			forge:   func() forge.Forge { return &logForge{err: errors.New("the forge is away")} },
			attempt: 1,
			now:     checkFinishedAt.Add(time.Minute),
			wantErr: true,
		},
		{
			name:        "a failed fetch on the last attempt is kept as failed",
			forge:       func() forge.Forge { return &logForge{err: errors.New("the forge is away")} },
			attempt:     fetchCheckLogMaxAttempts,
			now:         checkFinishedAt.Add(time.Minute),
			wantOutcome: pullrequeststore.CheckLogFailed,
		},
		{
			name:      "a host komrad cannot reach before the last attempt is retried",
			forge:     func() forge.Forge { return &logForge{} },
			finderErr: errors.New("no credential for github.com"),
			attempt:   1,
			now:       checkFinishedAt.Add(time.Minute),
			wantErr:   true,
		},
		{
			name: "a check still running is left alone",
			check: func() forge.StatusCheck {
				check := finishedCheck()
				check.State = forge.StatusCheckStateRunning
				check.Conclusion = ""
				check.FinishedAt = nil

				return check
			},
			forge:      func() forge.Forge { return &logForge{log: &forge.JobLog{Text: rawLog}} },
			now:        checkFinishedAt.Add(time.Minute),
			wantNoCall: true,
		},
		{
			name: "a check no job ran is left alone",
			check: func() forge.StatusCheck {
				check := finishedCheck()
				check.JobID = nil

				return check
			},
			forge:      func() forge.Forge { return &logForge{log: &forge.JobLog{Text: rawLog}} },
			now:        checkFinishedAt.Add(time.Minute),
			wantNoCall: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stores := storetest.Open(t)

			check := finishedCheck()
			if tt.check != nil {
				check = tt.check()
			}
			checkID := seedCheckForLog(t, stores, check)

			hostForge := tt.forge()
			worker := NewFetchCheckLogWorker(NewFetchCheckLogWorkerParams{
				Pool:     stores.Pool,
				Forges:   fakeForgeFinder{forge: hostForge, err: tt.finderErr},
				MaxBytes: func() (int64, error) { return 1 << 20, nil },
				Now:      func() time.Time { return tt.now },
			})

			attempt := tt.attempt
			if attempt == 0 {
				attempt = 1
			}
			err := worker.Work(t.Context(), fetchJob(checkID, attempt))

			var snooze *rivertype.JobSnoozeError
			switch {
			case tt.wantSnooze > 0:
				require.ErrorAs(t, err, &snooze)
				assert.Equal(t, tt.wantSnooze, snooze.Duration)
			case tt.wantErr:
				require.Error(t, err)
				require.NotErrorAs(t, err, &snooze)
			default:
				require.NoError(t, err)
			}

			if fake, ok := hostForge.(*logForge); ok && tt.wantNoCall {
				assert.Empty(t, fake.calls)
			}

			kept := keptLog(t, stores, checkID)
			if tt.wantOutcome == "" {
				assert.Nil(t, kept)

				return
			}
			require.NotNil(t, kept)
			assert.Equal(t, tt.wantOutcome, kept.Outcome)
			assert.Equal(t, tt.wantText, kept.Text)
			assert.Equal(t, tt.wantLines, kept.LineCount)
			assert.Equal(t, tt.wantErrors, kept.ErrorCount)
		})
	}
}

// TestFetchCheckLogWorkerHoldsOffALowHost covers that a fetch whose answer left the host's budget low holds
// the next fetch on that host off until the budget comes back.
func TestFetchCheckLogWorkerHoldsOffALowHost(t *testing.T) {
	stores := storetest.Open(t)
	checkID := seedCheckForLog(t, stores, finishedCheck())

	now := checkFinishedAt.Add(time.Minute)
	hostForge := &logForge{log: &forge.JobLog{
		Text:      "2026-09-20T11:59:00Z ok\n",
		RateLimit: forge.RateLimit{Known: true, Remaining: 10, Limit: 5000, ResetAt: now.Add(20 * time.Minute)},
	}}
	worker := NewFetchCheckLogWorker(NewFetchCheckLogWorkerParams{
		Pool:     stores.Pool,
		Forges:   fakeForgeFinder{forge: hostForge},
		MaxBytes: func() (int64, error) { return 1 << 20, nil },
		Now:      func() time.Time { return now },
	})

	require.NoError(t, worker.Work(t.Context(), fetchJob(checkID, 1)))

	err := worker.Work(t.Context(), fetchJob(checkID, 1))
	var snooze *rivertype.JobSnoozeError
	require.ErrorAs(t, err, &snooze)
	assert.Equal(t, 20*time.Minute, snooze.Duration)
	assert.Len(t, hostForge.calls, 1)
}
