package pullrequest

import (
	"testing"

	"komrad/internal/forge"
	"komrad/internal/store"

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

func openCheckLogs(t *testing.T) *checkLogHarness {
	t.Helper()

	h := open(t)
	checks := NewStatusCheckStore(h.pool.GetDB())
	written, err := h.Upsert(t.Context(), openPullRequest("PR_1", 1, "agent/x"))
	require.NoError(t, err)
	require.NoError(t, checks.SetStatusChecks(t.Context(), SetStatusChecksParams{
		PullRequestID: written.ID,
		HeadSHA:       "c1",
		StatusChecks:  []forge.StatusCheck{inJob(statusCheckRun("1", "test", forge.StatusCheckStateCompleted, "failure"), 42)},
		ReadAt:        statusCheckReadAt,
	}))

	l := &checkLogHarness{harness: h, checks: checks, pullRequestID: written.ID}
	require.NoError(t, h.pool.GetDB().QueryRow(t.Context(), "SELECT id, reading_id FROM pull_request_checks WHERE provider_id = '1'").Scan(&l.checkID, &l.readingID))

	return l
}

func (h *checkLogHarness) record(t *testing.T, log checkLog) {
	t.Helper()

	log.CheckID = h.checkID
	log.FetchedAt = statusCheckReadAt
	require.NoError(t, h.pool.InTx(t.Context(), func(tx store.DBTX) error {
		return NewStatusCheckStore(tx).RecordCheckLog(t.Context(), log)
	}))
}

func (h *checkLogHarness) list(t *testing.T) []checkLog {
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

	var logs, parts int
	require.NoError(t, h.pool.GetDB().QueryRow(t.Context(), "SELECT (SELECT count(*) FROM pull_request_check_logs), (SELECT count(*) FROM pull_request_check_log_parts)").Scan(&logs, &parts))

	return logs, parts
}

func splitLog() checkLog {
	return checkLog{
		Outcome:   checkLogFetched,
		Split:     true,
		LineCount: 5,
		Parts: []checkLogPart{
			{StepNumber: 1, Text: "checkout\n", LineCount: 1},
			{StepNumber: 3, Text: "go test\n##[error]FAIL\nexit 1\n", LineCount: 3, ErrorCount: 1},
		},
	}
}

// TestCheckLogIsReplacedWhole covers that recording a check's log again keeps one log and only the new
// parts.
func TestCheckLogIsReplacedWhole(t *testing.T) {
	h := openCheckLogs(t)
	h.record(t, splitLog())
	h.record(t, checkLog{
		Outcome:   checkLogFetched,
		Truncated: true,
		LineCount: 2,
		Parts:     []checkLogPart{{StepNumber: 0, Text: "whole\nlog\n", LineCount: 2}},
	})

	logs, parts := h.count(t)
	assert.Equal(t, 1, logs)
	assert.Equal(t, 1, parts)

	listed := h.list(t)
	assert.Equal(t, []checkLog{{
		CheckID:   h.checkID,
		FetchedAt: statusCheckReadAt,
		Outcome:   checkLogFetched,
		Truncated: true,
		LineCount: 2,
		Parts:     []checkLogPart{{StepNumber: 0, LineCount: 2}},
	}}, listed)
}

// TestCheckLogsListWithoutText covers that the listing carries each log's outcome and part counts but no
// text, and a log with no parts lists none.
func TestCheckLogsListWithoutText(t *testing.T) {
	h := openCheckLogs(t)
	h.record(t, splitLog())

	listed := h.list(t)
	assert.Equal(t, []checkLog{{
		CheckID:   h.checkID,
		FetchedAt: statusCheckReadAt,
		Outcome:   checkLogFetched,
		Split:     true,
		LineCount: 5,
		Parts: []checkLogPart{
			{StepNumber: 1, LineCount: 1},
			{StepNumber: 3, LineCount: 3, ErrorCount: 1},
		},
	}}, listed)

	h.record(t, checkLog{Outcome: checkLogExpired})
	listed = h.list(t)
	require.Len(t, listed, 1)
	assert.Equal(t, checkLogExpired, listed[0].Outcome)
	assert.Empty(t, listed[0].Parts)
}

// TestCheckLogPartReadsByStep covers that a part reads back with its text by step number, and a step with
// no part is not found.
func TestCheckLogPartReadsByStep(t *testing.T) {
	h := openCheckLogs(t)
	h.record(t, splitLog())

	part, err := h.checks.GetCheckLogPart(t.Context(), h.checkID, 3)
	require.NoError(t, err)
	assert.Equal(t, &checkLogPart{StepNumber: 3, Text: "go test\n##[error]FAIL\nexit 1\n", LineCount: 3, ErrorCount: 1}, part)

	_, err = h.checks.GetCheckLogPart(t.Context(), h.checkID, 2)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

// TestCheckLogsGoWithThePullRequest covers that a check's log and its parts go when the pull request does.
func TestCheckLogsGoWithThePullRequest(t *testing.T) {
	h := openCheckLogs(t)
	h.record(t, splitLog())

	_, err := h.pool.GetDB().Exec(t.Context(), "DELETE FROM pull_requests WHERE id = $1", h.pullRequestID)
	require.NoError(t, err)

	logs, parts := h.count(t)
	assert.Zero(t, logs)
	assert.Zero(t, parts)
}
