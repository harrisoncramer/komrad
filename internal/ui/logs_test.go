package ui_test

import (
	"errors"
	"testing"

	logv1 "komrad/proto/gen/log/v1"
	"komrad/proto/gen/log/v1/logv1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
)

const logsScreen = "the logs"

// logReads answers each read of an agent's logs by the cursor it was sent with.
type logReads map[string]*logv1.ReadAgentLogsResponse

func (r logReads) answer(harness *panelHarness) {
	harness.answer(logv1connect.LogServiceReadAgentLogsProcedure, func(req connect.AnyRequest) (connect.AnyResponse, error) {
		read, _ := req.Any().(*logv1.ReadAgentLogsRequest)
		if resp, ok := r[read.GetCursor()]; ok {
			return connect.NewResponse(resp), nil
		}

		return connect.NewResponse(&logv1.ReadAgentLogsResponse{Cursor: read.GetCursor()}), nil
	})
}

// tailOfTwo is a log holding two lines about the agent, and a third written after the first read.
func tailOfTwo() logReads {
	return logReads{
		"":   {Lines: []string{`{"body":"first line"}`, `{"body":"second line"}`}, Cursor: "10"},
		"10": {Lines: []string{`{"body":"third line"}`}, Cursor: "20"},
	}
}

// openLogs opens the logs of the author from the subpages menu, over a log answering reads.
func openLogs(t *testing.T, reads logReads) *panelHarness {
	t.Helper()

	harness := openAgents(t, buildSubpagesAgents()...)
	reads.answer(harness)
	moveToAgentRow(t, harness, "author")
	harness.press("space", "l")

	return harness
}

func logRead(cursor string) *logv1.ReadAgentLogsRequest {
	return &logv1.ReadAgentLogsRequest{AgentIds: []string{"author"}, Cursor: cursor, Limit: 500}
}

// TestLogsOpen covers the screen opening from the subpages menu on the newest lines about the agent.
func TestLogsOpen(t *testing.T) {
	t.Parallel()

	t.Run("the menu opens the newest lines about the agent", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())

		frame := harness.readFrame()
		assert.Equal(t, logsScreen, harness.readScreen())
		assert.True(t, harness.panel.IsZoomed(), "the screen holds the whole window")
		assert.Contains(t, frame, "agents › author › logs")
		assert.Contains(t, readStageText(frame), `{"body":"first line"}`)
		assert.Contains(t, readStageText(frame), `›{"body":"second line"}`)
		assert.Contains(t, readSidebarProse(frame), "following 2 lines")
		harness.assertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead(""))
	})

	t.Run("a log with nothing about the agent says so", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, logReads{})

		assert.Contains(t, readStageText(harness.readFrame()), "Nothing about this agent in the last 64 MB of the log.")
	})

	t.Run("back returns to the list on the agent it opened for", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())
		harness.press("esc")

		assert.Equal(t, agentsScreen, harness.readScreen())
		assert.Contains(t, readHoveredRow(harness.readFrame()), "author")
	})
}

// TestLogsTail covers the reads after the first, each from where the last left off.
func TestLogsTail(t *testing.T) {
	t.Parallel()

	t.Run("the next read carries the cursor and appends what is new", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())
		harness.fireTimers()

		harness.assertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead(""), logRead("10"))
		assert.Contains(t, readStageText(harness.readFrame()), `›{"body":"third line"}`)
		assert.Contains(t, readSidebarProse(harness.readFrame()), "3 lines")
	})

	t.Run("a log that started over clears what was held", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, logReads{
			"":   {Lines: []string{`{"body":"first line"}`}, Cursor: "10"},
			"10": {Lines: []string{`{"body":"fresh line"}`}, Cursor: "4", Reset_: true},
		})
		harness.fireTimers()

		stage := readStageText(harness.readFrame())
		assert.Contains(t, stage, "fresh line")
		assert.NotContains(t, stage, "first line")
	})

	t.Run("leaving the screen ends the reads", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())
		harness.press("esc")
		harness.fireTimers()

		harness.assertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead(""))
	})

	t.Run("a failed read says why and is tried again", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, buildSubpagesAgents()...)
		harness.refuse(logv1connect.LogServiceReadAgentLogsProcedure, connect.NewError(connect.CodeUnavailable, errors.New("the log is gone")))
		moveToAgentRow(t, harness, "author")
		harness.press("space", "l")
		harness.fireTimers()

		harness.assertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead(""), logRead(""))
		assert.Contains(t, readSidebarProse(harness.readFrame()), "could not read: the log is gone")
		assert.Contains(t, readStageText(harness.readFrame()), "could not read: the log is gone")
	})
}

// TestLogsFollow covers moving through the lines, which stops following the newest until the
// bottom key.
func TestLogsFollow(t *testing.T) {
	t.Parallel()

	harness := openLogs(t, tailOfTwo())
	harness.press("k")

	assert.Contains(t, readStageText(harness.readFrame()), `›{"body":"first line"}`)
	assert.Contains(t, readSidebarProse(harness.readFrame()), "paused")

	harness.fireTimers()
	assert.Contains(t, readStageText(harness.readFrame()), `›{"body":"first line"}`, "a new line leaves the cursor where it was")

	harness.press("G")
	assert.Contains(t, readStageText(harness.readFrame()), `›{"body":"third line"}`)
	assert.Contains(t, readSidebarProse(harness.readFrame()), "following")

	harness.press("g", "g")
	assert.Contains(t, readStageText(harness.readFrame()), `›{"body":"first line"}`)
}
