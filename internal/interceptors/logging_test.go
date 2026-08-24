package interceptors

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"weave/internal/caller"
	"weave/internal/logging"
	"weave/internal/logtest"
	agentv1 "weave/proto/gen/agent/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	require.NotNil(t, seen)
	assert.NotEmpty(t, caller.Trace(seen), "the handler can reach the trace id")
	assert.NotNil(t, logging.From(seen))

	return out.String(), res, err
}

type recordingRequest struct {
	connect.AnyRequest
	spec connect.Spec
	peer connect.Peer
}

func (r *recordingRequest) Spec() connect.Spec { return r.spec }
func (r *recordingRequest) Peer() connect.Peer { return r.peer }

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

// TestLoggingNamesTheCallingAgent covers a call from an agent the panel spawned. The panel
// and every agent reach the service over the same loopback connection, so the address says
// nothing about who called and the header is the only thing that does.
func TestLoggingNamesTheCallingAgent(t *testing.T) {
	req := unaryRequest("/agent.v1.AgentService/CreateAgent", "")
	req.Header().Set(caller.AgentHeader, "agent-1")
	req.Header().Set(caller.SessionHeader, "sess-1")

	written, _, err := loggedCall(t, slog.LevelDebug, req, nil)
	require.NoError(t, err)

	record := logtest.Record(t, written)
	assert.Equal(t, "agent-1", record[logging.AgentID])
	assert.Equal(t, "sess-1", record[logging.SessionID])
}

// TestLoggingLeavesThePanelUnattributed covers the panel's own calls, which are the user
// rather than an agent. An empty field would read as an agent whose id nobody recorded, so
// there is no field at all.
func TestLoggingLeavesThePanelUnattributed(t *testing.T) {
	written, _, err := loggedCall(t, slog.LevelDebug, unaryRequest("/agent.v1.AgentService/ListAgents", ""), nil)
	require.NoError(t, err)

	record := logtest.Record(t, written)
	assert.NotContains(t, record, logging.AgentID)
	assert.NotContains(t, record, logging.SessionID)
}

// TestLoggingHandsTheIdentityDown covers what the handler under the interceptor is left
// with. An RPC that calls back out has to be able to say who it is acting for, which is
// what keeps a call made on an agent's behalf attributed to that agent.
func TestLoggingHandsTheIdentityDown(t *testing.T) {
	req := unaryRequest("/agent.v1.AgentService/CreateAgent", "")
	req.Header().Set(caller.AgentHeader, "agent-1")

	var seen caller.Caller
	next := func(ctx context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
		seen = caller.From(ctx)

		return connect.NewResponse(&agentv1.ListAgentsResponse{}), nil
	}

	var out bytes.Buffer
	_, err := NewLogging(logging.New(&out, slog.LevelDebug)).WrapUnary(next)(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, caller.Caller{AgentID: "agent-1"}, seen)
}
