package tmux

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServersRouting drives two real tmux servers, standing in for the user's and
// the manager's, and checks each call lands on the one that can answer it. Faking
// this would only assert the routing table against itself, where the thing worth
// proving is that a global option written through Servers is readable by a key
// binding on the user's server and a key bound through it is not.
func TestServersRouting(t *testing.T) {
	hostSocket := newTestServer(t, "host")
	managerSocket := newTestServer(t, "manager")

	manager := NewClient(managerSocket)
	host := NewHost(manager, "")
	servers := NewServers(manager, host)

	assert.Empty(t, servers.GetClientPath(""), "with no host recorded there is no client to locate")
	servers.SetGlobal("@probe", "1")
	assert.Empty(t, manager.ShowGlobal("@probe"), "an unrecorded host is not silently the manager")

	host.Record(socketPath(t, hostSocket))

	servers.SetGlobal("@weave_open", "1")
	assert.Equal(t, "1", tmuxRun(t, hostSocket, "show-option", "-gqv", "@weave_open"),
		"globals belong to the user's server, where the key binding reads them")
	assert.Empty(t, tmuxRun(t, managerSocket, "show-option", "-gqv", "@weave_open"),
		"and are not written to the manager's")
	assert.Equal(t, "1", servers.ShowGlobal("@weave_open"), "and are read back from the same place")

	servers.BindKey("g", "detach-client", "-s", "weave")
	assert.Contains(t, tmuxRun(t, managerSocket, "list-keys", "-T", "prefix"), "weave",
		"the key pressed inside the popup is bound on the manager's server")
	assert.NotContains(t, tmuxRun(t, hostSocket, "list-keys", "-T", "prefix"), "weave",
		"and not on the user's, whose binding opens the popup")

	assert.NotEmpty(t, servers.GetClientPath(""), "the recorded host answers where its client sits")
}

// TestHostFallsBackToThisServer covers reaching the user's server with weave's own server gone,
// which is what hiding the panel after that server died looks like. The socket a previous open
// recorded is unreadable then, and it is the only route the flag the key binding branches on is
// cleared through, so without a fallback that flag stays set and the panel key stops working.
func TestHostFallsBackToThisServer(t *testing.T) {
	hostSocket := newTestServer(t, "fallback-host")
	managerSocket := newTestServer(t, "fallback-manager")

	manager := NewClient(managerSocket)
	host := NewHost(manager, paneEnv(socketPath(t, hostSocket)))
	host.Record(socketPath(t, hostSocket))

	require.NoError(t, exec.Command("tmux", "-L", managerSocket, "kill-server").Run())

	NewServers(manager, host).SetGlobal("@weave_open", "0")
	assert.Equal(t, "0", tmuxRun(t, hostSocket, "show-option", "-gqv", "@weave_open"),
		"the flag the key binding reads is cleared with no recorded socket to be had")
}

// TestHostDoesNotFallBackToTheManager covers the panel's own process, whose TMUX names weave's
// own server rather than the user's. Falling back there would write the options the key binding
// reads onto the one server nothing reads them from, which is worse than not writing them.
func TestHostDoesNotFallBackToTheManager(t *testing.T) {
	managerSocket := newTestServer(t, "no-fallback")

	manager := NewClient(managerSocket)
	host := NewHost(manager, paneEnv(socketPath(t, managerSocket)))

	NewServers(manager, host).SetGlobal("@weave_open", "1")
	assert.Empty(t, tmuxRun(t, managerSocket, "show-option", "-gqv", "@weave_open"))
}

// TestClientAtPath checks a client addressed by socket path reaches the same server
// as one addressed by name. The user's server is only ever named by path, since that
// is what tmux hands the popup.
func TestClientAtPath(t *testing.T) {
	socket := newTestServer(t, "path")

	byName := NewClient(socket)
	byName.SetGlobal("@probe", "here")

	byPath := NewClientAtPath(socketPath(t, socket))
	assert.Equal(t, "here", byPath.ShowGlobal("@probe"))
}

// TestFromEnv covers reading the user's server and session out of the TMUX variable
// tmux sets in the popup. It is the only channel there is, so a value that is not the
// three fields tmux writes yields nothing rather than a socket path made of whatever
// was there.
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

func paneEnv(path string) string { return path + ",1,0" }

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
