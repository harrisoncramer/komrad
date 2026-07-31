package interceptors

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"weave/internal/logging"
	"weave/internal/logtest"
	agentv1 "weave/proto/gen/agent/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loggedCall runs one request through the interceptor and gives back what it wrote and
// what came out. The handler answers with whatever it is given, so a case says only what
// it is about.
func loggedCall(t *testing.T, level slog.Level, req connect.AnyRequest, handlerErr error) (string, connect.AnyResponse, error) {
	t.Helper()

	var out bytes.Buffer
	interceptor := NewLogging(logging.New(&out, level))

	var seen context.Context
	next := func(ctx context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
		seen = ctx
		if handlerErr != nil {
			return nil, handlerErr
		}

		return connect.NewResponse(&agentv1.ListAgentsResponse{}), nil
	}

	res, err := interceptor.WrapUnary(next)(context.Background(), req)

	// The handler below the interceptor has to be able to log under the same trace as the
	// line the interceptor writes, which is the whole reason the logger is on the context.
	require.NotNil(t, seen)
	assert.NotEmpty(t, logging.Trace(seen), "the handler can reach the trace id")
	assert.NotNil(t, logging.From(seen))

	return out.String(), res, err
}

// recordingRequest is a request with the spec and peer a test wants. connect fills those
// in from the connection a real call arrived on, and the interface they belong to cannot
// be implemented outside connect, so a real request is embedded and the two answers that
// matter are given here.
type recordingRequest struct {
	connect.AnyRequest
	spec connect.Spec
	peer connect.Peer
}

func (r *recordingRequest) Spec() connect.Spec { return r.spec }
func (r *recordingRequest) Peer() connect.Peer { return r.peer }

// unaryRequest builds a request for a procedure, as connect hands one to an interceptor.
func unaryRequest(procedure, trace string) connect.AnyRequest {
	req := connect.NewRequest(&agentv1.ListAgentsRequest{})
	if trace != "" {
		req.Header().Set(TraceHeader, trace)
	}

	spec := connect.Spec{Procedure: procedure, IsClient: false}
	peer := connect.Peer{Addr: "127.0.0.1:54321", Protocol: connect.ProtocolConnect}

	return &recordingRequest{AnyRequest: req, spec: spec, peer: peer}
}

// TestLoggingRecordsAServedCall covers the record a call that worked writes, which is one
// record rather than one on the way in and another on the way out. It is a debug record:
// the panel lists everything it draws every second it is open, so an info line per call
// would be the whole file.
func TestLoggingRecordsAServedCall(t *testing.T) {
	written, res, err := loggedCall(t, slog.LevelDebug, unaryRequest("/agent.v1.AgentService/ListAgents", ""), nil)

	require.NoError(t, err)
	require.NotNil(t, res)

	record := logtest.Record(t, written)
	assert.Equal(t, "DEBUG", record["severity_text"])
	assert.Equal(t, "rpc served", record["body"])
	assert.Equal(t, "agent.v1.AgentService", record[logging.RPCService])
	assert.Equal(t, "ListAgents", record[logging.RPCMethod])
	assert.Equal(t, logging.RPCSystemConnect, record[logging.RPCSystem])
	assert.Equal(t, "127.0.0.1:54321", record[logging.ClientAddress])
	assert.Contains(t, record, logging.DurationMS)
	assert.NotEmpty(t, res.Header().Get(TraceHeader), "the caller is told the trace it can be found under")
}

// TestLoggingKeepsTheDefaultQuiet covers the level the file sits at. A served call writes
// nothing at info, which is what leaves a failure the only thing in it.
func TestLoggingKeepsTheDefaultQuiet(t *testing.T) {
	written, _, err := loggedCall(t, slog.LevelInfo, unaryRequest("/agent.v1.AgentService/ListAgents", ""), nil)

	require.NoError(t, err)
	assert.Empty(t, written)
}

// TestLoggingRecordsAFailure covers a call the handler refused. The connect code is its
// own field because it is what separates a caller sending something invalid from the
// service itself breaking, and a log where both read the same cannot be triaged.
func TestLoggingRecordsAFailure(t *testing.T) {
	refused := connect.NewError(connect.CodeInvalidArgument, errors.New("name is required"))

	written, _, err := loggedCall(t, slog.LevelInfo, unaryRequest("/agent.v1.AgentService/CreateAgent", ""), refused)

	require.Error(t, err)

	record := logtest.Record(t, written)
	assert.Equal(t, "ERROR", record["severity_text"])
	assert.EqualValues(t, 17, record["severity_number"])
	assert.Equal(t, "rpc failed", record["body"])
	assert.Equal(t, "invalid_argument", record[logging.RPCErrorCode])
	assert.Contains(t, record[logging.ErrorMessage], "name is required")

	var connectErr *connect.Error
	require.True(t, errors.As(err, &connectErr))
	assert.NotEmpty(t, connectErr.Meta().Get(TraceHeader), "a failure carries the trace back to whoever saw it")
}

// TestLoggingRecordsABareFailure covers a handler that returned something that is not a
// connect error. Connect answers those as unknown, so that is what the record says rather
// than leaving the field empty and the line unreadable.
func TestLoggingRecordsABareFailure(t *testing.T) {
	written, _, err := loggedCall(t, slog.LevelInfo, unaryRequest("/agent.v1.AgentService/CreateAgent", ""), errors.New("boom"))

	require.Error(t, err)

	record := logtest.Record(t, written)
	assert.Equal(t, "unknown", record[logging.RPCErrorCode])
	assert.Equal(t, "boom", record[logging.ErrorMessage])
	assert.Equal(t, "*errors.errorString", record[logging.ErrorType])
}

// TestLoggingKeepsTheCallersTrace covers a caller that already has a trace id. Its own id
// is what the call is logged under, so a request crossing two processes is one search
// rather than two.
func TestLoggingKeepsTheCallersTrace(t *testing.T) {
	written, res, err := loggedCall(t, slog.LevelDebug, unaryRequest("/agent.v1.AgentService/ListAgents", "abc-123"), nil)

	require.NoError(t, err)
	assert.Equal(t, "abc-123", logtest.Record(t, written)[logging.TraceID])
	assert.Equal(t, "abc-123", res.Header().Get(TraceHeader))
}

// TestSplitProcedure covers how a procedure path becomes two fields, including the shapes
// connect does not produce, since a record naming the wrong thing is worse than one
// naming less.
func TestSplitProcedure(t *testing.T) {
	tests := []struct {
		name        string
		procedure   string
		wantService string
		wantMethod  string
	}{
		{
			name:        "a procedure as connect gives it",
			procedure:   "/workflow.v1.WorkflowService/ListWorkflows",
			wantService: "workflow.v1.WorkflowService",
			wantMethod:  "ListWorkflows",
		},
		{
			name:       "a path with nothing to split is kept whole",
			procedure:  "/health",
			wantMethod: "health",
		},
		{
			name: "nothing at all",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, method := splitProcedure(tt.procedure)

			assert.Equal(t, tt.wantService, service)
			assert.Equal(t, tt.wantMethod, method)
		})
	}
}
