package log

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"komrad/config"
	"komrad/internal/logread"
	"komrad/lib/xptr"
	logv1 "komrad/proto/gen/log/v1"
	"komrad/proto/gen/log/v1/logv1komrad"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/structpb"
)

type fakeLogReader struct {
	lines     []logread.Line
	summaries []logread.Summary
	window    *logread.Window
	runs      []config.RunLog
	err       error
	traces    []string
	limits    []int
	reads     []agentRead
}

// agentRead is one read of agent logs as the fake saw it.
type agentRead struct {
	ids     []string
	source  string
	pattern string
	offset  int64
	limit   int
}

// patternOf is the pattern as the read was handed it, empty for none.
func patternOf(pattern *regexp.Regexp) string {
	if pattern == nil {
		return ""
	}

	return pattern.String()
}

// List answers runs.
func (f *fakeLogReader) List(string) []config.RunLog {
	return f.runs
}

// Newest records the read of the run and answers window.
func (f *fakeLogReader) Newest(ctx context.Context, run logread.Run, query logread.Query) (*logread.Window, error) {
	return f.Since(ctx, run, -1, query)
}

// Since records the read of the run and answers window.
func (f *fakeLogReader) Since(_ context.Context, run logread.Run, offset int64, query logread.Query) (*logread.Window, error) {
	f.reads = append(f.reads, agentRead{
		ids:     []string{run.AgentID},
		source:  run.Name,
		pattern: patternOf(query.Pattern),
		offset:  offset,
		limit:   query.Limit,
	})

	if f.err != nil {
		return nil, f.err
	}

	return f.window, nil
}

// NewestAboutAgents records the read and answers window.
func (f *fakeLogReader) NewestAboutAgents(_ context.Context, agentIDs []string, query logread.Query) (*logread.Window, error) {
	f.reads = append(f.reads, agentRead{
		ids:     agentIDs,
		pattern: patternOf(query.Pattern),
		offset:  -1,
		limit:   query.Limit,
	})

	if f.err != nil {
		return nil, f.err
	}

	return f.window, nil
}

// AboutAgentsSince records the read and answers window.
func (f *fakeLogReader) AboutAgentsSince(_ context.Context, agentIDs []string, offset int64, query logread.Query) (*logread.Window, error) {
	f.reads = append(f.reads, agentRead{
		ids:     agentIDs,
		pattern: patternOf(query.Pattern),
		offset:  offset,
		limit:   query.Limit,
	})

	if f.err != nil {
		return nil, f.err
	}

	return f.window, nil
}

// Lines records the trace and answers lines unless err refuses it.
func (f *fakeLogReader) Lines(trace string) ([]logread.Line, error) {
	f.traces = append(f.traces, trace)
	if f.err != nil {
		return nil, f.err
	}

	return f.lines, nil
}

// RecentTraces records the limit and answers summaries unless err refuses it.
func (f *fakeLogReader) RecentTraces(limit int) ([]logread.Summary, error) {
	f.limits = append(f.limits, limit)
	if f.err != nil {
		return nil, f.err
	}

	return f.summaries, nil
}

// TestLogRefusalsMapToCodes pins the code each refusal of the log reader reaches the caller as.
func TestLogRefusalsMapToCodes(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode connect.Code
	}{
		{
			name:     "a trace nothing was written under",
			err:      logread.ErrNoSuchTrace.Saying("nothing under trace-nine"),
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "a log that cannot be read",
			err:      errors.New("permission denied"),
			wantCode: connect.CodeInternal,
		},
		{
			name:     "a run the agent never recorded",
			err:      logread.ErrNoSuchRun.Saying("no run named 009-provision-setup.log"),
			wantCode: connect.CodeNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fakeLogReader{err: tt.err}
			service := logv1komrad.LogServiceAdapter{Impl: NewHandler(reader, reader)}

			_, err := service.GetLogLines(t.Context(), connect.NewRequest(&logv1.GetLogLinesRequest{TraceId: "trace-nine"}))
			assert.Equal(t, tt.wantCode, connect.CodeOf(err))

			_, err = service.ListRecentTraces(t.Context(), connect.NewRequest(&logv1.ListRecentTracesRequest{Limit: 5}))
			assert.Equal(t, tt.wantCode, connect.CodeOf(err))

			_, err = service.ReadAgentLogs(t.Context(), connect.NewRequest(&logv1.ReadAgentLogsRequest{AgentIds: []string{"a1"}, Source: "009-provision-setup.log"}))
			assert.Equal(t, tt.wantCode, connect.CodeOf(err))
		})
	}
}

