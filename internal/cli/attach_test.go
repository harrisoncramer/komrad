package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"

	"weave/config"
	"weave/internal/agents"
	"weave/internal/tmux"
	"weave/internal/ui"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAttachRefusesWithoutAPanel runs against a real tmux server, since what is checked is
// that no session appears as a side effect.
func TestAttachRefusesWithoutAPanel(t *testing.T) {
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
			assert.Error(t, exec.Command("tmux", "-L", socket, "has-session", "-t", agents.ManagerSession).Run(),
				"attach created the session it was supposed to refuse")
		})
	}
}

func attachState(socket string) *app {
	servers := tmux.NewServers(tmux.NewClient(socket))

	return &app{
		env:   &config.Environment{TmuxSocket: socket},
		tmux:  servers,
		popup: ui.NewPopupWindow(servers, agents.ManagerSession),
	}
}

var sockets atomic.Int64

func throwawaySocket(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}

	socket := fmt.Sprintf("weave-attach-%d-%d", os.Getpid(), sockets.Add(1))
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", socket, "kill-server").Run() })

	return socket
}
