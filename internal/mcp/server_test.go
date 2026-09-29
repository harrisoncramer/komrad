package mcp

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	annotationv1 "komrad/proto/gen/annotation/v1"

	"komrad/internal/caller"
	"komrad/internal/rpcapi"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandlerCallerRole pins the tool server a caller is handed by the role komrad reads for it.
func TestHandlerCallerRole(t *testing.T) {
	tests := []struct {
		name       string
		agentID    string
		lookup     error
		wantStatus int
	}{
		{
			name:       "an agent komrad does not know is refused",
			agentID:    "gone",
			lookup:     rpcapi.ErrUnknownCaller,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "an agent whose role cannot be read is refused until it can",
			agentID:    "unreadable",
			lookup:     errors.New("connection refused"),
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "a known agent is served",
			agentID:    "implementer",
			wantStatus: http.StatusOK,
		},
		{
			name:       "a caller naming no agent is served",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := NewHandler(NewHandlerParams{
				BaseURL: "http://127.0.0.1:1",
				RoleOf: func(context.Context, string) (annotationv1.Role, error) {
					return annotationv1.Role_ROLE_IMPLEMENTER, tt.lookup
				},
				Logger: slog.New(slog.DiscardHandler),
			})
			require.NoError(t, err)

			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", strings.NewReader(
				`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Accept", "application/json, text/event-stream")
			caller.Caller{AgentID: tt.agentID}.SetHeader(request.Header)

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			assert.Equal(t, tt.wantStatus, recorder.Code, recorder.Body.String())
		})
	}
}
