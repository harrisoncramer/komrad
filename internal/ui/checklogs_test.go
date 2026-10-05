package ui_test

import (
	"fmt"
	"testing"

	"komrad/lib/xslice"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	"komrad/proto/gen/pullrequest/v1/pullrequestv1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
)

const checkLogsScreen = "the check logs"

func newLoggedCheck(id, name, conclusion string, lines []string, steps ...*pullrequestv1.CheckLogStep) *pullrequestv1.StatusCheck {
	check := newStatusCheck(id, name, pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED, conclusion)
	check.Id = "check-" + id
	check.Log = &pullrequestv1.CheckLog{
		State:     pullrequestv1.CheckLogState_CHECK_LOG_STATE_FETCHED,
		LineCount: int32(len(lines)),
		Steps:     steps,
	}

	return check
}

func newCheckLogStep(number int32, name, conclusion string, firstLine, lineCount int32) *pullrequestv1.CheckLogStep {
	return &pullrequestv1.CheckLogStep{
		Number:     number,
		Name:       name,
		Conclusion: conclusion,
		FirstLine:  firstLine,
		LineCount:  lineCount,
	}
}

func numberedLines(prefix string, n int) []string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("%s %d", prefix, i+1)
	}

	return lines
}

// answerCheckLogs answers each read of a check's log with the lines it asks for.
func answerCheckLogs(logs map[string][]string) func(*panelHarness) {
	return func(harness *panelHarness) {
		harness.answer(pullrequestv1connect.PullRequestServiceReadCheckLogProcedure, func(req connect.AnyRequest) (connect.AnyResponse, error) {
			read := req.Any().(*pullrequestv1.ReadCheckLogRequest)
			lines := logs[read.GetCheckId()]
			first := min(int(read.GetFirstLine())-1, len(lines))
			last := min(first+int(read.GetCount()), len(lines))

			return connect.NewResponse(&pullrequestv1.ReadCheckLogResponse{
				LineCount: int32(len(lines)),
				Lines: xslice.Map(lines[first:last], func(text string) *pullrequestv1.CheckLogLine {
					return &pullrequestv1.CheckLogLine{Text: text}
				}),
			}), nil
		})
	}
}

func openCheckLogs(t *testing.T, checks []*pullrequestv1.StatusCheck, logs map[string][]string) *panelHarness {
	t.Helper()

	harness := openStatusChecks(t, func(harness *panelHarness) {
		answerStatusChecks(checks...)(harness)
		answerCheckLogs(logs)(harness)
	})
	harness.press("enter")

	return harness
}

// TestCheckLogsOpen covers the logs opening from the status checks screen.
func TestCheckLogsOpen(t *testing.T) {
	t.Parallel()

	lines := []string{"go vet ./...", "##[error]vet: unreachable code"}
	checks := []*pullrequestv1.StatusCheck{
		newLoggedCheck("1", "vet", "FAILURE", lines, newCheckLogStep(1, "run vet", "failure", 1, 2)),
		newLoggedCheck("2", "lint", "SUCCESS", []string{"lint ok"}),
	}
	logs := map[string][]string{"check-1": lines, "check-2": {"lint ok"}}

	t.Run("enter opens the logs", func(t *testing.T) {
		t.Parallel()

		harness := openCheckLogs(t, checks, logs)

		assert.Equal(t, checkLogsScreen, harness.readScreen())
		assert.Contains(t, harness.readFrame(), "status checks › logs")
	})

	t.Run("it reads the log of each check the scope shows", func(t *testing.T) {
		t.Parallel()

		harness := openCheckLogs(t, checks, logs)

		harness.assertSentTo(pullrequestv1connect.PullRequestServiceReadCheckLogProcedure, &pullrequestv1.ReadCheckLogRequest{CheckId: "check-1", FirstLine: 1, Count: 2})
	})

	t.Run("each check is a row with its steps under it", func(t *testing.T) {
		t.Parallel()

		harness := openCheckLogs(t, checks, logs)

		assert.Equal(t, []string{"vet", "  └ run vet"}, readListRows(harness.readFrame()))
	})

	t.Run("each check is drawn under its header with its steps marked", func(t *testing.T) {
		t.Parallel()

		harness := openCheckLogs(t, checks, logs)

		frame := harness.readFrame()
		assert.Contains(t, frame, "═ vet ═")
		assert.Contains(t, frame, "▸ run vet")
		assert.Contains(t, frame, "##[error]vet: unreachable code")
		assert.NotContains(t, frame, "═ lint ═")
	})

	t.Run("widening the scope joins the passing check's log", func(t *testing.T) {
		t.Parallel()

		harness := openCheckLogs(t, checks, logs)
		harness.press(everyStatusCheck...)

		frame := harness.readFrame()
		assert.Contains(t, frame, "═ lint ═")
		assert.Contains(t, frame, "lint ok")
		harness.assertSentTo(pullrequestv1connect.PullRequestServiceReadCheckLogProcedure,
			&pullrequestv1.ReadCheckLogRequest{CheckId: "check-1", FirstLine: 1, Count: 2},
			&pullrequestv1.ReadCheckLogRequest{CheckId: "check-2", FirstLine: 1, Count: 1},
		)
	})

	t.Run("going back returns to the status checks", func(t *testing.T) {
		t.Parallel()

		harness := openCheckLogs(t, checks, logs)
		harness.press("esc")

		assert.Equal(t, statusChecksScreen, harness.readScreen())
	})
}

