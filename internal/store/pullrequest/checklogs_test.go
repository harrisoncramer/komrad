package pullrequest

import (
	"strings"
	"testing"
	"time"

	"komrad/internal/checklog"
	"komrad/internal/forge"
	"komrad/internal/store"
	"komrad/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type checkLogHarness struct {
	*harness
	checks        *StatusCheckStore
	pullRequestID string
	checkID       string
	readingID     string
}

func openCheckLogs(t *testing.T, checks ...forge.StatusCheck) *checkLogHarness {
	t.Helper()

	if len(checks) == 0 {
		checks = []forge.StatusCheck{inJob(statusCheckRun("1", "test", forge.StatusCheckStateCompleted, "failure"), 42)}
	}

	h := open(t)
	store := NewStatusCheckStore(h.pool.GetDB())
	written, err := h.Upsert(t.Context(), openPullRequest("PR_1", 1, "agent/x"))
	require.NoError(t, err)
	require.NoError(t, store.SetStatusChecks(t.Context(), SetStatusChecksParams{
		PullRequestID: written.ID,
		HeadSHA:       "abc",
		StatusChecks:  checks,
		ReadAt:        statusCheckReadAt,
	}))

	l := &checkLogHarness{harness: h, checks: store, pullRequestID: written.ID}
	require.NoError(t, h.pool.GetDB().QueryRow(t.Context(), "SELECT id, reading_id FROM pull_request_checks WHERE provider_id = '1'").Scan(&l.checkID, &l.readingID))

	return l
}

func (h *checkLogHarness) record(t *testing.T, log CheckLog) {
	t.Helper()

	log.CheckID = h.checkID
	log.FetchedAt = statusCheckReadAt
	require.NoError(t, h.pool.InTx(t.Context(), func(tx store.DBTX) error {
		return NewStatusCheckStore(tx).RecordCheckLog(t.Context(), log)
	}))
}

func (h *checkLogHarness) list(t *testing.T) []CheckLog {
	t.Helper()

	listed, err := h.checks.ListCheckLogsOfReadings(t.Context(), []string{h.readingID})
	require.NoError(t, err)
	for i := range listed {
		listed[i].FetchedAt = listed[i].FetchedAt.UTC()
	}

	return listed
}

func (h *checkLogHarness) count(t *testing.T) (int, int) {
	t.Helper()

	var logs, steps int
	require.NoError(t, h.pool.GetDB().QueryRow(t.Context(), "SELECT (SELECT count(*) FROM pull_request_check_logs), (SELECT count(*) FROM pull_request_check_log_steps)").Scan(&logs, &steps))

	return logs, steps
}

func fetchedLog() CheckLog {
	return CheckLog{
		Outcome:    CheckLogFetched,
		Text:       "preamble\ncheckout\ngo test\n##[error]FAIL\nexit 1\n",
		LineCount:  5,
		ErrorCount: 1,
		Steps: []checklog.StepRange{
			{Number: 1, Name: "Run actions/checkout@v4", Conclusion: "success", FirstLine: 2, LineCount: 1},
			{Number: 3, Name: "Run go test", Conclusion: "failure", FirstLine: 3, LineCount: 3, ErrorCount: 1},
		},
	}
}

// TestCheckLogIsReplacedWhole covers that recording a check's log again keeps one log and only the new
// steps.
func TestCheckLogIsReplacedWhole(t *testing.T) {
	h := openCheckLogs(t)
	h.record(t, fetchedLog())
	h.record(t, CheckLog{
		Outcome:   CheckLogFetched,
		Text:      "whole\nlog\n",
		LineCount: 2,
		Steps:     []checklog.StepRange{{Number: 1, Name: "Set up job", Conclusion: "success", FirstLine: 1, LineCount: 2}},
	})

	logs, steps := h.count(t)
	assert.Equal(t, 1, logs)
	assert.Equal(t, 1, steps)

	lines, err := h.checks.GetCheckLogLines(t.Context(), h.checkID, 1, 10)
	require.NoError(t, err)
	assert.Equal(t, []string{"whole", "log"}, lines.Lines)
}

