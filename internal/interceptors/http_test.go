package interceptors

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"weave/internal/logging"
	"weave/internal/logtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// servedCall puts one request through the middleware and gives back what was logged and
// what the handler under it saw of the body.
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

	// Reading the body to find the tool consumes a stream that can only be read once, so
	// the handler below has to still get all of it.
	assert.Equal(t, body, seen)
}

// TestHTTPLoggingRecordsAFailure covers a call the handler refused. Nothing below this
// layer returns an error to it, so the status is the only thing that says the call did not
// work, and a refusal logged at info would sit unread among the ones that did.
func TestHTTPLoggingRecordsAFailure(t *testing.T) {
	written, _ := servedCall(t, `{"method":"tools/call","params":{"name":"query"}}`, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	})

	record := logtest.Record(t, written)
	assert.Equal(t, "ERROR", record["severity_text"])
	assert.EqualValues(t, 400, record[logging.HTTPStatus])
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
		// The trace is on the context as well as on the response, so anything below can log
		// under the same id as the line this writes.
		assert.NotEmpty(t, logging.Trace(r.Context()))
		w.WriteHeader(http.StatusOK)
	})

	NewLoggingHandler(logging.New(&out, slog.LevelInfo), next).ServeHTTP(recorder, req)

	assert.NotEmpty(t, recorder.Header().Get(TraceHeader))
	assert.NotEmpty(t, logtest.Record(t, out.String())[logging.TraceID])
}
