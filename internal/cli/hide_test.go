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

// TestHideClearsTheOpenFlagWithoutTheManagerServer covers the flag living on the user's
// server, where left set it makes every press take the hide branch and the panel unreachable.
func TestHideClearsTheOpenFlagWithoutTheManagerServer(t *testing.T) {
	hostSocket := throwawaySocket(t)
	out, err := exec.Command("tmux", "-L", hostSocket, "-f", "/dev/null",
		"new-session", "-d", "-s", "userland").CombinedOutput()
	require.NoError(t, err, "tmux: %s", out)
	require.NoError(t, exec.Command("tmux", "-L", hostSocket, "set-option", "-g", "@weave_open", "1").Run())

	state := hideState(throwawaySocket(t), hostPaneEnv(t, hostSocket))

	require.NoError(t, hideCmd(state).RunE(nil, nil))
	assert.Equal(t, "0", hostGlobal(t, hostSocket, "@weave_open"),
		"the panel key reads this flag, so a hide that leaves it set makes the key do nothing")
}

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
