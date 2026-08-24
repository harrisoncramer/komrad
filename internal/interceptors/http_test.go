package interceptors

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"weave/internal/caller"
	"weave/internal/logging"
	"weave/internal/logtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func servedCall(t *testing.T, body string, handler http.HandlerFunc) (string, string) {
	t.Helper()

	var out bytes.Buffer

	seen := ""
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		read, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		seen = string(read)

		handler(w, r)
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	NewLoggingHandler(logging.New(&out, slog.LevelInfo), next).ServeHTTP(httptest.NewRecorder(), req)

	return out.String(), seen
}

// TestHTTPLoggingNamesTheTool covers the field that makes an MCP line worth reading. Every
// call arrives as a POST to the same path, so the path says nothing: the tool the model
// asked for is the only part that does, and it is in the body rather than the request line.
func TestHTTPLoggingNamesTheTool(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_agents","arguments":{}}}`

	written, seen := servedCall(t, body, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	record := logtest.Record(t, written)
	assert.Equal(t, "http served", record["body"])
	assert.Equal(t, "list_agents", record[logging.MCPTool])
	assert.Equal(t, "tools/call", record[logging.RPCMethod])
	assert.Equal(t, "POST", record[logging.HTTPMethod])
	assert.Equal(t, "/mcp", record[logging.URLPath])
	assert.EqualValues(t, 200, record[logging.HTTPStatus])
	assert.Contains(t, record, logging.DurationMS)

	assert.Equal(t, body, seen)
}

// TestHTTPLoggingRecordsAFailure covers what the status is read as, since nothing below
// this layer returns an error to it. A 5xx is the service breaking. A 4xx is the caller
// being told no, which is not the same thing: an MCP client probes this endpoint with a GET
// on every connect and is answered 405, and logging that as a failure would put a red
// herring in the file every time an agent starts.
func TestHTTPLoggingRecordsAFailure(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		wantSeverity string
	}{
		{name: "the service breaking", status: http.StatusInternalServerError, wantSeverity: "ERROR"},
		{name: "the caller being refused", status: http.StatusBadRequest, wantSeverity: "WARN"},
		{name: "a client probing for a method this endpoint does not serve", status: http.StatusMethodNotAllowed, wantSeverity: "WARN"},
		{name: "a call that worked", status: http.StatusOK, wantSeverity: "INFO"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			written, _ := servedCall(t, `{"method":"tools/call","params":{"name":"query"}}`, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			})

			record := logtest.Record(t, written)
			assert.Equal(t, tt.wantSeverity, record["severity_text"])
			assert.EqualValues(t, tt.status, record[logging.HTTPStatus])
		})
	}
}

// TestHTTPLoggingWithAnUnreadableBody covers a request that is not the envelope this knows
// how to describe. A line naming the path alone is still worth writing, so the call is
// logged without the fields that could not be worked out rather than not at all.
func TestHTTPLoggingWithAnUnreadableBody(t *testing.T) {
	written, seen := servedCall(t, "not json at all", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	record := logtest.Record(t, written)
	assert.Equal(t, "http served", record["body"])
	assert.NotContains(t, record, logging.MCPTool)
	assert.Equal(t, "not json at all", seen)
}

// TestHTTPLoggingKeepsTheHandlerFlushable covers the wrapper this puts around the response.
// MCP streams, and a flush that cannot find the real writer through the wrapper leaves the
// caller waiting on a response that has been written but never sent.
func TestHTTPLoggingKeepsTheHandlerFlushable(t *testing.T) {
	flushed := false

	written, _ := servedCall(t, `{"method":"tools/list"}`, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("chunk"))
		flushed = http.NewResponseController(w).Flush() == nil
	})

	assert.True(t, flushed, "the handler under this can still flush")
	assert.EqualValues(t, 200, logtest.Record(t, written)[logging.HTTPStatus],
		"a handler that only wrote a body still logs the status it implies")
}

// TestHTTPLoggingCarriesATrace covers the id the caller is given back, which is how a
// failure somebody is looking at is found in the log by the id they were shown.
func TestHTTPLoggingCarriesATrace(t *testing.T) {
	var out bytes.Buffer

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"method":"tools/list"}`))

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.NotEmpty(t, caller.Trace(r.Context()))
		w.WriteHeader(http.StatusOK)
	})

	NewLoggingHandler(logging.New(&out, slog.LevelInfo), next).ServeHTTP(recorder, req)

	assert.NotEmpty(t, recorder.Header().Get(TraceHeader))
	assert.NotEmpty(t, logtest.Record(t, out.String())[logging.TraceID])
}

// TestHTTPLoggingNamesTheCallingAgent covers an agent calling a tool. This is the edge most
// agent traffic arrives at, and the identity is read here exactly as it is on the RPC edge,
// so following one agent is one filter however it called.
func TestHTTPLoggingNamesTheCallingAgent(t *testing.T) {
	var out bytes.Buffer

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"method":"tools/call","params":{"name":"create_agent"}}`))
	req.Header.Set(caller.AgentHeader, "agent-1")
	req.Header.Set(caller.SessionHeader, "sess-1")

	var seen caller.Caller
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = caller.From(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	NewLoggingHandler(logging.New(&out, slog.LevelInfo), next).ServeHTTP(httptest.NewRecorder(), req)

	record := logtest.Record(t, out.String())
	assert.Equal(t, "agent-1", record[logging.AgentID])
	assert.Equal(t, "sess-1", record[logging.SessionID])
	assert.Equal(t, "create_agent", record[logging.MCPTool])

	assert.Equal(t, caller.Caller{AgentID: "agent-1", SessionID: "sess-1"}, seen)
}

// TestHTTPLoggingIgnoresAnUnexpandedVariable covers a CLI that passed the config through
// without filling it in. The header names a variable rather than a value, and recording it
// would put an agent called ${WEAVE_AGENT_ID} in the log and in every filter built on it.
func TestHTTPLoggingIgnoresAnUnexpandedVariable(t *testing.T) {
	var out bytes.Buffer

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"method":"tools/list"}`))
	req.Header.Set(caller.AgentHeader, "${WEAVE_AGENT_ID}")

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	NewLoggingHandler(logging.New(&out, slog.LevelInfo), next).ServeHTTP(httptest.NewRecorder(), req)

	assert.NotContains(t, logtest.Record(t, out.String()), logging.AgentID)
}
