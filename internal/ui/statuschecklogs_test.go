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

const statusCheckLogsScreen = "the status check logs"

func newLoggedStatusCheck(id, name, conclusion string, lines []string, steps ...*pullrequestv1.CheckLogStep) *pullrequestv1.StatusCheck {
	check := newStatusCheck(id, name, pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED, conclusion)
	check.Id = "check-" + id
	check.Log = &pullrequestv1.CheckLog{
		State:     pullrequestv1.CheckLogState_CHECK_LOG_STATE_FETCHED,
		LineCount: int32(len(lines)),
		Steps:     steps,
	}

	return check
}

func newStatusCheckLogStep(number int32, name, conclusion string, firstLine, lineCount int32) *pullrequestv1.CheckLogStep {
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

// answerStatusCheckLogs answers each read of a check's log with the lines it asks for.
func answerStatusCheckLogs(logs map[string][]string) func(*panelHarness) {
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

func openStatusCheckLogs(t *testing.T, checks []*pullrequestv1.StatusCheck, logs map[string][]string) *panelHarness {
	t.Helper()

	harness := openStatusChecks(t, func(harness *panelHarness) {
		answerStatusChecks(checks...)(harness)
		answerStatusCheckLogs(logs)(harness)
	})
	harness.press("enter")

	return harness
}

// TestStatusCheckLogsOpen covers the logs opening from the status checks screen.
func TestStatusCheckLogsOpen(t *testing.T) {
	t.Parallel()

	lines := []string{"go vet ./...", "##[error]vet: unreachable code"}
	checks := []*pullrequestv1.StatusCheck{
		newLoggedStatusCheck("1", "vet", "FAILURE", lines, newStatusCheckLogStep(1, "run vet", "failure", 1, 2)),
		newLoggedStatusCheck("2", "lint", "SUCCESS", []string{"lint ok"}),
	}
	logs := map[string][]string{"check-1": lines, "check-2": {"lint ok"}}

	t.Run("enter opens the logs", func(t *testing.T) {
		t.Parallel()

		harness := openStatusCheckLogs(t, checks, logs)

		assert.Equal(t, statusCheckLogsScreen, harness.readScreen())
		assert.Contains(t, harness.readFrame(), "status checks › logs")
	})

	t.Run("it reads the log of each check the scope shows", func(t *testing.T) {
		t.Parallel()

		harness := openStatusCheckLogs(t, checks, logs)

		harness.assertSentTo(pullrequestv1connect.PullRequestServiceReadCheckLogProcedure, &pullrequestv1.ReadCheckLogRequest{CheckId: "check-1", FirstLine: 1, Count: 2})
	})

	t.Run("each check is a row with its steps under it", func(t *testing.T) {
		t.Parallel()

		harness := openStatusCheckLogs(t, checks, logs)

		assert.Equal(t, []string{"vet", "  └ run vet"}, readListRows(harness.readFrame()))
	})

	t.Run("each check is drawn under its header with its steps marked", func(t *testing.T) {
		t.Parallel()

		harness := openStatusCheckLogs(t, checks, logs)

		frame := harness.readFrame()
		assert.Contains(t, frame, "═ vet ═")
		assert.Contains(t, frame, "▸ run vet")
		assert.Contains(t, frame, "##[error]vet: unreachable code")
		assert.NotContains(t, frame, "═ lint ═")
	})

	t.Run("widening the scope joins the passing check's log", func(t *testing.T) {
		t.Parallel()

		harness := openStatusCheckLogs(t, checks, logs)
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

		harness := openStatusCheckLogs(t, checks, logs)
		harness.press("esc")

		assert.Equal(t, statusChecksScreen, harness.readScreen())
	})
}

// TestStatusCheckLogsReading covers a check's log read in chunks and the document moving to the row hovered.
func TestStatusCheckLogsReading(t *testing.T) {
	t.Parallel()

	t.Run("a long log is read a chunk at a time", func(t *testing.T) {
		t.Parallel()

		lines := numberedLines("line", 7000)
		harness := openStatusCheckLogs(t, []*pullrequestv1.StatusCheck{newLoggedStatusCheck("1", "test", "FAILURE", lines)}, map[string][]string{"check-1": lines})

		harness.assertSentTo(pullrequestv1connect.PullRequestServiceReadCheckLogProcedure,
			&pullrequestv1.ReadCheckLogRequest{CheckId: "check-1", FirstLine: 1, Count: 5000},
			&pullrequestv1.ReadCheckLogRequest{CheckId: "check-1", FirstLine: 5001, Count: 2000},
		)
	})

	t.Run("hovering a step brings its header into view", func(t *testing.T) {
		t.Parallel()

		lines := numberedLines("line", 300)
		harness := openStatusCheckLogs(t, []*pullrequestv1.StatusCheck{newLoggedStatusCheck("1", "test", "FAILURE", lines,
			newStatusCheckLogStep(1, "set up", "success", 1, 249),
			newStatusCheckLogStep(2, "run tests", "failure", 250, 51),
		)}, map[string][]string{"check-1": lines})

		assert.NotContains(t, harness.readFrame(), "▸ run tests")

		harness.press("j", "j")

		frame := harness.readFrame()
		assert.Contains(t, frame, "▸ run tests")
		assert.Contains(t, frame, "line 250")
	})

	t.Run("a step that starts after the last line keeps its header", func(t *testing.T) {
		t.Parallel()

		lines := []string{"go test ./...", "ok"}
		harness := openStatusCheckLogs(t, []*pullrequestv1.StatusCheck{newLoggedStatusCheck("1", "test", "FAILURE", lines,
			newStatusCheckLogStep(1, "run tests", "failure", 1, 2),
			newStatusCheckLogStep(2, "post job", "success", 3, 0),
		)}, map[string][]string{"check-1": lines})

		assert.Contains(t, harness.readFrame(), "▸ post job")
	})

	t.Run("steps that start on the same line each keep a header", func(t *testing.T) {
		t.Parallel()

		lines := []string{"go test ./..."}
		harness := openStatusCheckLogs(t, []*pullrequestv1.StatusCheck{newLoggedStatusCheck("1", "test", "FAILURE", lines,
			newStatusCheckLogStep(1, "set up", "success", 1, 0),
			newStatusCheckLogStep(2, "run tests", "failure", 1, 1),
		)}, map[string][]string{"check-1": lines})

		frame := harness.readFrame()
		assert.Contains(t, frame, "▸ set up")
		assert.Contains(t, frame, "▸ run tests")
	})

	t.Run("the hovered header stays in view as a log above it is read", func(t *testing.T) {
		t.Parallel()

		build, test := numberedLines("build", 6000), numberedLines("test", 10)
		harness := openStatusChecks(t, func(harness *panelHarness) {
			answerStatusChecks(
				newLoggedStatusCheck("1", "build", "FAILURE", build),
				newLoggedStatusCheck("2", "test", "FAILURE", test, newStatusCheckLogStep(1, "run tests", "failure", 1, 10)),
			)(harness)
			answerStatusCheckLogs(map[string][]string{"check-1": build, "check-2": test})(harness)
		})
		harness.hold(pullrequestv1connect.PullRequestServiceReadCheckLogProcedure)
		harness.press("enter")
		harness.release(pullrequestv1connect.PullRequestServiceReadCheckLogProcedure)
		harness.release(pullrequestv1connect.PullRequestServiceReadCheckLogProcedure)
		harness.press("j", "j")
		harness.release(pullrequestv1connect.PullRequestServiceReadCheckLogProcedure)

		frame := harness.readFrame()
		assert.Contains(t, frame, "▸ run tests")
		assert.Contains(t, frame, "test 1")
	})

	t.Run("searching the logs lands on what it found", func(t *testing.T) {
		t.Parallel()

		lines := append(numberedLines("line", 200), "panic: nil map")
		harness := openStatusCheckLogs(t, []*pullrequestv1.StatusCheck{newLoggedStatusCheck("1", "test", "FAILURE", lines)}, map[string][]string{"check-1": lines})
		harness.press("pick:block", "/", "p", "a", "n", "i", "c", "enter")

		frame := harness.readFrame()
		assert.Contains(t, frame, "panic: nil map")
		assert.Contains(t, frame, "1 of 1")
	})

	t.Run("a refused read says why", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, func(harness *panelHarness) {
			answerStatusChecks(newLoggedStatusCheck("1", "test", "FAILURE", []string{"ok"}))(harness)
			harness.refuse(pullrequestv1connect.PullRequestServiceReadCheckLogProcedure, connect.NewError(connect.CodeUnavailable, nil))
		})
		harness.press("enter")

		assert.Contains(t, harness.readFrame(), "could not read")
	})
}

// TestStatusCheckLogsNotices covers what is drawn under a check whose log cannot be read.
func TestStatusCheckLogsNotices(t *testing.T) {
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

			check := newLoggedStatusCheck("1", "test", "FAILURE", nil)
			check.Log.State = tt.state
			harness := openStatusCheckLogs(t, []*pullrequestv1.StatusCheck{check}, nil)

			assert.Contains(t, harness.readFrame(), tt.want)
			assert.Empty(t, harness.readSent(pullrequestv1connect.PullRequestServiceReadCheckLogProcedure))
		})
	}
}
