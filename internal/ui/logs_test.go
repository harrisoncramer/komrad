package ui_test

import (
	"errors"
	"slices"
	"testing"

	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	logv1 "komrad/proto/gen/log/v1"
	"komrad/proto/gen/log/v1/logv1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"
)

const logsScreen = "the logs"

const provisionRun = "001-provision-setup.log"

// logReads answers each read of an agent's logs by the source and cursor it was sent with.
type logReads map[string]*logv1.ReadAgentLogsResponse

func (r logReads) answer(harness *panelHarness) {
	harness.answer(logv1connect.LogServiceListAgentLogSourcesProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&logv1.ListAgentLogSourcesResponse{Sources: []*logv1.LogSource{
			{Id: "service", Name: "service"},
			{Id: provisionRun, Name: "001 provision setup", Run: xptr.Ptr[int32](1)},
		}}), nil
	})
	harness.answer(logv1connect.LogServiceReadAgentLogsProcedure, func(req connect.AnyRequest) (connect.AnyResponse, error) {
		read, _ := req.Any().(*logv1.ReadAgentLogsRequest)
		if resp, ok := r[read.GetSource()+"@"+read.GetCursor()]; ok {
			return connect.NewResponse(resp), nil
		}

		return connect.NewResponse(&logv1.ReadAgentLogsResponse{Cursor: read.GetCursor()}), nil
	})
}

// tailOfTwo is a log holding two lines about the agent, a third written after the first read, and
// a provision run that printed one line.
func tailOfTwo() logReads {
	return logReads{
		"service@":         {Lines: []string{`{"body":"first line"}`, `{"body":"second line"}`}, Cursor: "10"},
		"service@10":       {Lines: []string{`{"body":"third line"}`}, Cursor: "20"},
		provisionRun + "@": {Lines: []string{"error: port 5470 already allocated"}, Cursor: "35"},
	}
}

// openLogs opens the logs of the author from the subpages menu, over a log answering reads.
func openLogs(t *testing.T, reads logReads, rows ...proto.Message) *panelHarness {
	t.Helper()

	if len(rows) == 0 {
		rows = buildSubpagesAgents()
	}
	harness := openAgents(t, rows...)
	reads.answer(harness)
	moveToAgentRow(t, harness, "author")
	harness.press("space", "l")

	return harness
}

func logRead(source, cursor string) *logv1.ReadAgentLogsRequest {
	return &logv1.ReadAgentLogsRequest{AgentIds: []string{"author"}, Source: source, Cursor: cursor, Limit: 500}
}

// TestLogsOpen covers the screen opening from the subpages menu on the agent's log sources.
func TestLogsOpen(t *testing.T) {
	t.Parallel()

	t.Run("the menu lists the sources and opens the newest lines of the service log", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())

		frame := harness.readFrame()
		assert.Equal(t, logsScreen, harness.readScreen())
		assert.True(t, harness.panel.IsZoomed(), "the screen holds the whole window")
		assert.Contains(t, frame, "agents › author › logs")
		assert.Contains(t, readHoveredRow(frame), "service")
		assert.Contains(t, readSidebarProse(frame), "001 provision setup")
		assert.Contains(t, readStageText(frame), `{"body":"first line"}`)
		assert.Contains(t, readStageText(frame), `{"body":"second line"}`)
		harness.assertSentTo(logv1connect.LogServiceListAgentLogSourcesProcedure, &logv1.ListAgentLogSourcesRequest{AgentId: "author"})
		harness.assertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead("service", ""))
	})

	t.Run("an agent with a failed run opens on it", func(t *testing.T) {
		t.Parallel()

		rows := buildSubpagesAgents()
		author, _ := rows[2].(*agentv1.Agent)
		author.FailedRunLog = xptr.Ptr[int32](1)
		harness := openLogs(t, tailOfTwo(), rows...)

		frame := harness.readFrame()
		assert.Contains(t, readHoveredRow(frame), "001 provision setup")
		assert.Contains(t, readSidebarProse(frame), "failed")
		assert.Contains(t, readStageText(frame), "error: port 5470 already allocated")
		harness.assertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead(provisionRun, ""))
	})

	t.Run("a log with nothing about the agent says so", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, logReads{})

		assert.Contains(t, readStageText(harness.readFrame()), "Nothing about this agent in the last 64 MB of the log.")
	})

	t.Run("a run that printed nothing says so", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, logReads{})
		harness.press("j")

		assert.Contains(t, readStageText(harness.readFrame()), "This run wrote nothing.")
	})

	t.Run("back returns to the list on the agent it opened for", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())
		harness.press("esc")

		assert.Equal(t, agentsScreen, harness.readScreen())
		assert.Contains(t, readHoveredRow(harness.readFrame()), "author")
	})
}

