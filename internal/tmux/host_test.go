//go:build integration

package tmux

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationHostRecord drives two real tmux servers and checks everything the panel writes about the
// user's server stays on its own, which a fake could only assert against itself.
func TestIntegrationHostRecord(t *testing.T) {
	hostSocket := newTestServer(t, "host")
	managerSocket := newTestServer(t, "manager")

	manager := NewClient(managerSocket)

	assert.Empty(t, manager.GetHostSocket(), "before an open there is no user server to name")
	assert.Empty(t, manager.GetHostPath(), "and no directory a new agent could inherit")

	manager.RecordHostSocket(socketPath(t, hostSocket))
	assert.Equal(t, socketPath(t, hostSocket), manager.GetHostSocket(),
		"the open command records the server it was pressed on for everything else to read")
	assert.Empty(t, tmuxRun(t, hostSocket, "show-option", "-gqv", hostSocketOption),
		"and records it on its own server, out of reach of a second user server")

	manager.RecordHostPath("/tmp/where-the-user-was")
	assert.Equal(t, "/tmp/where-the-user-was", manager.GetHostPath(),
		"the panel reads the directory back rather than asking a server it does not run")

	manager.RecordHostPath("")
	assert.Empty(t, manager.GetHostPath(),
		"and an open with nowhere to read forgets the last answer rather than repeating it")
}

// TestIntegrationClientAtPath checks a client addressed by socket path reaches the same server as one
// addressed by name, which matters because tmux hands the popup only a path.
func TestIntegrationClientAtPath(t *testing.T) {
	socket := newTestServer(t, "path")

	byName := NewClient(socket)
	byName.SetGlobal("@probe", "here")

	byPath := NewClientAtPath(socketPath(t, socket))
	assert.Equal(t, "here", byPath.ShowGlobal("@probe"))
}

func newTestServer(t *testing.T, role string) string {
	t.Helper()
	_, err := exec.LookPath("tmux")
	require.NoError(t, err)

	socket := "cca-" + role + "-" + filepath.Base(t.TempDir())
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", socket, "kill-server").Run() })

	cmd := exec.Command("tmux", "-L", socket, "-f", "/dev/null", "new-session", "-d", "-s", role, "-c", t.TempDir())
	require.NoError(t, cmd.Run(), "failed to start the %s tmux server", role)

	return socket
}

func socketPath(t *testing.T, socket string) string {
	t.Helper()

	return tmuxRun(t, socket, "display-message", "-p", "#{socket_path}")
}

func tmuxRun(t *testing.T, socket string, args ...string) string {
	t.Helper()

	out, err := exec.Command("tmux", append([]string{"-L", socket}, args...)...).Output()
	require.NoError(t, err, "tmux %v", args)

	return strings.TrimSpace(string(out))
}