// TestGetLogLines pins that a line's bookkeeping keys and unrenderable values leave its fields.
func TestGetLogLines(t *testing.T) {
	reader := &fakeLogReader{lines: []logread.Line{{
		Time:    "2026-08-06T22:47:32.293Z",
		Level:   "ERROR",
		Message: "rpc failed",
		Trace:   "trace-one",
		Fields: map[string]any{
			"timestamp":       "2026-08-06T22:47:32.293Z",
			"severity_text":   "ERROR",
			"severity_number": 17,
			"body":            "rpc failed",
			"trace.id":        "trace-one",
			"rpc.method":      "ArchiveAgents",
			"unspeakable":     func() {},
		},
	}}}
	service := logv1komrad.LogServiceAdapter{Impl: NewHandler(reader, reader)}

	resp, err := service.GetLogLines(t.Context(), connect.NewRequest(&logv1.GetLogLinesRequest{TraceId: "trace-one"}))
	require.NoError(t, err)

	fields, err := structpb.NewStruct(map[string]any{"rpc.method": "ArchiveAgents"})
	require.NoError(t, err)

	want := &logv1.GetLogLinesResponse{Lines: []*logv1.LogLine{{
		Time:    "2026-08-06T22:47:32.293Z",
		Level:   "ERROR",
		Message: "rpc failed",
		Fields:  fields,
	}}}
	assert.Equal(t, []string{"trace-one"}, reader.traces)
	assert.Empty(t, cmp.Diff(want, resp.Msg, protocmp.Transform()))
}

// TestListRecentTraces pins that the limit is handed on and each summary renders.
func TestListRecentTraces(t *testing.T) {
	reader := &fakeLogReader{summaries: []logread.Summary{{
		Trace:     "trace-one",
		StartedAt: "2026-08-06T22:47:32.293Z",
		What:      "ArchiveAgents",
		Lines:     2,
	}}}
	service := logv1komrad.LogServiceAdapter{Impl: NewHandler(reader, reader)}

	resp, err := service.ListRecentTraces(t.Context(), connect.NewRequest(&logv1.ListRecentTracesRequest{Limit: 5}))
	require.NoError(t, err)

	want := &logv1.ListRecentTracesResponse{Traces: []*logv1.TraceSummary{{
		TraceId:   "trace-one",
		StartedAt: "2026-08-06T22:47:32.293Z",
		What:      "ArchiveAgents",
		Lines:     2,
	}}}
	assert.Equal(t, []int{5}, reader.limits)
	assert.Empty(t, cmp.Diff(want, resp.Msg, protocmp.Transform()))
}

