package log

import (
	"testing"

	"weave/internal/logread"
	logv1 "weave/proto/gen/log/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeLog struct {
	lines     []logread.Line
	summaries []logread.Summary
	err       error
	limit     int
}

func (f *fakeLog) Lines(string) ([]logread.Line, error) { return f.lines, f.err }

func (f *fakeLog) RecentTraces(limit int) ([]logread.Summary, error) {
	f.limit = limit

	return f.summaries, f.err
}

// TestGetLogLines covers a line's own fields riding along and the log's bookkeeping keys, which
// the line already carries as its time, level and message, left out of them.
func TestGetLogLines(t *testing.T) {
	t.Parallel()

	service := NewService(&fakeLog{lines: []logread.Line{{
		Time:    "2026-08-06T22:47:32.293Z",
		Level:   "ERROR",
		Message: "rpc failed",
		Trace:   "trace-one",
		Fields: map[string]any{
			"timestamp": "2026-08-06T22:47:32.293Z", "severity_text": "ERROR", "body": "rpc failed",
			"trace.id": "trace-one", "rpc.method": "ArchiveAgents", "error.message": "pane already gone",
			"unspeakable": func() {},
		},
	}}})

	resp, failure := service.GetLogLines(t.Context(), &logv1.GetLogLinesRequest{TraceId: "trace-one"})
	require.Nil(t, failure)
	require.Len(t, resp.GetLines(), 1)

	line := resp.GetLines()[0]
	assert.Equal(t, "rpc failed", line.GetMessage())
	assert.Equal(t, "ERROR", line.GetLevel())
	fields := line.GetFields().AsMap()
	assert.Equal(t, "pane already gone", fields["error.message"])
	assert.Equal(t, "ArchiveAgents", fields["rpc.method"])
	assert.NotContains(t, fields, "trace.id")
	assert.NotContains(t, fields, "body")
	assert.NotContains(t, fields, "unspeakable", "a value a Struct cannot hold is left out rather than failing the call")
}

// TestGetLogLinesReportsWhatTheReaderSaid covers the reader's refusals reaching the caller with
// their kind.
func TestGetLogLinesReportsWhatTheReaderSaid(t *testing.T) {
	t.Parallel()

	service := NewService(&fakeLog{err: logread.ErrNoSuchTrace.Saying("nothing under trace-nine")})

	_, failure := service.GetLogLines(t.Context(), &logv1.GetLogLinesRequest{TraceId: "trace-nine"})
	require.NotNil(t, failure)
	assert.ErrorIs(t, failure, logread.ErrNoSuchTrace)
}

// TestListRecentTraces covers the summaries mapped through and the limit handed on.
func TestListRecentTraces(t *testing.T) {
	t.Parallel()

	log := &fakeLog{summaries: []logread.Summary{{
		Trace:     "trace-one",
		StartedAt: "t",
		What:      "ArchiveAgents",
		Lines:     2,
	}}}
	service := NewService(log)

	resp, failure := service.ListRecentTraces(t.Context(), &logv1.ListRecentTracesRequest{Limit: 5})
	require.Nil(t, failure)
	assert.Equal(t, 5, log.limit)
	require.Len(t, resp.GetTraces(), 1)
	assert.Equal(t, "trace-one", resp.GetTraces()[0].GetTraceId())
	assert.Equal(t, int32(2), resp.GetTraces()[0].GetLines())
}
