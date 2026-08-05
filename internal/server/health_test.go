package server

import (
	"errors"
	"net/http"
	"testing"

	"weave/internal/version"

	"github.com/stretchr/testify/assert"
)

// TestHealthBody covers what the route says about itself, since the panel decides whether to
// replace the server on the version in it.
func TestHealthBody(t *testing.T) {
	tests := []struct {
		name       string
		ping       error
		wantStatus int
		wantState  string
		wantError  string
	}{
		{
			name:       "a database it cannot reach",
			ping:       errors.New("connection refused"),
			wantStatus: http.StatusServiceUnavailable,
			wantState:  "unavailable",
			wantError:  "database unreachable: connection refused",
		},
		{
			name:       "a database it can reach",
			ping:       nil,
			wantStatus: http.StatusOK,
			wantState:  "ok",
			wantError:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, status := healthBody(tt.ping)

			assert.Equal(t, tt.wantStatus, status)
			assert.Equal(t, tt.wantState, body.Status)
			assert.Equal(t, tt.wantError, body.Error)
			assert.Equal(t, version.Version(), body.Version,
				"a build whose database is down still has to say which build it is")
		})
	}
}