// TestReadAgentLogs pins how a cursor turns into a read and how the read turns back into a cursor.
func TestReadAgentLogs(t *testing.T) {
	tests := []struct {
		name     string
		req      *logv1.ReadAgentLogsRequest
		window   *logread.Window
		wantRead agentRead
		want     *logv1.ReadAgentLogsResponse
		wantCode connect.Code
	}{
		{
			name:     "a cursor this service never answered",
			req:      &logv1.ReadAgentLogsRequest{AgentIds: []string{"a1"}, Cursor: "later"},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "a cursor before the start of the log",
			req:      &logv1.ReadAgentLogsRequest{AgentIds: []string{"a1"}, Cursor: "-1"},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:   "no cursor reads the newest, two hundred by default",
			req:    &logv1.ReadAgentLogsRequest{AgentIds: []string{"a1", "b2"}},
			window: &logread.Window{Lines: []string{"one", "two"}, Offset: 42},
			wantRead: agentRead{
				ids:    []string{"a1", "b2"},
				offset: -1,
				limit:  200,
			},
			want: &logv1.ReadAgentLogsResponse{Lines: []string{"one", "two"}, Cursor: "42"},
		},
		{
			name: "a cursor reads from where it left off",
			req: &logv1.ReadAgentLogsRequest{
				AgentIds: []string{"a1"},
				Cursor:   "42",
				Limit:    5,
			},
			window: &logread.Window{
				Lines:  []string{"three"},
				Offset: 90,
				More:   true,
			},
			wantRead: agentRead{
				ids:    []string{"a1"},
				offset: 42,
				limit:  5,
			},
			want: &logv1.ReadAgentLogsResponse{
				Lines:  []string{"three"},
				Cursor: "90",
				More:   true,
			},
		},
		{
			name: "a log that started over says so",
			req:  &logv1.ReadAgentLogsRequest{AgentIds: []string{"a1"}, Cursor: "900"},
			window: &logread.Window{
				Lines:  []string{"one"},
				Offset: 10,
				Reset:  true,
			},
			wantRead: agentRead{
				ids:    []string{"a1"},
				offset: 900,
				limit:  200,
			},
			want: &logv1.ReadAgentLogsResponse{
				Lines:  []string{"one"},
				Cursor: "10",
				Reset_: true,
			},
		},
		{
			name:     "a run's source asked of two agents",
			req:      &logv1.ReadAgentLogsRequest{AgentIds: []string{"a1", "b2"}, Source: "001-provision-setup.log"},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:   "the service source reads the service log",
			req:    &logv1.ReadAgentLogsRequest{AgentIds: []string{"a1"}, Source: "service"},
			window: &logread.Window{Lines: []string{"one"}, Offset: 42},
			wantRead: agentRead{
				ids:    []string{"a1"},
				offset: -1,
				limit:  200,
			},
			want: &logv1.ReadAgentLogsResponse{Lines: []string{"one"}, Cursor: "42"},
		},
		{
			name:   "a run's source reads the newest of that run",
			req:    &logv1.ReadAgentLogsRequest{AgentIds: []string{"a1"}, Source: "001-provision-setup.log"},
			window: &logread.Window{Lines: []string{"+ mise trust"}, Offset: 13},
			wantRead: agentRead{
				ids:    []string{"a1"},
				source: "001-provision-setup.log",
				offset: -1,
				limit:  200,
			},
			want: &logv1.ReadAgentLogsResponse{Lines: []string{"+ mise trust"}, Cursor: "13"},
		},
		{
			name: "a run's source reads from its cursor",
			req: &logv1.ReadAgentLogsRequest{
				AgentIds: []string{"a1"},
				Source:   "001-provision-setup.log",
				Cursor:   "13",
			},
			window: &logread.Window{Lines: []string{"+ mise install"}, Offset: 28},
			wantRead: agentRead{
				ids:    []string{"a1"},
				source: "001-provision-setup.log",
				offset: 13,
				limit:  200,
			},
			want: &logv1.ReadAgentLogsResponse{Lines: []string{"+ mise install"}, Cursor: "28"},
		},
		{
			name:     "a pattern that does not compile",
			req:      &logv1.ReadAgentLogsRequest{AgentIds: []string{"a1"}, Pattern: "error("},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name: "a pattern narrows the service log from its cursor",
			req: &logv1.ReadAgentLogsRequest{
				AgentIds: []string{"a1"},
				Cursor:   "42",
				Pattern:  "(?i)error",
			},
			window: &logread.Window{Lines: []string{"ERROR one"}, Offset: 90},
			wantRead: agentRead{
				ids:     []string{"a1"},
				pattern: "(?i)error",
				offset:  42,
				limit:   200,
			},
			want: &logv1.ReadAgentLogsResponse{Lines: []string{"ERROR one"}, Cursor: "90"},
		},
		{
			name: "a pattern narrows a run's source",
			req: &logv1.ReadAgentLogsRequest{
				AgentIds: []string{"a1"},
				Source:   "001-provision-setup.log",
				Pattern:  "mise",
			},
			window: &logread.Window{Lines: []string{"+ mise trust"}, Offset: 13},
			wantRead: agentRead{
				ids:     []string{"a1"},
				source:  "001-provision-setup.log",
				pattern: "mise",
				offset:  -1,
				limit:   200,
			},
			want: &logv1.ReadAgentLogsResponse{Lines: []string{"+ mise trust"}, Cursor: "13"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fakeLogReader{window: tt.window}
			service := logv1komrad.LogServiceAdapter{Impl: NewHandler(reader, reader)}

			resp, err := service.ReadAgentLogs(t.Context(), connect.NewRequest(tt.req))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				assert.Empty(t, reader.reads)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, []agentRead{tt.wantRead}, reader.reads)
			assert.Empty(t, cmp.Diff(tt.want, resp.Msg, protocmp.Transform()))
		})
	}
}

// TestListAgentLogSources pins that the service log comes first and each run after it is named by
// its number, trigger and script.
func TestListAgentLogSources(t *testing.T) {
	reader := &fakeLogReader{runs: []config.RunLog{
		{
			Number:  2,
			Trigger: "pre-launch",
			Script:  "setup",
			Path:    "/runs/a1/002-pre-launch-setup.log",
		},
		{
			Number:  1,
			Trigger: "provision",
			Script:  "_worktree_setup",
			Path:    "/runs/a1/001-provision-_worktree_setup.log",
		},
	}}
	service := logv1komrad.LogServiceAdapter{Impl: NewHandler(reader, reader)}

	resp, err := service.ListAgentLogSources(t.Context(), connect.NewRequest(&logv1.ListAgentLogSourcesRequest{AgentId: "a1"}))
	require.NoError(t, err)

	want := &logv1.ListAgentLogSourcesResponse{Sources: []*logv1.LogSource{
		{Id: "service", Name: "service"},
		{
			Id:   "002-pre-launch-setup.log",
			Name: "002 pre-launch setup",
			Run:  xptr.Ptr[int32](2),
		},
		{
			Id:   "001-provision-_worktree_setup.log",
			Name: "001 provision _worktree_setup",
			Run:  xptr.Ptr[int32](1),
		},
	}}
	assert.Empty(t, cmp.Diff(want, resp.Msg, protocmp.Transform()))
}