// TestLogsSources covers moving between sources, each drawn on its own from its newest lines.
func TestLogsSources(t *testing.T) {
	t.Parallel()

	t.Run("moving to another source draws its lines alone", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())
		harness.press("j")

		stage := readStageText(harness.readFrame())
		assert.Contains(t, readHoveredRow(harness.readFrame()), "001 provision setup")
		assert.Contains(t, stage, "error: port 5470 already allocated")
		assert.NotContains(t, stage, "first line")
		harness.assertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead("service", ""), logRead(provisionRun, ""))
	})

	t.Run("moving back reads the source from its newest again", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())
		harness.press("j", "k")

		assert.Contains(t, readStageText(harness.readFrame()), "second line")
		harness.assertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead("service", ""), logRead(provisionRun, ""), logRead("service", ""))
	})

	t.Run("a read of the source moved off is dropped", func(t *testing.T) {
		t.Parallel()

		read := logv1connect.LogServiceReadAgentLogsProcedure
		harness := openAgents(t, buildSubpagesAgents()...)
		tailOfTwo().answer(harness)
		moveToAgentRow(t, harness, "author")
		harness.press("space", "l")
		harness.hold(read)
		harness.fireTimers()
		harness.press("j")

		harness.release(read)

		assert.NotContains(t, readStageText(harness.readFrame()), "third line")
	})
}

// TestLogsTail covers the reads after the first, each from where the last left off.
func TestLogsTail(t *testing.T) {
	t.Parallel()

	t.Run("the next read carries the cursor and appends what is new", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())
		harness.fireTimers()

		harness.assertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead("service", ""), logRead("service", "10"))
		assert.Contains(t, readStageText(harness.readFrame()), `{"body":"third line"}`)
	})

	t.Run("a log that started over clears what was held", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, logReads{
			"service@":   {Lines: []string{`{"body":"first line"}`}, Cursor: "10"},
			"service@10": {Lines: []string{`{"body":"fresh line"}`}, Cursor: "4", Reset_: true},
		})
		harness.fireTimers()

		stage := readStageText(harness.readFrame())
		assert.Contains(t, stage, "fresh line")
		assert.NotContains(t, stage, "first line")
	})

	t.Run("a read with more behind it reads again at once", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, logReads{
			"service@":   {Lines: []string{`{"body":"first line"}`}, Cursor: "10", More: true},
			"service@10": {Lines: []string{`{"body":"second line"}`}, Cursor: "20"},
		})

		harness.assertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead("service", ""), logRead("service", "10"))
		assert.Contains(t, readStageText(harness.readFrame()), `{"body":"second line"}`)
	})

	t.Run("a read answered after the screen was left and opened again is dropped", func(t *testing.T) {
		t.Parallel()

		read := logv1connect.LogServiceReadAgentLogsProcedure
		harness := openAgents(t, buildSubpagesAgents()...)
		logReads{}.answer(harness)
		harness.answer(read, func(req connect.AnyRequest) (connect.AnyResponse, error) {
			sent, _ := req.Any().(*logv1.ReadAgentLogsRequest)
			if slices.Contains(sent.GetAgentIds(), "author") {
				return connect.NewResponse(&logv1.ReadAgentLogsResponse{Lines: []string{`{"body":"first line"}`}, Cursor: "10", More: true}), nil
			}

			return connect.NewResponse(&logv1.ReadAgentLogsResponse{Cursor: "30"}), nil
		})
		harness.hold(read)
		moveToAgentRow(t, harness, "author")
		harness.press("space", "l", "esc")
		moveToAgentRow(t, harness, "quiet")
		harness.press("space", "l")

		harness.release(read)
		harness.release(read)

		quiet := &logv1.ReadAgentLogsRequest{AgentIds: []string{"quiet"}, Source: "service", Limit: 500}
		assert.NotContains(t, readStageText(harness.readFrame()), "first line")
		harness.assertSentTo(read, logRead("service", ""), quiet)
	})

	t.Run("leaving the screen ends the reads", func(t *testing.T) {
		t.Parallel()

		harness := openLogs(t, tailOfTwo())
		harness.press("esc")
		harness.fireTimers()

		harness.assertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead("service", ""))
	})

	t.Run("a failed read says why and is tried again", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, buildSubpagesAgents()...)
		logReads{}.answer(harness)
		harness.refuse(logv1connect.LogServiceReadAgentLogsProcedure, connect.NewError(connect.CodeUnavailable, errors.New("the log is gone")))
		moveToAgentRow(t, harness, "author")
		harness.press("space", "l")
		harness.fireTimers()

		harness.assertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead("service", ""), logRead("service", ""))
		assert.Contains(t, readStageText(harness.readFrame()), "could not read: the log is gone")
	})

	t.Run("a failed listing says why and is tried again", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, buildSubpagesAgents()...)
		harness.refuse(logv1connect.LogServiceListAgentLogSourcesProcedure, connect.NewError(connect.CodeUnavailable, errors.New("the engine is gone")))
		moveToAgentRow(t, harness, "author")
		harness.press("space", "l")
		harness.fireTimers()

		assert.Len(t, harness.readSent(logv1connect.LogServiceListAgentLogSourcesProcedure), 2)
		assert.Contains(t, readSidebarProse(harness.readFrame()), "could not list: the engine is gone")
	})
}
