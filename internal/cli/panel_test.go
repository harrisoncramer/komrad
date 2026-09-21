package cli

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"weave/internal/logging"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInstallPanelLoggerWritesToTheLogFile covers where a panel-side record ends up.
func TestInstallPanelLoggerWritesToTheLogFile(t *testing.T) {
	restore := slog.Default()
	t.Cleanup(func() { slog.SetDefault(restore) })

	logPath := filepath.Join(t.TempDir(), "sub", "weave.log")

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

	logPath := filepath.Join(t.TempDir(), "weave.log")

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

	logPath := filepath.Join(t.TempDir(), "weave.log")

	require.Error(t, installPanelLogger(logPath, "chatty"))
	assert.NoFileExists(t, logPath)
}
