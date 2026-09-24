package cli

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"komrade/internal/engine"
	"komrade/internal/git"
	"komrade/internal/logging"
	"komrade/internal/ui"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInstallPanelLoggerWritesToTheLogFile covers where a panel-side record ends up.
func TestInstallPanelLoggerWritesToTheLogFile(t *testing.T) {
	restore := slog.Default()
	t.Cleanup(func() { slog.SetDefault(restore) })

	logPath := filepath.Join(t.TempDir(), "sub", "komrade.log")

	require.NoError(t, installPanelLogger(logPath, "info"))

	logging.From(t.Context()).ErrorContext(t.Context(), "panicked")

	written, err := os.ReadFile(logPath)
	require.NoError(t, err)

	var record map[string]any
	require.NoError(t, json.Unmarshal(written, &record))

	assert.Equal(t, "panicked", record[logging.BodyKey])
	assert.Equal(t, logging.ProcessPanel, record[logging.ProcessName],
		"the service appends to this file too, so a record has to say which process wrote it")
}

// TestInstallPanelLoggerCatchesThePanicHandler covers the record main writes on the way out,
// which carries no logger of its own and so reaches the file only through the default one.
func TestInstallPanelLoggerCatchesThePanicHandler(t *testing.T) {
	restore := slog.Default()
	t.Cleanup(func() { slog.SetDefault(restore) })

	logPath := filepath.Join(t.TempDir(), "komrade.log")

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

	logPath := filepath.Join(t.TempDir(), "komrade.log")

	require.Error(t, installPanelLogger(logPath, "chatty"))
	assert.NoFileExists(t, logPath)
}

// TestReviewGitRunsOnTheEngineWhenTheAgentsAreThere covers the review screen reading the worktree where it is.
func TestReviewGitRunsOnTheEngineWhenTheAgentsAreThere(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		remote engine.Remote
		want   ui.ReviewGit
	}{
		{
			name:   "a remote engine reads through its transport",
			remote: engine.Remote{Destination: "komrade@host"},
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

			assert.IsType(t, tt.want, reviewGit(tt.remote))
		})
	}
}