// TestCheckLogsListWithoutText covers that the listing carries each log's outcome, counts and steps but no
// text, and a log kept with no text lists no steps.
func TestCheckLogsListWithoutText(t *testing.T) {
	h := openCheckLogs(t)
	h.record(t, fetchedLog())

	want := fetchedLog()
	want.CheckID = h.checkID
	want.FetchedAt = statusCheckReadAt
	want.Text = ""
	assert.Equal(t, []CheckLog{want}, h.list(t))

	h.record(t, CheckLog{Outcome: CheckLogTooLarge})
	assert.Equal(t, []CheckLog{{CheckID: h.checkID, FetchedAt: statusCheckReadAt, Outcome: CheckLogTooLarge}}, h.list(t))
}

// TestGetCheckLogLines covers which lines each window of a log reads back.
func TestGetCheckLogLines(t *testing.T) {
	h := openCheckLogs(t)
	h.record(t, fetchedLog())

	tests := []struct {
		name      string
		firstLine int
		count     int
		want      []string
	}{
		{name: "a window inside the log", firstLine: 2, count: 2, want: []string{"checkout", "go test"}},
		{name: "a window past the end stops at the last line", firstLine: 4, count: 10, want: []string{"##[error]FAIL", "exit 1"}},
		{name: "a first line before the start reads from the first", firstLine: 0, count: 1, want: []string{"preamble"}},
		{name: "a first line past the end reads nothing", firstLine: 9, count: 3, want: []string{}},
		{name: "a count of nothing reads nothing", firstLine: 1, count: 0, want: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines, err := h.checks.GetCheckLogLines(t.Context(), h.checkID, tt.firstLine, tt.count)
			require.NoError(t, err)
			assert.Equal(t, tt.want, lines.Lines)
			assert.Equal(t, 5, lines.LineCount)
		})
	}
}

// TestGetCheckLogLinesOfNoText covers that a check with no log, or a log kept without its text, is not
// found.
func TestGetCheckLogLinesOfNoText(t *testing.T) {
	h := openCheckLogs(t)

	_, err := h.checks.GetCheckLogLines(t.Context(), h.checkID, 1, 10)
	require.ErrorIs(t, err, store.ErrNotFound)

	h.record(t, CheckLog{Outcome: CheckLogTooLarge})
	_, err = h.checks.GetCheckLogLines(t.Context(), h.checkID, 1, 10)
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestListCheckLogHits covers which lines a search finds and the step each sits in.
func TestListCheckLogHits(t *testing.T) {
	h := openCheckLogs(t)
	h.record(t, fetchedLog())

	tests := []struct {
		name    string
		pattern string
		want    []CheckLogHit
	}{
		{
			name:    "a line in a step carries its number",
			pattern: `##\[error\]`,
			want:    []CheckLogHit{{CheckID: h.checkID, Line: 4, StepNumber: 3, Text: "##[error]FAIL"}},
		},
		{
			name:    "a line before the first step is in none",
			pattern: `^pre`,
			want:    []CheckLogHit{{CheckID: h.checkID, Line: 1, Text: "preamble"}},
		},
		{
			name:    "matches come in line order",
			pattern: `t`,
			want: []CheckLogHit{
				{CheckID: h.checkID, Line: 2, StepNumber: 1, Text: "checkout"},
				{CheckID: h.checkID, Line: 3, StepNumber: 3, Text: "go test"},
				{CheckID: h.checkID, Line: 5, StepNumber: 3, Text: "exit 1"},
			},
		},
		{
			name:    "a pattern matching nothing finds nothing",
			pattern: `nowhere`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			search, err := h.checks.ListCheckLogHits(t.Context(), []string{h.checkID}, tt.pattern)
			require.NoError(t, err)
			assert.Equal(t, tt.want, search.Hits)
			assert.False(t, search.Limited)
		})
	}
}

