package checks_test

import (
	"fmt"
	"testing"
	"time"

	"komrad/internal/ui/paneltest"
	"komrad/internal/ui/screen"
	"komrad/lib/xslice"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	"komrad/proto/gen/pullrequest/v1/pullrequestv1connect"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
)

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

		assert.Equal(t, screen.StatusCheckLogs, paneltest.Screen(harness.Frame()))
		assert.Contains(t, harness.Frame(), "status checks › logs")
	})

	t.Run("it reads the log of each check the scope shows", func(t *testing.T) {
		t.Parallel()

		harness := openStatusCheckLogs(t, checks, logs)

		harness.AssertSentTo(pullrequestv1connect.PullRequestServiceReadCheckLogProcedure, &pullrequestv1.ReadCheckLogRequest{
			CheckId:   "check-1",
			FirstLine: 1,
			Count:     2,
		})
	})

	t.Run("each check is a row with its steps under it", func(t *testing.T) {
		t.Parallel()

		harness := openStatusCheckLogs(t, checks, logs)

		assert.Equal(t, []string{"vet", "  └ run vet"}, paneltest.ListRows(harness.Frame()))
	})

	t.Run("each check is rendered under its header with its steps marked", func(t *testing.T) {
		t.Parallel()

		harness := openStatusCheckLogs(t, checks, logs)

		frame := harness.Frame()
		assert.Contains(t, frame, "═ vet ═")
		assert.Contains(t, frame, "▸ run vet")
		assert.Contains(t, frame, "##[error]vet: unreachable code")
		assert.NotContains(t, frame, "═ lint ═")
	})

	t.Run("widening the scope joins the passing check's log", func(t *testing.T) {
		t.Parallel()

		harness := openStatusCheckLogs(t, checks, logs)
		harness.Press(everyStatusCheck...)

		frame := harness.Frame()
		assert.Contains(t, frame, "═ lint ═")
		assert.Contains(t, frame, "lint ok")
		harness.AssertSentTo(pullrequestv1connect.PullRequestServiceReadCheckLogProcedure,
			&pullrequestv1.ReadCheckLogRequest{
				CheckId:   "check-1",
				FirstLine: 1,
				Count:     2,
			},
			&pullrequestv1.ReadCheckLogRequest{
				CheckId:   "check-2",
				FirstLine: 1,
				Count:     1,
			},
		)
	})

	t.Run("going back returns to the status checks", func(t *testing.T) {
		t.Parallel()

		harness := openStatusCheckLogs(t, checks, logs)
		harness.Press("esc")

		assert.Equal(t, screen.StatusChecks, paneltest.Screen(harness.Frame()))
	})
}

