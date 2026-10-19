package cli

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"komrad/internal/diff"
	"komrad/internal/engine"
	"komrad/internal/git"
	"komrad/internal/logging"
	"komrad/internal/server"
	"komrad/lib/xhttp"
	"komrad/proto/gen/git/v1/gitv1connect"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInstallPanelLoggerWritesToTheLogFile covers where a panel-side record ends up.
func TestInstallPanelLoggerWritesToTheLogFile(t *testing.T) {
	restore := slog.Default()
	t.Cleanup(func() { slog.SetDefault(restore) })

	logPath := filepath.Join(t.TempDir(), "sub", "komrad.log")

	require.NoError(t, installPanelLogger(logPath, "info"))

	logging.From(t.Context()).ErrorContext(t.Context(), "panicked")

	written, err := os.ReadFile(logPath)
	require.NoError(t, err)

	var record map[string]any
	require.NoError(t, json.Unmarshal(written, &record))

	assert.Equal(t, "panicked", record[logging.BodyKey])
	assert.Equal(t, logging.ProcessPanel, record[logging.ProcessName],
		"the server appends to this file too, so a record has to say which process wrote it")
}

// TestInstallPanelLoggerCatchesThePanicHandler covers the record main writes on the way out,
// which carries no logger of its own and so reaches the file only through the default one.
func TestInstallPanelLoggerCatchesThePanicHandler(t *testing.T) {
	restore := slog.Default()
	t.Cleanup(func() { slog.SetDefault(restore) })

	logPath := filepath.Join(t.TempDir(), "komrad.log")

	require.NoError(t, installPanelLogger(logPath, "info"))

	logging.LogPanic(t.Context(), "runtime error: index out of range")

	written, err := os.ReadFile(logPath)
	require.NoError(t, err)

	var record map[string]any
	require.NoError(t, json.Unmarshal(written, &record))

	assert.Equal(t, "panicked", record[logging.BodyKey])
	assert.Equal(t, "runtime error: index out of range", record[logging.ErrorMessage])
	assert.NotEmpty(t, record[logging.StackTrace])
}

// TestInstallPanelLoggerRefusesABadLevel covers a typo in the configured level being a startup
// error rather than a panel that logs nothing.
func TestInstallPanelLoggerRefusesABadLevel(t *testing.T) {
	restore := slog.Default()
	t.Cleanup(func() { slog.SetDefault(restore) })

	logPath := filepath.Join(t.TempDir(), "komrad.log")

	require.Error(t, installPanelLogger(logPath, "chatty"))
	assert.NoFileExists(t, logPath)
}

// TestPatchReaderRunsOnTheEngineWhenTheAgentsAreThere covers the review screen reading the worktree where it is.
func TestPatchReaderRunsOnTheEngineWhenTheAgentsAreThere(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		remote engine.Remote
		want   diff.Reader
	}{
		{
			name:   "a remote engine reads through its api",
			remote: engine.Remote{URL: "https://komrad.example.com"},
			want:   &git.EngineClient{},
		},
		{
			name:   "a local engine reads here",
			remote: engine.Remote{},
			want:   &git.Client{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.IsType(t, tt.want, patchReader(tt.remote, gitv1connect.NewGitServiceClient(http.DefaultClient, "http://engine")))
		})
	}
}

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
				_ = json.NewEncoder(w).Encode(server.Health{Status: tt.health})
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
				require.NotErrorIs(t, err, engine.ErrEngineStopped)
			default:
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantEnsureSSH, ensuredSSH)
		})
	}
}
