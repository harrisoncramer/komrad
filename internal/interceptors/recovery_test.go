package interceptors

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"komrad/internal/logging"
	"komrad/internal/logtest"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRecoverRPCIsInternal covers a panic answered as internal with the value left out.
func TestRecoverRPCIsInternal(t *testing.T) {
	var out bytes.Buffer
	ctx := logging.WithLogger(t.Context(), logging.New(&out, slog.LevelError))

	err := recoverRPC(ctx, "the pool was closed")

	require.Error(t, err)
	assert.Equal(t, connect.CodeInternal, connect.CodeOf(err))
	assert.Equal(t, "internal: the server panicked", err.Error())

	record := logtest.Record(t, out.String())
	assert.Equal(t, "panicked", record["body"])
	assert.Equal(t, "the pool was closed", record[logging.ErrorMessage])
	assert.Contains(t, record[logging.StackTrace], "recovery_test.go",
		"the stack reaches back to what panicked")
}

// TestServeAndRecoverAnswers covers a panicking MCP call being answered rather than dropped,
// which connect's own recovery never sees on an endpoint served as plain HTTP.
func TestServeAndRecoverAnswers(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantStatus int
	}{
		{
			name: "a panic before anything was written is answered as a failure",
			handler: func(_ http.ResponseWriter, _ *http.Request) {
				panic("the store went away")
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "and one partway through a response leaves the status it sent",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusAccepted)
				panic("the store went away")
			},
			wantStatus: http.StatusAccepted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer

			response := httptest.NewRecorder()
			recorder := &statusRecorder{ResponseWriter: response, status: http.StatusOK}

			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			ctx := logging.WithLogger(req.Context(), logging.New(&out, slog.LevelError))

			serveAndRecover(recorder, req.WithContext(ctx), tt.handler)

			assert.Equal(t, tt.wantStatus, response.Code)
			assert.Equal(t, tt.wantStatus, recorder.status, "the line for the call reports what was sent")

			record := logtest.Record(t, out.String())
			assert.Equal(t, "panicked", record["body"])
			assert.Equal(t, "the store went away", record[logging.ErrorMessage])
		})
	}
}

// TestServeAndRecoverPassesOnAnAbort covers net/http's own signal for a deliberate drop,
// which recovering here would turn into a failure of ours.
func TestServeAndRecoverPassesOnAnAbort(t *testing.T) {
	var out bytes.Buffer

	recorder := &statusRecorder{ResponseWriter: httptest.NewRecorder(), status: http.StatusOK}
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	ctx := logging.WithLogger(req.Context(), logging.New(&out, slog.LevelError))

	aborting := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic(http.ErrAbortHandler)
	})

	assert.PanicsWithValue(t, http.ErrAbortHandler, func() {
		serveAndRecover(recorder, req.WithContext(ctx), aborting)
	})
	assert.Empty(t, out.String(), "an abort is not a failure to report")
}

// TestLoggingHandlerRecordsAPanickedCall covers a panicking MCP call leaving both the stack
// and the line saying it ended in a 500, neither of which is written without the recovery.
func TestLoggingHandlerRecordsAPanickedCall(t *testing.T) {
	var out bytes.Buffer

	panicking := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("the store went away")
	})

	response := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	NewLoggingHandler(logging.New(&out, slog.LevelInfo), panicking).ServeHTTP(response, req)

	assert.Equal(t, http.StatusInternalServerError, response.Code)
	assert.Equal(t, []string{"panicked", "http served"}, logtest.Bodies(t, out.String()))
}
