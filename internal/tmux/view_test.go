//go:build integration

package tmux

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"komrad/lib/clock"

	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type attachment struct {
	*exec.Cmd

	done chan struct{}
}

// TestIntegrationViews covers a view session from being opened on a pane to being killed.
func TestIntegrationViews(t *testing.T) {
	client := testServer(t, "komrad")

	pane, err := client.NewWindow("komrad", "agent-one", t.TempDir())
	require.NoError(t, err)
	window, err := client.query("display-message", "-p", "-t", pane, "#{window_id}")
	require.NoError(t, err)

	t.Run("a view looks at the pane's window with no status line", func(t *testing.T) {
		require.NoError(t, client.OpenView("view-1", "komrad", pane))

		shown, err := client.query("display-message", "-p", "-t", "=view-1:", "#{window_id} #{status}")
		require.NoError(t, err)
		assert.Equal(t, window+" off", shown)
	})

	t.Run("a view opened again is reused", func(t *testing.T) {
		require.NoError(t, client.OpenView("view-1", "komrad", pane))

		sessions, err := client.query("list-sessions", "-F", "#{session_name}")
		require.NoError(t, err)
		assert.Equal(t, []string{"komrad", "view-1"}, strings.Split(sessions, "\n"))
	})

	t.Run("a second attach detaches the first", func(t *testing.T) {
		clients, err := client.CountSessionClients("view-1")
		require.NoError(t, err)
		require.Zero(t, clients)

		first := attachTo(t, client, "view-1")
		waitForClients(t, client, "view-1", 1)

		second := attachTo(t, client, "view-1")

		select {
		case <-first.done:
		case <-time.After(5 * time.Second):
			t.Fatal("the first attach was not detached")
		}

		waitForClients(t, client, "view-1", 1)
		require.NoError(t, second.Process.Kill())
		waitForClients(t, client, "view-1", 0)
	})

	t.Run("killing a view leaves the agent's window", func(t *testing.T) {
		require.NoError(t, client.KillSession("view-1"))

		_, err := client.CountSessionClients("view-1")
		require.Error(t, err)
		assert.True(t, client.IsPaneAlive(pane))
	})
}

// attachTo attaches a terminal to a session with BuildAttachArgv.
func attachTo(t *testing.T, client *Client, session string) attachment {
	t.Helper()

	argv := client.BuildAttachArgv(session)
	command := exec.Command(argv[0], argv[1:]...) //nolint:gosec // the argv is tmux's own
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "TMUX=") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, "TERM=xterm-256color")

	terminal, err := pty.StartWithSize(command, &pty.Winsize{Rows: 24, Cols: 80})
	require.NoError(t, err)
	t.Cleanup(func() { _ = terminal.Close() })

	done := make(chan struct{})
	go func() {
		buffer := make([]byte, 4096)
		for {
			if _, err := terminal.Read(buffer); err != nil {
				break
			}
		}
	}()
	go func() {
		_ = command.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		_ = command.Process.Kill()
		<-done
	})

	return attachment{Cmd: command, done: done}
}

func waitForClients(t *testing.T, client *Client, session string, want int) {
	t.Helper()

	for range 100 {
		clients, err := client.CountSessionClients(session)
		if err == nil && clients == want {
			return
		}

		clock.Sleep(t.Context(), 50*time.Millisecond)
	}

	t.Fatalf("session %s never had %d clients attached", session, want)
}
