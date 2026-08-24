package cli

import (
	"fmt"
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

// TestAttachRefusesWithoutAPanel covers the one decision attach makes that open does not: it
// shows a panel that is running and starts nothing. It runs against a real tmux server rather
// than a fake, since what is checked is that no session appears as a side effect.
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

			err := attachCmd(attachState(socket)).RunE(nil, nil)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "the panel is not running")
			assert.Error(t, exec.Command("tmux", "-L", socket, "has-session", "-t", agents.ManagerSession).Run(),
				"attach created the session it was supposed to refuse")
		})
	}
}

// attachState is the wiring the root command builds, cut down to what attach reaches.
func attachState(socket string) *app {
	manager := tmux.NewClient(socket)
	host := tmux.NewHost(manager, "")
	servers := tmux.NewServers(manager, host)

	return &app{
		env:   &config.Environment{TmuxSocket: socket},
		host:  host,
		tmux:  servers,
		popup: ui.NewPopupWindow(servers, agents.ManagerSession),
	}
}

// sockets counts the servers this package has named. A name has to be this call's own, not
// this test's: t.TempDir() numbers its directories per test, so its first is 001 in every
// one, and two tests asking for that socket each registered a cleanup that killed the
// other's server. The pid keeps the names apart from any other package's run.
var sockets atomic.Int64

// throwawaySocket names a tmux server of this test's own, killed on cleanup, so nothing here
// touches the panel's server or the user's.
func throwawaySocket(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}

	socket := fmt.Sprintf("weave-attach-%d-%d", os.Getpid(), sockets.Add(1))
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", socket, "kill-server").Run() })

	return socket
}
