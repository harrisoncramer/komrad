package log

import (
	"errors"
	"testing"

	"komrade/internal/logread"
	logv1 "komrade/proto/gen/log/v1"
	"komrade/proto/gen/log/v1/logv1komrade"

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
	err       error
	traces    []string
	limits    []int
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := logv1komrade.LogServiceAdapter{Impl: NewHandler(&fakeLogReader{err: tt.err})}

			_, err := service.GetLogLines(t.Context(), connect.NewRequest(&logv1.GetLogLinesRequest{TraceId: "trace-nine"}))
			assert.Equal(t, tt.wantCode, connect.CodeOf(err))

			_, err = service.ListRecentTraces(t.Context(), connect.NewRequest(&logv1.ListRecentTracesRequest{Limit: 5}))
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
	service := logv1komrade.LogServiceAdapter{Impl: NewHandler(reader)}

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
	service := logv1komrade.LogServiceAdapter{Impl: NewHandler(reader)}

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
