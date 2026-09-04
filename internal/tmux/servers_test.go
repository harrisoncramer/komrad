package tmux

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServersRouting drives two real tmux servers and checks everything the panel writes about
// the user's server stays on its own, which a fake could only assert against itself.
func TestServersRouting(t *testing.T) {
	hostSocket := newTestServer(t, "host")
	managerSocket := newTestServer(t, "manager")

	servers := NewServers(NewClient(managerSocket))

	assert.Empty(t, servers.GetHostSocket(), "before an open there is no user server to name")
	assert.Empty(t, servers.HostPath(), "and no directory a new agent could inherit")

	servers.SetGlobal("@probe", "1")
	assert.Equal(t, "1", tmuxRun(t, managerSocket, "show-option", "-gqv", "@probe"),
		"what the panel writes down stays on its own server, out of reach of a second user server")
	assert.Empty(t, tmuxRun(t, hostSocket, "show-option", "-gqv", "@probe"))

	servers.RecordHostSocket(socketPath(t, hostSocket))
	assert.Equal(t, socketPath(t, hostSocket), servers.GetHostSocket(),
		"the open command records the server it was pressed on for everything else to read")

	servers.BindKey("g", "detach-client", "-s", "weave")
	assert.Contains(t, tmuxRun(t, managerSocket, "list-keys", "-T", "prefix"), "weave",
		"the key pressed inside the popup is bound on the manager's server")
	assert.NotContains(t, tmuxRun(t, hostSocket, "list-keys", "-T", "prefix"), "weave",
		"and not on the user's, whose binding opens the popup")

	servers.RecordHostPath("/tmp/where-the-user-was")
	assert.Equal(t, "/tmp/where-the-user-was", servers.HostPath(),
		"the panel reads the directory back rather than asking a server it does not run")
	assert.Empty(t, tmuxRun(t, hostSocket, "show-option", "-gqv", hostPathOption),
		"and that record lives on the panel's own server too")
}

// TestClientAtPath checks a client addressed by socket path reaches the same server as one
// addressed by name, which matters because tmux hands the popup only a path.
func TestClientAtPath(t *testing.T) {
	socket := newTestServer(t, "path")

	byName := NewClient(socket)
	byName.SetGlobal("@probe", "here")

	byPath := NewClientAtPath(socketPath(t, socket))
	assert.Equal(t, "here", byPath.ShowGlobal("@probe"))
}

// TestFromEnv covers reading the user's server and session out of the TMUX variable tmux sets
// in the popup, where a value that is not the three fields tmux writes must yield nothing.
func TestFromEnv(t *testing.T) {
	tests := []struct {
		name        string
		value       string
		wantSocket  string
		wantSession string
	}{
		{
			name:  "an unset variable names no server",
			value: "",
		},
		{
			name:  "a value short of the fields tmux writes is not read",
			value: "/tmp/tmux-501/default,74333",
		},
		{
			name:       "a value missing its session still names the server",
			value:      "/tmp/tmux-501/default,74333,",
			wantSocket: "/tmp/tmux-501/default",
		},
		{
			name:        "the socket comes first and the session id last",
			value:       "/tmp/tmux-501/default,74333,0",
			wantSocket:  "/tmp/tmux-501/default",
			wantSession: "$0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantSocket, SocketFromEnv(tt.value))
			assert.Equal(t, tt.wantSession, SessionFromEnv(tt.value))
		})
	}
}

func newTestServer(t *testing.T, role string) string {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

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