// TestListCheckLogHitsLimits covers that a search stops at the most hits it answers with, and cuts a long
// line on a whole character.
func TestListCheckLogHitsLimits(t *testing.T) {
	h := openCheckLogs(t)
	long := strings.Repeat("é", maxHitRunes+1)
	h.record(t, CheckLog{
		Outcome:   CheckLogFetched,
		Text:      long + "\n" + strings.Repeat("hit\n", maxSearchHits),
		LineCount: maxSearchHits + 1,
	})

	search, err := h.checks.ListCheckLogHits(t.Context(), []string{h.checkID}, `é`)
	require.NoError(t, err)
	assert.Equal(t, []CheckLogHit{{CheckID: h.checkID, Line: 1, Text: strings.Repeat("é", maxHitRunes), Cut: true}}, search.Hits)

	search, err = h.checks.ListCheckLogHits(t.Context(), []string{h.checkID}, `.`)
	require.NoError(t, err)
	assert.Len(t, search.Hits, maxSearchHits)
	assert.True(t, search.Limited)
}

// TestListCheckLogHitsRefusesABadPattern covers that a pattern that does not compile is an invalid argument.
func TestListCheckLogHitsRefusesABadPattern(t *testing.T) {
	h := openCheckLogs(t)

	_, err := h.checks.ListCheckLogHits(t.Context(), []string{h.checkID}, `(`)
	require.ErrorIs(t, err, ErrInvalidPattern)
}

