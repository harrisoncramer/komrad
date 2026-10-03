//go:build integration

package tmuxtest_test

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"komrad/internal/tmuxtest"
	"komrad/lib/clock"

	"github.com/stretchr/testify/require"
)

var holdServer = flag.String("hold-server", "", "start a server, write its socket to this file, and wait to be killed")

// TestIntegrationServerGoesWithAKilledTestProcess covers a test killed before its cleanup can run.
func TestIntegrationServerGoesWithAKilledTestProcess(t *testing.T) {
	if *holdServer != "" {
		server := tmuxtest.New(t)
		server.NewSession("held")
		require.NoError(t, os.WriteFile(*holdServer, []byte(server.Socket()), 0o600))
		clock.Sleep(t.Context(), time.Hour)

		return
	}

	// Explainer: the test runs itself again as a child process, which starts a server, writes its socket name to a file, and sleeps.
	// The parent kills the child the way a timeout or a closed pane would, so none of the child's cleanup runs.
	// Only the watchdog is left to shut the server down, so the server going away shows the watchdog worked.
	socketFile := filepath.Join(t.TempDir(), "socket")
	child := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$", "-hold-server="+socketFile)
	require.NoError(t, child.Start())
	t.Cleanup(func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	})

	var socket string
	require.Eventually(t, func() bool {
		raw, err := os.ReadFile(socketFile)
		socket = string(raw)

		return err == nil && socket != ""
	}, 30*time.Second, 50*time.Millisecond, "the child never started its server")
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", socket, "kill-server").Run() })

	require.NoError(t, child.Process.Kill())
	_ = child.Wait()

	require.Eventually(t, func() bool {
		return exec.Command("tmux", "-L", socket, "has-session").Run() != nil
	}, 10*time.Second, 100*time.Millisecond, "the server outlived the test process that started it")
}
