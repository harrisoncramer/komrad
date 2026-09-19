package cli

import (
	"net"
	"strconv"
	"testing"

	"weave/config"
	"weave/internal/caller"
	"weave/internal/engine"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDialAPIReachesARemoteEngineThroughItsForward covers which address a command talks to, since
// a remote engine's own address means something else on the machine the command runs on.
func TestDialAPIReachesARemoteEngineThroughItsForward(t *testing.T) {
	t.Parallel()

	const engineAddr = "127.0.0.1:8787"

	tests := []struct {
		name string
		ssh  string
	}{
		{
			name: "the engine is this machine, so the configured address is dialled",
			ssh:  "",
		},
		{
			name: "the engine is elsewhere, so the forward is dialled instead",
			ssh:  "weave@203.0.113.4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			state := &app{env: &config.Environment{Addr: engineAddr, SSH: tt.ssh}}

			want := engineAddr
			if tt.ssh != "" {
				// A forward already carrying the engine is reused rather than replaced, so a
				// listener on the port it always lands on stands in for ssh.
				want = listenOnTunnelPort(t, state.engine(), engineAddr)
			}

			addr, release, err := state.dialAPI(t.Context())
			require.NoError(t, err)

			t.Cleanup(release)

			assert.Equal(t, want, addr)
		})
	}
}

// listenOnTunnelPort answers on the port an engine's forward always lands on and returns it.
func listenOnTunnelPort(t *testing.T, remote engine.Remote, engineAddr string) string {
	t.Helper()

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(remote.TunnelPort(engineAddr)))

	listener, err := net.Listen("tcp", addr)
	require.NoError(t, err, "the port this engine's forward lands on is already taken")

	t.Cleanup(func() { _ = listener.Close() })

	return addr
}

// TestCallerIsDroppedForAnotherEngine covers an agent's own id travelling to its own panel and
// not to an engine that has no such agent.
func TestCallerIsDroppedForAnotherEngine(t *testing.T) {
	t.Parallel()

	local := &app{env: &config.Environment{AgentID: "a1", SessionID: "s1"}}
	assert.Equal(t, caller.Caller{AgentID: "a1", SessionID: "s1"}, local.caller())

	remote := &app{env: &config.Environment{
		AgentID:   "a1",
		SessionID: "s1",
		SSH:       "user@engine",
	}}
	assert.Equal(t, caller.Caller{}, remote.caller())
}
