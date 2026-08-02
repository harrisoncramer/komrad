package interceptors

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"weave/internal/logging"
	"weave/internal/logtest"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRecoverRPCIsInternal covers what a caller is told when a handler panicked. The code is
// internal because nothing the caller sent explains it, and the value is left out of the
// message since a panic can be holding anything the handler was.
func TestRecoverRPCIsInternal(t *testing.T) {
	var out bytes.Buffer
	ctx := logging.WithLogger(context.Background(), logging.New(&out, slog.LevelError))

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

// TestServeAndRecoverAnswers covers the MCP endpoint, which connect's own recovery never sees
// because it is served as plain HTTP. A panicking call is answered and recorded rather than
// dropping the connection, and one that had already written its status keeps it, since a
// second status is a lie the client cannot read anyway.
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

// TestServeAndRecoverPassesOnAnAbort covers net/http's own signal for a handler dropping a
// request deliberately. The server above is the thing meant to see it, so recovering it here
// would turn a stream the client hung up on into a failure of ours.
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

// TestLoggingHandlerRecordsAPanickedCall covers the two records a panicking MCP call leaves:
// what broke, with the stack, and the line saying the call ended in a 500. Without the
// recovery the call would leave neither, since the line is written after the handler returns.
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
