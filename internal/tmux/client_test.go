package tmux

import (
	"bytes"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestQueryCarriesStderr checks a failed tmux call names the command and carries tmux's own
// complaint.
func TestQueryCarriesStderr(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}

	_, err := NewClient("weave-test-no-such-socket").GetPanes()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "list-panes", "the failing command is named")
	assert.Contains(t, err.Error(), "weave-test-no-such-socket", "so is the server it ran against")
	assert.Contains(t, err.Error(), "error connecting", "and tmux's own stderr comes through")
}

// TestQueryGivesUpOnWedgedTmux covers a tmux that never answers, which would otherwise stall
// a whole reconcile pass with no word of why.
func TestQueryGivesUpOnWedgedTmux(t *testing.T) {
	tests := []struct {
		name   string
		script string
	}{
		{
			name:   "a tmux that never answers",
			script: "#!/bin/sh\nexec /bin/sleep 30\n",
		},
		{
			// A real tmux daemonizes a server holding the stdout it inherited, which keeps the
			// read open past the kill.
			name:   "a tmux that leaves a child holding its output",
			script: "#!/bin/sh\n/bin/sleep 30 &\nexec /bin/sleep 30\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PATH", fakeTmux(t, tt.script))

			type result struct {
				out string
				err error
			}

			done := make(chan result, 1)
			go func() {
				out, err := (&Client{}).query("list-panes")
				done <- result{out: out, err: err}
			}()

			select {
			case got := <-done:
				require.Error(t, got.err, "giving up is a failure, so a caller reading state is not told the pane is gone")
				assert.Contains(t, got.err.Error(), "gave up waiting", "it returned because it ran out of time, not because tmux answered")
				assert.Contains(t, got.err.Error(), "list-panes", "the call that hung is named")
				assert.Empty(t, got.out, "and nothing half-read is passed off as an answer")
			case <-time.After(queryTimeout + 2*time.Second):
				t.Fatal("query did not return against a tmux that never answers")
			}
		})
	}
}

func fakeTmux(t *testing.T, script string) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tmux"), []byte(script), 0o755))

	return dir
}

func captureDefaultLog(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	return &buf
}

// TestKillPaneIsQuietWhenThePaneIsAlreadyGone covers a delete and an archive racing the same
// pane, where the loser's kill-pane finds it gone and used to log a WARN.
func TestKillPaneIsQuietWhenThePaneIsAlreadyGone(t *testing.T) {
	t.Setenv("PATH", fakeTmux(t, "#!/bin/sh\necho \"can't find pane: %13\" >&2\nexit 1\n"))
	buf := captureDefaultLog(t)

	(&Client{}).KillPane("%13")

	assert.Empty(t, buf.String(), "the pane being already gone is the wanted outcome, not a WARN")
}

// TestKillPaneStillWarnsOnARealFailure guards the quiet case above from swallowing a failure
// that is not the pane being gone.
func TestKillPaneStillWarnsOnARealFailure(t *testing.T) {
	t.Setenv("PATH", fakeTmux(t, "#!/bin/sh\necho 'error connecting to /tmp/tmux-501/weave (No such file or directory)' >&2\nexit 1\n"))
	buf := captureDefaultLog(t)

	(&Client{}).KillPane("%13")

	assert.Contains(t, buf.String(), "a tmux command failed", "a real failure is not silenced")
}

// TestIsRunningShellReadsMeasuredCommands pins the shell allowlist to what a pane actually
// reports, which is never the binary weave launched.
func TestIsRunningShellReadsMeasuredCommands(t *testing.T) {
	shells := []string{"sh", "bash", "dash", "ash", "zsh", "ksh", "csh", "tcsh", "fish", "nu", "-zsh", "-bash", "-dash"}
	for _, command := range shells {
		assert.True(t, PaneStatus{Command: command}.IsRunningShell(), "%q is a shell", command)
	}

	// Claude sets its own process title so a pane reports its version, and Codex reports the
	// wrapper script it was installed as.
	others := []string{"2.1.241", ".codex-wrapped", "codex", "claude", "node", "cat", ""}
	for _, command := range others {
		assert.False(t, PaneStatus{Command: command}.IsRunningShell(), "%q is not a shell", command)
	}
}
