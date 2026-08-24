package tmux

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientTarget(t *testing.T) {
	tests := []struct {
		name   string
		client string
		want   []string
	}{
		{
			name:   "an empty client yields no target flags",
			client: "",
			want:   nil,
		},
		{
			name:   "a named client yields a -c target",
			client: "/dev/ttys001",
			want:   []string{"-c", "/dev/ttys001"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, clientTarget(tt.client))
		})
	}
}

// TestQueryCarriesStderr covers what a failed tmux call reports. These run on a loop with
// nobody watching, so an exit status with neither the command nor tmux's own complaint in
// it is the difference between a diagnosable failure and a mystery.
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

// TestQueryGivesUpOnWedgedTmux covers a tmux that never answers, which is the failure the
// reconcile loop cannot survive. A pass runs to completion before the loop looks at its
// context again, so a call that blocks forever stops every agent being observed and never
// says why.
//
// The second case is why killing the process is not the whole fix. A tmux that leaves
// something else holding the stdout it inherited keeps the read open past the kill, and a
// real tmux daemonizes a server that does precisely that.
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

// TestRunsShellReadsMeasuredCommands pins the allowlist to what the CLIs actually report.
// A pane running Claude reports its version string, because Claude sets its own process
// title, and one running Codex reports the name of the wrapper script it was installed as.
// Neither is the binary weave launched, so the test is only ever whether a pane is back at
// a shell, and anything unrecognised has to read as a CLI still running.
func TestRunsShellReadsMeasuredCommands(t *testing.T) {
	shells := []string{"sh", "bash", "dash", "ash", "zsh", "ksh", "csh", "tcsh", "fish", "nu", "-zsh", "-bash", "-dash"}
	for _, command := range shells {
		assert.True(t, PaneStatus{Command: command}.RunsShell(), "%q is a shell", command)
	}

	others := []string{"2.1.241", ".codex-wrapped", "codex", "claude", "node", "cat", ""}
	for _, command := range others {
		assert.False(t, PaneStatus{Command: command}.RunsShell(), "%q is not a shell", command)
	}
}
