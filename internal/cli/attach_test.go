//go:build integration

package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"

	"komrad/config"
	"komrad/internal/agents"
	"komrad/internal/tmux"
	"komrad/internal/ui"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationAttachRefusesWithoutAPanel runs against a real tmux server, where the check is that no
// session appears as a side effect.
func TestIntegrationAttachRefusesWithoutAPanel(t *testing.T) {
	tests := []struct {
		name   string
		server bool
	}{
		{
			name:   "no tmux server at all",
			server: false,
		},
		{
			name:   "a server holding some other session",
			server: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			socket := throwawaySocket(t)
			if tt.server {
				out, err := exec.Command("tmux", "-L", socket, "-f", "/dev/null",
					"new-session", "-d", "-s", "something-else").CombinedOutput()
				require.NoError(t, err, "tmux: %s", out)
			}

			cmd := attachCmd(attachState(socket))
			cmd.SetArgs([]string{})
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)

			err := cmd.Execute()

			require.Error(t, err)
			assert.Contains(t, err.Error(), "the panel is not running")
			require.Error(t, exec.Command("tmux", "-L", socket, "has-session", "-t", agents.ManagerSession).Run(),
				"attach created the session it was supposed to refuse")
		})
	}
}

func attachState(socket string) *app {
	servers := tmux.NewClient(socket)

	return &app{
		env:  &config.Environment{TmuxSocket: socket},
		tmux: servers,
		session: ui.NewSession(ui.NewSessionParams{
			Tmux:        servers,
			Session:     agents.ManagerSession,
			LeaderKey:   tmux.LeaderKey,
			LeaderTable: tmux.LeaderTable,
		}),
	}
}

var sockets atomic.Int64

func throwawaySocket(t *testing.T) string {
	t.Helper()
	_, err := exec.LookPath("tmux")
	require.NoError(t, err)

	socket := fmt.Sprintf("komrad-attach-%d-%d", os.Getpid(), sockets.Add(1))
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", socket, "kill-server").Run() })

	return socket
}

func panelState(t *testing.T, socket string) *app {
	t.Helper()
	t.Setenv("HOME", t.TempDir())

	return attachState(socket)
}

func panelSession(t *testing.T, socket string) {
	t.Helper()

	out, err := exec.Command("tmux", "-L", socket, "-f", "/dev/null",
		"new-session", "-d", "-s", agents.ManagerSession).CombinedOutput()
	require.NoError(t, err, "tmux: %s", out)
}

func hostQuery(t *testing.T, socket string, args ...string) string {
	t.Helper()

	out, err := exec.Command("tmux", append([]string{"-L", socket}, args...)...).Output()
	require.NoError(t, err, "tmux %v", args)

	return strings.TrimSpace(string(out))
}
