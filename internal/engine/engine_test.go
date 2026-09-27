package engine_test

import (
	"testing"
	"time"

	"komrad/internal/engine"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIsRemoteIsWhetherADestinationWasGiven covers the zero value meaning a local engine.
func TestIsRemoteIsWhetherADestinationWasGiven(t *testing.T) {
	t.Parallel()

	assert.False(t, engine.Remote{}.IsRemote())
	assert.False(t, engine.Remote{Destination: "  "}.IsRemote())
	assert.True(t, engine.Remote{Destination: "ubuntu@host"}.IsRemote())
}

// TestStringNamesTheEngine covers what a log line or an error says about where agents are.
func TestStringNamesTheEngine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		remote engine.Remote
		want   string
	}{
		{
			name:   "a local engine",
			remote: engine.Remote{},
			want:   "local",
		},
		{
			name: "an engine named by its destination",
			remote: engine.Remote{
				Destination: "ubuntu@host",
			},
			want: "ubuntu@host",
		},
		{
			name:   "an engine named by its forward",
			remote: ssmEngine(),
			want:   `aws ssm start-session --target i-0abc --document-name AWS-StartPortForwardingSession --parameters {"portNumber":["{{remote_port}}"],"localPortNumber":["{{local_port}}"]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.remote.String())
		})
	}
}

// TestTunnelArgvForwardsTheEnginesAddress covers the forward ssh is asked for.
func TestTunnelArgvForwardsTheEnginesAddress(t *testing.T) {
	t.Parallel()

	remote := engine.Remote{Destination: "komrad@host"}

	argv := remote.TunnelArgv("127.0.0.1:54321", "127.0.0.1:8787")

	assert.Equal(t, "ssh", argv[0])
	assert.Contains(t, argv, "-N")
	assert.Contains(t, argv, "127.0.0.1:54321:127.0.0.1:8787")
	assert.Equal(t, "komrad@host", argv[len(argv)-1])
}

// TestTunnelArgvFailsRatherThanOpeningAUselessSession covers ssh otherwise staying up with no
// forward when the port is taken, which would look like a working tunnel that carries nothing.
func TestTunnelArgvFailsRatherThanOpeningAUselessSession(t *testing.T) {
	t.Parallel()

	remote := engine.Remote{Destination: "komrad@host"}

	assert.Contains(t, remote.TunnelArgv("127.0.0.1:1", "127.0.0.1:8787"), "ExitOnForwardFailure=yes")
}

// TestDialRefusesALocalEngine covers the caller being told rather than an ssh to nowhere.
func TestDialRefusesALocalEngine(t *testing.T) {
	t.Parallel()

	tunnel, err := engine.Remote{}.Dial(t.Context(), "127.0.0.1:8787", time.Second)

	require.Error(t, err)
	assert.Nil(t, tunnel)
}

// TestClosingANilTunnelIsFine covers the panel's exit path running whether or not one was opened.
func TestClosingANilTunnelIsFine(t *testing.T) {
	t.Parallel()

	var tunnel *engine.Tunnel

	assert.NoError(t, tunnel.Close())
}

// TestTunnelPortIsStableForAnEngine covers a tunnel that outlived its panel being found again
// rather than leaked, which needs the port to be the same every time.
func TestTunnelPortIsStableForAnEngine(t *testing.T) {
	t.Parallel()

	// Two values rather than two calls on one.
	first := engine.Remote{Destination: "komrad@host"}
	second := engine.Remote{Destination: "komrad@host"}

	assert.Equal(t, first.TunnelPort("127.0.0.1:8787"), second.TunnelPort("127.0.0.1:8787"))
}

// TestTunnelPortDiffersBetweenEngines covers two engines driven from one machine not fighting
// over the same forward.
func TestTunnelPortDiffersBetweenEngines(t *testing.T) {
	t.Parallel()

	first := engine.Remote{Destination: "komrad@one"}.TunnelPort("127.0.0.1:8787")
	second := engine.Remote{Destination: "komrad@two"}.TunnelPort("127.0.0.1:8787")
	other := engine.Remote{Destination: "komrad@one"}.TunnelPort("127.0.0.1:9999")

	assert.NotEqual(t, first, second)
	assert.NotEqual(t, first, other)
	assert.NotEqual(t,
		ssmEngine().TunnelPort("127.0.0.1:8787"),
		engine.Remote{Forward: []string{"other", "forward"}}.TunnelPort("127.0.0.1:8787"))
}

// TestTunnelPortIsInTheChosenRange covers the forward staying out of the way of both privileged
// ports and the range the kernel hands out on its own.
func TestTunnelPortIsInTheChosenRange(t *testing.T) {
	t.Parallel()

	for _, destination := range []string{"a@b", "komrad@host", "someone@somewhere.example"} {
		port := engine.Remote{Destination: destination}.TunnelPort("127.0.0.1:8787")
		assert.GreaterOrEqual(t, port, 19000)
		assert.Less(t, port, 20000)
	}
}

// ssmEngine is an engine reached by a transport that is not ssh, written the way a user would
// configure AWS Session Manager, to show komrad needs to know nothing about it.
func ssmEngine() engine.Remote {
	return engine.Remote{
		Forward: []string{
			"aws", "ssm", "start-session", "--target", "i-0abc",
			"--document-name", "AWS-StartPortForwardingSession",
			"--parameters", `{"portNumber":["{{remote_port}}"],"localPortNumber":["{{local_port}}"]}`,
		},
	}
}

// TestATransportIsRemoteWithoutAnSshDestination covers a transport that needs no host, which is
// what lets the engine keep no public address.
func TestATransportIsRemoteWithoutAnSshDestination(t *testing.T) {
	t.Parallel()

	assert.True(t, ssmEngine().IsRemote())
}

// TestTunnelArgvFillsBothPorts covers the forward being told which two ends to join.
func TestTunnelArgvFillsBothPorts(t *testing.T) {
	t.Parallel()

	argv := ssmEngine().TunnelArgv("127.0.0.1:19123", "127.0.0.1:8787")
	parameters := argv[len(argv)-1]

	assert.Equal(t, []string{"aws", "ssm", "start-session"}, argv[:3])
	assert.Contains(t, parameters, `"portNumber":["8787"]`)
	assert.Contains(t, parameters, `"localPortNumber":["19123"]`)
}
