//go:build integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"weave/internal/agents"
	"weave/internal/tmux"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	waitForClient = 3 * time.Second
	pollForClient = 20 * time.Millisecond
)

// TestShownAnswersFromWhatIsOnScreen drives real tmux servers, with a client attached on one and
// the record of the press on another.
func TestShownAnswersFromWhatIsOnScreen(t *testing.T) {
	managerSocket := throwawaySocket(t)
	panelSession(t, managerSocket)

	hostSocket := throwawaySocket(t)
	require.NoError(t, exec.Command("tmux", "-L", hostSocket, "-f", "/dev/null",
		"new-session", "-d", "-s", "userland").Run())

	state := hideState(t, managerSocket, hostPaneEnv(t, hostSocket))
	state.tmux.RecordHostSocket(hostQuery(t, hostSocket, "display-message", "-p", "#{socket_path}"))
	shown := shownCmd(state)

	require.ErrorIs(t, shown.RunE(nil, nil), ErrPanelHidden,
		"the panel is running with nobody looking at it, so the press has a popup to open")

	viewer := attachViewer(t, managerSocket)
	require.NoError(t, shown.RunE(nil, nil), "a client is attached and this is the server that opened it")

	state.tmux.RecordHostSocket("/tmp/some-other-tmux-server")
	require.ErrorIs(t, shown.RunE(nil, nil), ErrPanelHidden,
		"the panel is on screen somewhere else, so the press opens it here rather than hiding it there")

	require.NoError(t, viewer.Process.Kill())
	_ = viewer.Wait()
}

// TestShownWithoutATmuxServer covers the key binding running somewhere weave cannot work out
// which server pressed it, where saying hidden is what leaves the press with a popup.
func TestShownWithoutATmuxServer(t *testing.T) {
	managerSocket := throwawaySocket(t)
	panelSession(t, managerSocket)
	attachViewer(t, managerSocket)

	state := hideState(t, managerSocket, "")

	require.ErrorIs(t, shownCmd(state).RunE(nil, nil), ErrPanelHidden)
}

func panelSession(t *testing.T, socket string) {
	t.Helper()

	out, err := exec.Command("tmux", "-L", socket, "-f", "/dev/null",
		"new-session", "-d", "-s", agents.ManagerSession).CombinedOutput()
	require.NoError(t, err, "tmux: %s", out)
}

// attachViewer attaches a client to the panel's session the way the popup does, in tmux's
// control mode so a test needs no terminal of its own to hold it open.
func attachViewer(t *testing.T, socket string) *exec.Cmd {
	t.Helper()

	viewer := exec.Command("tmux", "-L", socket, "-C", "attach", "-t", agents.ManagerSession)
	stdin, err := viewer.StdinPipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = viewer.Process.Kill()
		_ = viewer.Wait()
	})

	require.NoError(t, viewer.Start())
	require.Eventually(t, func() bool {
		out, err := exec.Command("tmux", "-L", socket, "list-clients",
			"-t", agents.ManagerSession, "-F", "#{client_name}").Output()

		return err == nil && len(out) > 0
	}, waitForClient, pollForClient, "no client attached to the panel's session")

	return viewer
}

// TestShownAndHideReachAnEnginesPanel covers the one key finding the engine's panel on screen
// rather than only this machine's, and hiding that one.
func TestShownAndHideReachAnEnginesPanel(t *testing.T) {
	localSocket := throwawaySocket(t)
	panelSession(t, localSocket)

	engineSocket := throwawaySocket(t)
	panelSession(t, engineSocket)

	hostSocket := throwawaySocket(t)
	require.NoError(t, exec.Command("tmux", "-L", hostSocket, "-f", "/dev/null",
		"new-session", "-d", "-s", "userland").Run())
	hostPath := hostQuery(t, hostSocket, "display-message", "-p", "#{socket_path}")

	state := hideState(t, localSocket, hostPaneEnv(t, hostSocket))
	for _, name := range []string{"WEAVE_CONFIG", "WEAVE_ADDR", "WEAVE_TMUX_SOCKET", "WEAVE_CACHE_DIR", "WEAVE_DATABASE_URL", "WEAVE_READONLY_DATABASE_URL"} {
		t.Setenv(name, "")
	}

	engines := filepath.Join(os.Getenv("HOME"), ".config", "weave", "engines")
	require.NoError(t, os.MkdirAll(engines, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(engines, "remote.toml"),
		[]byte("[connection]\ntmux_socket = \""+engineSocket+"\"\n"), 0o600))

	engineManager := tmux.NewClient(engineSocket)
	engineManager.RecordHostSocket(hostPath)

	require.ErrorIs(t, shownCmd(state).RunE(nil, nil), ErrPanelHidden, "nothing is on screen yet")

	viewer := attachViewer(t, engineSocket)
	require.NoError(t, shownCmd(state).RunE(nil, nil), "the engine's panel is on screen from this server")

	require.NoError(t, hideCmd(state).RunE(nil, nil))
	require.NoError(t, viewer.Wait())
	assert.Empty(t, hostQuery(t, engineSocket, "list-clients", "-t", agents.ManagerSession, "-F", "#{client_name}"),
		"hide took the engine panel's client away")
	require.NoError(t, exec.Command("tmux", "-L", localSocket, "has-session", "-t", agents.ManagerSession).Run())
}