// TestGetCheckForLogFetch covers that a check reads back with its job, repository and steps, and an unknown
// check is not found.
func TestGetCheckForLogFetch(t *testing.T) {
	h := openCheckLogs(t, withSteps(
		inJob(statusCheckRun("1", "test", forge.StatusCheckStateCompleted, "failure"), 42),
		statusCheckStep(1, "Set up job", forge.StatusCheckStateCompleted, "success"),
		statusCheckStep(2, "Run go test", forge.StatusCheckStateCompleted, "failure"),
	))

	check, err := h.checks.GetCheckForLogFetch(t.Context(), h.checkID)
	require.NoError(t, err)
	for i := range check.Steps {
		check.Steps[i].StartedAt = xptr.Ptr(check.Steps[i].StartedAt.UTC())
	}
	assert.Equal(t, &checkForLogFetch{
		ID:            h.checkID,
		State:         string(forge.StatusCheckStateCompleted),
		JobID:         xptr.Ptr(int64(42)),
		Host:          "github.com",
		RepositoryURL: komrad,
		Steps: []checklog.Step{
			{Number: 1, Name: "Set up job", Conclusion: "success", StartedAt: xptr.Ptr(statusCheckReadAt.Add(-time.Minute))},
			{Number: 2, Name: "Run go test", Conclusion: "failure", StartedAt: xptr.Ptr(statusCheckReadAt.Add(-time.Minute))},
		},
	}, check)

	_, err = h.checks.GetCheckForLogFetch(t.Context(), "01a10000-0000-7000-8000-000000000000")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestListHeadChecksWantingLogs covers that only a finished check that ran in a job and has no log kept
// wants one.
func TestListHeadChecksWantingLogs(t *testing.T) {
	h := openCheckLogs(t,
		inJob(statusCheckRun("1", "test", forge.StatusCheckStateCompleted, "failure"), 42),
		inJob(statusCheckRun("2", "lint", forge.StatusCheckStateRunning, ""), 43),
		statusCheckRun("3", "app", forge.StatusCheckStateCompleted, "success"),
	)

	wanting, err := h.checks.ListHeadChecksWantingLogs(t.Context(), h.pullRequestID)
	require.NoError(t, err)
	assert.Equal(t, []string{h.checkID}, wanting)

	h.record(t, CheckLog{Outcome: CheckLogExpired})
	wanting, err = h.checks.ListHeadChecksWantingLogs(t.Context(), h.pullRequestID)
	require.NoError(t, err)
	assert.Empty(t, wanting)
}

// TestCheckLogsGoWithThePullRequest covers that a check's log and its steps go when the pull request does.
func TestCheckLogsGoWithThePullRequest(t *testing.T) {
	h := openCheckLogs(t)
	h.record(t, fetchedLog())

	_, err := h.pool.GetDB().Exec(t.Context(), "DELETE FROM pull_requests WHERE id = $1", h.pullRequestID)
	require.NoError(t, err)

	logs, steps := h.count(t)
	assert.Zero(t, logs)
	assert.Zero(t, steps)
}

// TestCheckLogIDsFromTheWire covers that an id that is no id finds no log rather than failing the read.
func TestCheckLogIDsFromTheWire(t *testing.T) {
	h := openCheckLogs(t)
	h.record(t, fetchedLog())

	_, err := h.checks.GetCheckLogLines(t.Context(), "check-1", 1, 10)
	require.ErrorIs(t, err, store.ErrNotFound)

	search, err := h.checks.ListCheckLogHits(t.Context(), []string{"check-1", h.checkID}, `FAIL`)
	require.NoError(t, err)
	assert.Equal(t, []CheckLogHit{{CheckID: h.checkID, Line: 4, StepNumber: 3, Text: "##[error]FAIL"}}, search.Hits)
}

// TestReadingsOfAPullRequestCarryTheirLogs covers that a pull request's readings name each check's id and
// carry the logs kept for them.
func TestReadingsOfAPullRequestCarryTheirLogs(t *testing.T) {
	h := openCheckLogs(t,
		inJob(statusCheckRun("1", "test", forge.StatusCheckStateCompleted, "failure"), 42),
		inJob(statusCheckRun("2", "lint", forge.StatusCheckStateCompleted, "success"), 43),
	)
	h.record(t, CheckLog{Outcome: CheckLogExpired})

	readings, err := h.checks.ListOfPullRequest(t.Context(), h.pullRequestID)
	require.NoError(t, err)
	require.Len(t, readings, 1)
	require.Len(t, readings[0].CheckIDs, 2)
	assert.Equal(t, h.checkID, readings[0].CheckIDs["1"])
	require.Contains(t, readings[0].Logs, h.checkID)
	assert.Equal(t, CheckLogExpired, readings[0].Logs[h.checkID].Outcome)
	assert.Len(t, readings[0].Logs, 1)
}

// TestLogOf covers where fetching a check's log stands for each check and log kept.
func TestLogOf(t *testing.T) {
	finished := inJob(statusCheckRun("1", "test", forge.StatusCheckStateCompleted, "failure"), 42)
	running := inJob(statusCheckRun("1", "test", forge.StatusCheckStateRunning, ""), 42)
	kept := func(outcome CheckLogOutcome) StatusCheckReading {
		return StatusCheckReading{
			CheckIDs: map[string]string{"1": "check-1"},
			Logs:     map[string]CheckLog{"check-1": {CheckID: "check-1", Outcome: outcome}},
		}
	}

	tests := []struct {
		name    string
		reading StatusCheckReading
		check   forge.StatusCheck
		want    CheckLogState
		wantLog bool
	}{
		{name: "a check in no job has none", reading: kept(CheckLogFetched), check: statusCheckRun("1", "app", forge.StatusCheckStateCompleted, "success"), want: CheckLogStateNone},
		{name: "a check still running is pending", reading: kept(CheckLogFetched), check: running, want: CheckLogStatePending},
		{name: "a finished check with no log kept is pending", reading: StatusCheckReading{CheckIDs: map[string]string{"1": "check-1"}}, check: finished, want: CheckLogStatePending},
		{name: "a fetched log", reading: kept(CheckLogFetched), check: finished, want: CheckLogState(CheckLogFetched), wantLog: true},
		{name: "a log too large to keep", reading: kept(CheckLogTooLarge), check: finished, want: CheckLogState(CheckLogTooLarge), wantLog: true},
		{name: "a log the forge never had", reading: kept(CheckLogMissing), check: finished, want: CheckLogState(CheckLogMissing), wantLog: true},
		{name: "a log the forge no longer keeps", reading: kept(CheckLogExpired), check: finished, want: CheckLogState(CheckLogExpired), wantLog: true},
		{name: "a log komrad gave up on", reading: kept(CheckLogFailed), check: finished, want: CheckLogState(CheckLogFailed), wantLog: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, log := tt.reading.GetLogOf(tt.check)
			assert.Equal(t, tt.want, state)
			assert.Equal(t, tt.wantLog, log != nil)
		})
	}
}
