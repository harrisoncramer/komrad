package cli

import (
	"os/exec"
	"strings"
	"testing"

	"weave/config"
	"weave/internal/agents"
	"weave/internal/tmux"
	"weave/internal/ui"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHideClearsTheOpenFlagWithoutTheManagerServer covers hiding the panel once weave's own
// tmux server has gone; the flag the key binding branches on lives on the user's server, so
// left set it makes the key take the hide branch on every press and the panel becomes
// unreachable. It runs against a real tmux server, since what is checked is which server an
// option landed on.
func TestHideClearsTheOpenFlagWithoutTheManagerServer(t *testing.T) {
	hostSocket := throwawaySocket(t)
	require.NoError(t, exec.Command("tmux", "-L", hostSocket, "-f", "/dev/null",
		"new-session", "-d", "-s", "userland").Run())
	require.NoError(t, exec.Command("tmux", "-L", hostSocket, "set-option", "-g", "@weave_open", "1").Run())

	// A socket name no server is listening on, which is what a killed manager server leaves.
	state := hideState(throwawaySocket(t), hostPaneEnv(t, hostSocket))

	require.NoError(t, hideCmd(state).RunE(nil, nil))
	assert.Equal(t, "0", hostGlobal(t, hostSocket, "@weave_open"),
		"the panel key reads this flag, so a hide that leaves it set makes the key do nothing")
}

// hideState is the wiring the root command builds, cut down to what hide reaches; tmuxEnv
// stands in for this process's TMUX, which is what tmux sets for the pane the key binding's
// run-shell puts hide in.
func hideState(socket, tmuxEnv string) *app {
	manager := tmux.NewClient(socket)
	host := tmux.NewHost(manager, tmuxEnv)
	servers := tmux.NewServers(manager, host)

	return &app{
		env:   &config.Environment{TmuxSocket: socket, Tmux: tmuxEnv},
		host:  host,
		tmux:  servers,
		popup: ui.NewPopupWindow(servers, agents.ManagerSession),
	}
}

// hostPaneEnv is a TMUX value naming a test server, the three fields tmux writes for a pane.
func hostPaneEnv(t *testing.T, socket string) string {
	t.Helper()

	return hostQuery(t, socket, "display-message", "-p", "#{socket_path}") + ",1,0"
}

func hostGlobal(t *testing.T, socket, name string) string {
	t.Helper()

	return hostQuery(t, socket, "show-option", "-gqv", name)
}

func hostQuery(t *testing.T, socket string, args ...string) string {
	t.Helper()

	out, err := exec.Command("tmux", append([]string{"-L", socket}, args...)...).Output()
	require.NoError(t, err, "tmux %v", args)

	return strings.TrimSpace(string(out))
}
