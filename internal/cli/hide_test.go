//go:build integration

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

// TestHideDetachesThePanelsClient covers hiding closing the popup by taking its client away
// while leaving the session up, which is what the next open comes back to.
func TestHideDetachesThePanelsClient(t *testing.T) {
	managerSocket := throwawaySocket(t)
	panelSession(t, managerSocket)
	viewer := attachViewer(t, managerSocket)

	state := hideState(managerSocket, "")

	require.NoError(t, hideCmd(state).RunE(nil, nil))
	require.NoError(t, viewer.Wait())

	assert.Empty(t, hostQuery(t, managerSocket, "list-clients", "-t", agents.ManagerSession, "-F", "#{client_name}"),
		"the popup keeps its client until hide takes it away")
	require.NoError(t, exec.Command("tmux", "-L", managerSocket, "has-session", "-t", agents.ManagerSession).Run(),
		"the session outlives the popup, so the next open lands on the view it left")
}

func hideState(socket, tmuxEnv string) *app {
	servers := tmux.NewClient(socket)

	return &app{
		env:   &config.Environment{TmuxSocket: socket, Tmux: tmuxEnv},
		tmux:  servers,
		popup: ui.NewPopupWindow(servers, agents.ManagerSession),
	}
}

func hostPaneEnv(t *testing.T, socket string) string {
	t.Helper()

	return hostQuery(t, socket, "display-message", "-p", "#{socket_path}") + ",1,0"
}

func hostQuery(t *testing.T, socket string, args ...string) string {
	t.Helper()

	out, err := exec.Command("tmux", append([]string{"-L", socket}, args...)...).Output()
	require.NoError(t, err, "tmux %v", args)

	return strings.TrimSpace(string(out))
}