// TestStatusCheckLogsReading covers a check's log read in chunks and the document moving to the row hovered.
func TestStatusCheckLogsReading(t *testing.T) {
	t.Parallel()

	t.Run("a long log is read a chunk at a time", func(t *testing.T) {
		t.Parallel()

		lines := numberedLines("line", 7000)
		harness := openStatusCheckLogs(t, []*pullrequestv1.StatusCheck{newLoggedStatusCheck("1", "test", "FAILURE", lines)}, map[string][]string{"check-1": lines})

		harness.AssertSentTo(pullrequestv1connect.PullRequestServiceReadCheckLogProcedure,
			&pullrequestv1.ReadCheckLogRequest{
				CheckId:   "check-1",
				FirstLine: 1,
				Count:     5000,
			},
			&pullrequestv1.ReadCheckLogRequest{
				CheckId:   "check-1",
				FirstLine: 5001,
				Count:     2000,
			},
		)
	})

	t.Run("hovering a step brings its header into view", func(t *testing.T) {
		t.Parallel()

		lines := numberedLines("line", 300)
		harness := openStatusCheckLogs(t, []*pullrequestv1.StatusCheck{newLoggedStatusCheck("1", "test", "FAILURE", lines,
			newStatusCheckLogStep(1, "set up", "success", 1, 249),
			newStatusCheckLogStep(2, "run tests", "failure", 250, 51),
		)}, map[string][]string{"check-1": lines})

		assert.NotContains(t, harness.Frame(), "▸ run tests")

		harness.Press("j", "j")

		frame := harness.Frame()
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

		assert.Contains(t, harness.Frame(), "▸ post job")
	})

	t.Run("steps that start on the same line each keep a header", func(t *testing.T) {
		t.Parallel()

		lines := []string{"go test ./..."}
		harness := openStatusCheckLogs(t, []*pullrequestv1.StatusCheck{newLoggedStatusCheck("1", "test", "FAILURE", lines,
			newStatusCheckLogStep(1, "set up", "success", 1, 0),
			newStatusCheckLogStep(2, "run tests", "failure", 1, 1),
		)}, map[string][]string{"check-1": lines})

		frame := harness.Frame()
		assert.Contains(t, frame, "▸ set up")
		assert.Contains(t, frame, "▸ run tests")
	})

	t.Run("the hovered header stays in view as a log above it is read", func(t *testing.T) {
		t.Parallel()

		harness := hoverTestStepWhileBuildReads(t)
		harness.Release(pullrequestv1connect.PullRequestServiceReadCheckLogProcedure)

		frame := harness.Frame()
		assert.Contains(t, frame, "▸ run tests")
		assert.Contains(t, frame, "test 1")
	})

	t.Run("a scroll off the hovered header stays put as a log above it is read", func(t *testing.T) {
		t.Parallel()

		harness := hoverTestStepWhileBuildReads(t)
		harness.Send(tea.MouseWheelMsg{
			X:      paneltest.DefaultWidth - 1,
			Y:      5,
			Button: tea.MouseWheelUp,
		})
		harness.Release(pullrequestv1connect.PullRequestServiceReadCheckLogProcedure)

		frame := harness.Frame()
		assert.NotContains(t, frame, "▸ run tests")
		assert.Contains(t, frame, "build 49")
	})

	t.Run("searching the logs lands on what it found", func(t *testing.T) {
		t.Parallel()

		lines := append(numberedLines("line", 200), "panic: nil map")
		harness := openStatusCheckLogs(t, []*pullrequestv1.StatusCheck{newLoggedStatusCheck("1", "test", "FAILURE", lines)}, map[string][]string{"check-1": lines})
		harness.Press("pick:block", "/", "p", "a", "n", "i", "c", "enter")

		frame := harness.Frame()
		assert.Contains(t, frame, "panic: nil map")
		assert.Contains(t, frame, "1 of 1")
	})

	t.Run("searching the logs waits for the typing to stop", func(t *testing.T) {
		t.Parallel()

		lines := append(numberedLines("line", 200), "panic: nil map")
		harness := openStatusCheckLogs(t, []*pullrequestv1.StatusCheck{newLoggedStatusCheck("1", "test", "FAILURE", lines)}, map[string][]string{"check-1": lines})
		harness.Press("pick:block", "/", "p", "a", "n", "i", "c")

		assert.NotContains(t, harness.Frame(), "panic: nil map")

		harness.Advance(time.Second)

		frame := harness.Frame()
		assert.Contains(t, frame, "panic: nil map")
		assert.Contains(t, frame, "1 of 1")
	})

	t.Run("searching the logs lands at once when the search is entered", func(t *testing.T) {
		t.Parallel()

		lines := append(numberedLines("line", 200), "panic: nil map")
		harness := openStatusCheckLogs(t, []*pullrequestv1.StatusCheck{newLoggedStatusCheck("1", "test", "FAILURE", lines)}, map[string][]string{"check-1": lines})
		harness.Press("pick:block", "/", "p", "a", "n", "i", "c", "enter")

		frame := harness.Frame()
		assert.Contains(t, frame, "panic: nil map")
		assert.Contains(t, frame, "1 of 1")

		harness.Advance(time.Second)

		assert.Equal(t, frame, harness.Frame())
	})

	t.Run("searching the logs for fewer than three characters finds nothing", func(t *testing.T) {
		t.Parallel()

		lines := append(numberedLines("line", 200), "panic: nil map")
		harness := openStatusCheckLogs(t, []*pullrequestv1.StatusCheck{newLoggedStatusCheck("1", "test", "FAILURE", lines)}, map[string][]string{"check-1": lines})
		harness.Press("pick:block", "/", "p", "a")
		harness.Advance(time.Second)
		harness.Press("enter", "n")

		frame := harness.Frame()
		assert.NotContains(t, frame, "panic: nil map")
		assert.NotContains(t, frame, "0 matches")
		assert.Contains(t, frame, "nothing to search for yet")
	})

	t.Run("a refused read says why", func(t *testing.T) {
		t.Parallel()

		harness := openStatusChecks(t, func(harness *paneltest.Panel) {
			answerStatusChecks(newLoggedStatusCheck("1", "test", "FAILURE", []string{"ok"}))(harness)
			harness.Refuse(pullrequestv1connect.PullRequestServiceReadCheckLogProcedure, connect.NewError(connect.CodeUnavailable, nil))
		})
		harness.Press("enter")

		assert.Contains(t, harness.Frame(), "could not read")
	})
}

