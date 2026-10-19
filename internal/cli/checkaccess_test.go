package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"komrad/internal/engine"
	"komrad/internal/server"
	"komrad/lib/xhttp"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCheckAccessReadsWhetherTheEngineIsRunning covers what the panel hears from an engine's
// health path when the control plane answers for the engine.
func TestCheckAccessReadsWhetherTheEngineIsRunning(t *testing.T) {
	tests := []struct {
		name          string
		status        int
		health        string
		wantErr       error
		wantReason    string
		wantEnsureSSH bool
	}{
		{
			name:          "an engine that answers",
			status:        http.StatusOK,
			health:        "ok",
			wantEnsureSSH: true,
		},
		{
			name:    "a stopped engine",
			status:  http.StatusServiceUnavailable,
			health:  server.HealthStopped,
			wantErr: engine.ErrEngineStopped,
		},
		{
			name:    "a starting engine",
			status:  http.StatusServiceUnavailable,
			health:  server.HealthStarting,
			wantErr: engine.ErrEngineStarting,
		},
		{
			name:       "an engine whose database is down",
			status:     http.StatusServiceUnavailable,
			health:     "unavailable",
			wantReason: "answered 503 Service Unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			served := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_ = json.NewEncoder(w).Encode(server.Health{Status: tt.health}) //nolint:errchkjson // the test hears a cut answer as a failed request
			}))
			t.Cleanup(served.Close)

			var ensuredSSH bool
			signIn := &panelSignIn{
				url:       served.URL + "/harry",
				transport: xhttp.NewTransport(),
				ensureSSH: func(context.Context) error {
					ensuredSSH = true

					return nil
				},
			}

			err := signIn.CheckAccess(t.Context())
			switch {
			case tt.wantErr != nil:
				require.ErrorIs(t, err, tt.wantErr)
			case tt.wantReason != "":
				require.ErrorContains(t, err, tt.wantReason)
				assert.NotErrorIs(t, err, engine.ErrEngineStopped)
			default:
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantEnsureSSH, ensuredSSH)
		})
	}
}
