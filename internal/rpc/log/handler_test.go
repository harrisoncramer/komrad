package log

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"komrad/config"
	"komrad/internal/logread"
	runstore "komrad/internal/store/run"
	"komrad/lib/xptr"
	eventv1 "komrad/proto/gen/event/v1"
	logv1 "komrad/proto/gen/log/v1"
	"komrad/proto/gen/log/v1/logv1komrad"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	agentID      = uuid.MustParse("01a11e6f-0000-7000-8000-0000000000a1")
	otherAgentID = uuid.MustParse("01a11e6f-0000-7000-8000-0000000000a2")
	taskID       = "01a11e6f-0000-7000-8000-0000000000f1"
)

type fakeLogReader struct {
	lines     []logread.Line
	summaries []logread.Summary
	window    *logread.Window
	runs      []config.RunLog
	taskRuns  []runstore.Run
	err       error
	traces    []string
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
func (f *fakeLogReader) List(uuid.UUID) []config.RunLog {
	return f.runs
}

// ListRuns answers taskRuns unless err refuses it.
func (f *fakeLogReader) ListRuns(context.Context, uuid.UUID) ([]runstore.Run, error) {
	if f.err != nil {
		return nil, f.err
	}

	return f.taskRuns, nil
}

// Newest records the read of the run and answers window.
func (f *fakeLogReader) Newest(ctx context.Context, run logread.Run, query logread.Query) (*logread.Window, error) {
	return f.Since(ctx, run, -1, query)
}

// Since records the read of the run and answers window.
func (f *fakeLogReader) Since(_ context.Context, run logread.Run, offset int64, query logread.Query) (*logread.Window, error) {
	f.reads = append(f.reads, agentRead{
		ids:     []string{run.AgentID.String()},
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

// RecentTraces answers summaries unless err refuses it.
func (f *fakeLogReader) RecentTraces(int) ([]logread.Summary, error) {
	if f.err != nil {
		return nil, f.err
	}

	return f.summaries, nil
}

// TestGetLogLines pins that a line's bookkeeping keys and unrenderable values leave its fields.
func TestGetLogLines(t *testing.T) {
	reader := &fakeLogReader{lines: []logread.Line{{
		Time:    time.Date(2026, 8, 6, 22, 47, 32, 293_000_000, time.UTC),
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
	service := logv1komrad.LogServiceAdapter{Impl: NewHandler(reader, reader, reader)}

	resp, err := service.GetLogLines(t.Context(), connect.NewRequest(&logv1.GetLogLinesRequest{TraceId: "trace-one"}))
	require.NoError(t, err)

	fields, err := structpb.NewStruct(map[string]any{"rpc.method": "ArchiveAgents"})
	require.NoError(t, err)

	want := &logv1.GetLogLinesResponse{Lines: []*logv1.LogLine{{
		Time:    timestamppb.New(time.Date(2026, 8, 6, 22, 47, 32, 293_000_000, time.UTC)),
		Level:   "ERROR",
		Message: "rpc failed",
		Fields:  fields,
	}}}
	assert.Equal(t, []string{"trace-one"}, reader.traces)
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
			name:     "a cursor this server never answered",
			req:      &logv1.ReadAgentLogsRequest{AgentIds: []string{agentID.String()}, Cursor: "later"},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "a cursor before the start of the log",
			req:      &logv1.ReadAgentLogsRequest{AgentIds: []string{agentID.String()}, Cursor: "-1"},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:   "no cursor reads the newest, two hundred by default",
			req:    &logv1.ReadAgentLogsRequest{AgentIds: []string{agentID.String(), otherAgentID.String()}},
			window: &logread.Window{Lines: []string{"one", "two"}, Offset: 42},
			wantRead: agentRead{
				ids:    []string{agentID.String(), otherAgentID.String()},
				offset: -1,
				limit:  200,
			},
			want: &logv1.ReadAgentLogsResponse{Lines: []string{"one", "two"}, Cursor: "42"},
		},
		{
			name: "a cursor reads from where it left off",
			req: &logv1.ReadAgentLogsRequest{
				AgentIds: []string{agentID.String()},
				Cursor:   "42",
				Limit:    5,
			},
			window: &logread.Window{
				Lines:  []string{"three"},
				Offset: 90,
				More:   true,
			},
			wantRead: agentRead{
				ids:    []string{agentID.String()},
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
			req:  &logv1.ReadAgentLogsRequest{AgentIds: []string{agentID.String()}, Cursor: "900"},
			window: &logread.Window{
				Lines:  []string{"one"},
				Offset: 10,
				Reset:  true,
			},
			wantRead: agentRead{
				ids:    []string{agentID.String()},
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
			req:      &logv1.ReadAgentLogsRequest{AgentIds: []string{agentID.String(), otherAgentID.String()}, Source: "001-agent.provision-setup.log"},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:   "the server source reads the server log",
			req:    &logv1.ReadAgentLogsRequest{AgentIds: []string{agentID.String()}, Source: "server"},
			window: &logread.Window{Lines: []string{"one"}, Offset: 42},
			wantRead: agentRead{
				ids:    []string{agentID.String()},
				offset: -1,
				limit:  200,
			},
			want: &logv1.ReadAgentLogsResponse{Lines: []string{"one"}, Cursor: "42"},
		},
		{
			name:   "a run's source reads the newest of that run",
			req:    &logv1.ReadAgentLogsRequest{AgentIds: []string{agentID.String()}, Source: "001-agent.provision-setup.log"},
			window: &logread.Window{Lines: []string{"+ mise trust"}, Offset: 13},
			wantRead: agentRead{
				ids:    []string{agentID.String()},
				source: "001-agent.provision-setup.log",
				offset: -1,
				limit:  200,
			},
			want: &logv1.ReadAgentLogsResponse{Lines: []string{"+ mise trust"}, Cursor: "13"},
		},
		{
			name: "a run's source reads from its cursor",
			req: &logv1.ReadAgentLogsRequest{
				AgentIds: []string{agentID.String()},
				Source:   "001-agent.provision-setup.log",
				Cursor:   "13",
			},
			window: &logread.Window{Lines: []string{"+ mise install"}, Offset: 28},
			wantRead: agentRead{
				ids:    []string{agentID.String()},
				source: "001-agent.provision-setup.log",
				offset: 13,
				limit:  200,
			},
			want: &logv1.ReadAgentLogsResponse{Lines: []string{"+ mise install"}, Cursor: "28"},
		},
		{
			name:     "a pattern that does not compile",
			req:      &logv1.ReadAgentLogsRequest{AgentIds: []string{agentID.String()}, Pattern: "error("},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name: "a pattern narrows the server log from its cursor",
			req: &logv1.ReadAgentLogsRequest{
				AgentIds: []string{agentID.String()},
				Cursor:   "42",
				Pattern:  "(?i)error",
			},
			window: &logread.Window{Lines: []string{"ERROR one"}, Offset: 90},
			wantRead: agentRead{
				ids:     []string{agentID.String()},
				pattern: "(?i)error",
				offset:  42,
				limit:   200,
			},
			want: &logv1.ReadAgentLogsResponse{Lines: []string{"ERROR one"}, Cursor: "90"},
		},
		{
			name: "a pattern narrows a run's source",
			req: &logv1.ReadAgentLogsRequest{
				AgentIds: []string{agentID.String()},
				Source:   "001-agent.provision-setup.log",
				Pattern:  "mise",
			},
			window: &logread.Window{Lines: []string{"+ mise trust"}, Offset: 13},
			wantRead: agentRead{
				ids:     []string{agentID.String()},
				source:  "001-agent.provision-setup.log",
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
			service := logv1komrad.LogServiceAdapter{Impl: NewHandler(reader, reader, reader)}

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

// TestListAgentLogSources pins that the server log comes first and each run of the agent's own
// moments after it is named by its number, trigger and script, leaving its task's moments out.
func TestListAgentLogSources(t *testing.T) {
	reader := &fakeLogReader{runs: []config.RunLog{
		{
			Number:  3,
			Trigger: eventv1.Moment_MOMENT_TASK_PRE_OPEN,
			Script:  "setup",
			Path:    "/runs/a1/003-task.pre-open-setup.log",
		},
		{
			Number:  2,
			Trigger: eventv1.Moment_MOMENT_AGENT_PRE_LAUNCH,
			Script:  "setup",
			Path:    "/runs/a1/002-agent.pre-launch-setup.log",
		},
		{
			Number:  1,
			Trigger: eventv1.Moment_MOMENT_AGENT_PROVISION,
			Script:  "_worktree_setup",
			Path:    "/runs/a1/001-agent.provision-_worktree_setup.log",
		},
	}}
	service := logv1komrad.LogServiceAdapter{Impl: NewHandler(reader, reader, reader)}

	resp, err := service.ListAgentLogSources(t.Context(), connect.NewRequest(&logv1.ListAgentLogSourcesRequest{AgentId: agentID.String()}))
	require.NoError(t, err)

	want := &logv1.ListAgentLogSourcesResponse{Sources: []*logv1.LogSource{
		{Id: "server"},
		{
			Id:      "002-agent.pre-launch-setup.log",
			Run:     xptr.Ptr[int32](2),
			Trigger: eventv1.Moment_MOMENT_AGENT_PRE_LAUNCH,
			Script:  "setup",
			AgentId: agentID.String(),
		},
		{
			Id:      "001-agent.provision-_worktree_setup.log",
			Run:     xptr.Ptr[int32](1),
			Trigger: eventv1.Moment_MOMENT_AGENT_PROVISION,
			Script:  "_worktree_setup",
			AgentId: agentID.String(),
		},
	}}
	assert.Empty(t, cmp.Diff(want, resp.Msg, protocmp.Transform()))
}

// TestListTaskLogSources pins each run on a task naming the agent whose log holds it, newest
// first, with a run that wrote no log left out.
func TestListTaskLogSources(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		runs     []runstore.Run
		wantCode connect.Code
		want     []*logv1.LogSource
	}{
		{
			name:     "a listing that fails is internal",
			err:      errors.New("connection refused"),
			wantCode: connect.CodeInternal,
		},
		{
			name: "every run on the task is listed under the agent that ran it, a failed one marked",
			runs: []runstore.Run{
				{
					AgentID:   otherAgentID,
					Trigger:   eventv1.Moment_MOMENT_AGENT_PRE_LAUNCH,
					Script:    "setup",
					LogNumber: xptr.Ptr[int32](4),
					Failure:   "exit status 1",
				},
				{
					AgentID: agentID,
					Trigger: eventv1.Moment_MOMENT_TASK_PRE_OPEN,
					Script:  "unlogged",
				},
				{
					AgentID:   agentID,
					Trigger:   eventv1.Moment_MOMENT_TASK_PRE_OPEN,
					Script:    "setup",
					LogNumber: xptr.Ptr[int32](7),
				},
			},
			want: []*logv1.LogSource{
				{
					Id:      "004-agent.pre-launch-setup.log",
					Run:     xptr.Ptr[int32](4),
					Trigger: eventv1.Moment_MOMENT_AGENT_PRE_LAUNCH,
					Script:  "setup",
					AgentId: otherAgentID.String(),
					Failed:  true,
				},
				{
					Id:      "007-task.pre-open-setup.log",
					Run:     xptr.Ptr[int32](7),
					Trigger: eventv1.Moment_MOMENT_TASK_PRE_OPEN,
					Script:  "setup",
					AgentId: agentID.String(),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fakeLogReader{
				err:      tt.err,
				taskRuns: tt.runs,
			}
			service := logv1komrad.LogServiceAdapter{Impl: NewHandler(reader, reader, reader)}

			resp, err := service.ListTaskLogSources(t.Context(), connect.NewRequest(&logv1.ListTaskLogSourcesRequest{TaskId: taskID}))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))

				return
			}
			require.NoError(t, err)
			assert.Empty(t, cmp.Diff(&logv1.ListTaskLogSourcesResponse{Sources: tt.want}, resp.Msg, protocmp.Transform()))
		})
	}
}