// TestCheckLogsReading covers a check's log read in chunks and the document moving to the row hovered.
func TestCheckLogsReading(t *testing.T) {
	t.Parallel()

	t.Run("a long log is read a chunk at a time", func(t *testing.T) {
		t.Parallel()

		lines := numberedLines("line", 7000)
		harness := openCheckLogs(t, []*pullrequestv1.StatusCheck{newLoggedCheck("1", "test", "FAILURE", lines)}, map[string][]string{"check-1": lines})

		harness.assertSentTo(pullrequestv1connect.PullRequestServiceReadCheckLogProcedure,
			&pullrequestv1.ReadCheckLogRequest{CheckId: "check-1", FirstLine: 1, Count: 5000},
			&pullrequestv1.ReadCheckLogRequest{CheckId: "check-1", FirstLine: 5001, Count: 2000},
		)
	})

	t.Run("hovering a step brings its header into view", func(t *testing.T) {
		t.Parallel()

		lines := numberedLines("line", 300)
		harness := openCheckLogs(t, []*pullrequestv1.StatusCheck{newLoggedCheck("1", "test", "FAILURE", lines,
			newCheckLogStep(1, "set up", "success", 1, 249),
			newCheckLogStep(2, "run tests", "failure", 250, 51),
		)}, map[string][]string{"check-1": lines})

		assert.NotContains(t, harness.readFrame(), "▸ run tests")

		harness.press("j", "j")

		frame := harness.readFrame()
		assert.Contains(t, frame, "▸ run tests")
		assert.Contains(t, frame, "line 250")
	})

	t.Run("searching the logs lands on what it found", func(t *testing.T) {
		t.Parallel()

		lines := append(numberedLines("line", 200), "panic: nil map")
		harness := openCheckLogs(t, []*pullrequestv1.StatusCheck{newLoggedCheck("1", "test", "FAILURE", lines)}, map[string][]string{"check-1": lines})
		harness.press("pick:block", "/", "p", "a", "n", "i", "c", "enter")

		frame := harness.readFrame()
		assert.Contains(t, frame, "panic: nil map")
		assert.Contains(t, frame, "1 of 1")
	})

	t.Run("a refused read says why", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, func(harness *panelHarness) {
			answerStatusChecks(newLoggedCheck("1", "test", "FAILURE", []string{"ok"}))(harness)
			harness.refuse(pullrequestv1connect.PullRequestServiceReadCheckLogProcedure, connect.NewError(connect.CodeUnavailable, nil))
		})
		harness.press("enter")

		assert.Contains(t, harness.readFrame(), "could not read")
	})
}

// TestCheckLogsNotices covers what is drawn under a check whose log cannot be read.
func TestCheckLogsNotices(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		state pullrequestv1.CheckLogState
		lines int32
		want  string
	}{
		{name: "a log not fetched yet", state: pullrequestv1.CheckLogState_CHECK_LOG_STATE_PENDING, want: "log not fetched yet"},
		{name: "a log too large to keep", state: pullrequestv1.CheckLogState_CHECK_LOG_STATE_TOO_LARGE, want: "log too large to keep"},
		{name: "a log the forge does not have", state: pullrequestv1.CheckLogState_CHECK_LOG_STATE_MISSING, want: "the forge has no log for this check"},
		{name: "a log expired on the forge", state: pullrequestv1.CheckLogState_CHECK_LOG_STATE_EXPIRED, want: "log expired on the forge"},
		{name: "a log that could not be fetched", state: pullrequestv1.CheckLogState_CHECK_LOG_STATE_FAILED, want: "log could not be fetched"},
		{name: "a fetched log with no lines", state: pullrequestv1.CheckLogState_CHECK_LOG_STATE_FETCHED, want: "empty log"},
		{name: "a check with no log", state: pullrequestv1.CheckLogState_CHECK_LOG_STATE_NONE, want: "no log"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			check := newLoggedCheck("1", "test", "FAILURE", nil)
			check.Log.State = tt.state
			harness := openCheckLogs(t, []*pullrequestv1.StatusCheck{check}, nil)

			assert.Contains(t, harness.readFrame(), tt.want)
			assert.Empty(t, harness.readSent(pullrequestv1connect.PullRequestServiceReadCheckLogProcedure))
		})
	}
}