// TestStatusCheckLogsNotices covers what is rendered under a check whose log cannot be read.
func TestStatusCheckLogsNotices(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		state pullrequestv1.CheckLogState
		want  string
	}{
		{
			name:  "a log not fetched yet",
			state: pullrequestv1.CheckLogState_CHECK_LOG_STATE_PENDING,
			want:  "log not fetched yet",
		},
		{
			name:  "a log too large to keep",
			state: pullrequestv1.CheckLogState_CHECK_LOG_STATE_TOO_LARGE,
			want:  "log too large to keep",
		},
		{
			name:  "a log the forge does not have",
			state: pullrequestv1.CheckLogState_CHECK_LOG_STATE_MISSING,
			want:  "the forge has no log for this check",
		},
		{
			name:  "a log expired on the forge",
			state: pullrequestv1.CheckLogState_CHECK_LOG_STATE_EXPIRED,
			want:  "log expired on the forge",
		},
		{
			name:  "a log that could not be fetched",
			state: pullrequestv1.CheckLogState_CHECK_LOG_STATE_FAILED,
			want:  "log could not be fetched",
		},
		{
			name:  "a fetched log with no lines",
			state: pullrequestv1.CheckLogState_CHECK_LOG_STATE_FETCHED,
			want:  "empty log",
		},
		{
			name:  "a check with no log",
			state: pullrequestv1.CheckLogState_CHECK_LOG_STATE_NONE,
			want:  "no log",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			check := newLoggedStatusCheck("1", "test", "FAILURE", nil)
			check.Log.State = tt.state
			harness := openStatusCheckLogs(t, []*pullrequestv1.StatusCheck{check}, nil)

			assert.Contains(t, harness.Frame(), tt.want)
			assert.Empty(t, harness.Sent(pullrequestv1connect.PullRequestServiceReadCheckLogProcedure))
		})
	}
}

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
		Passed:     fixturePassed(pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED, conclusion),
		FirstLine:  firstLine,
		LineCount:  lineCount,
	}
}

func numberedLines(prefix string, count int) []string {
	lines := make([]string, count)
	for i := range lines {
		lines[i] = fmt.Sprintf("%s %d", prefix, i+1)
	}

	return lines
}

// answerStatusCheckLogs answers each read of a check's log with the lines it asks for.
func answerStatusCheckLogs(logs map[string][]string) func(*paneltest.Panel) {
	return func(harness *paneltest.Panel) {
		harness.Answer(pullrequestv1connect.PullRequestServiceReadCheckLogProcedure, func(req connect.AnyRequest) (connect.AnyResponse, error) {
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

func openStatusCheckLogs(t *testing.T, checks []*pullrequestv1.StatusCheck, logs map[string][]string) *paneltest.Panel {
	t.Helper()

	harness := openStatusChecks(t, func(harness *paneltest.Panel) {
		answerStatusChecks(checks...)(harness)
		answerStatusCheckLogs(logs)(harness)
	})
	harness.Press("enter")

	return harness
}

func hoverTestStepWhileBuildReads(t *testing.T) *paneltest.Panel {
	t.Helper()

	build, test := numberedLines("build", 6000), numberedLines("test", 10)
	harness := openStatusChecks(t, func(harness *paneltest.Panel) {
		answerStatusChecks(
			newLoggedStatusCheck("1", "build", "FAILURE", build),
			newLoggedStatusCheck("2", "test", "FAILURE", test, newStatusCheckLogStep(1, "run tests", "failure", 1, 10)),
		)(harness)
		answerStatusCheckLogs(map[string][]string{"check-1": build, "check-2": test})(harness)
	})
	harness.Hold(pullrequestv1connect.PullRequestServiceReadCheckLogProcedure)
	harness.Press("enter")
	harness.Release(pullrequestv1connect.PullRequestServiceReadCheckLogProcedure)
	harness.Release(pullrequestv1connect.PullRequestServiceReadCheckLogProcedure)
	harness.Press("j", "j")

	return harness
}
