package engine_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"weave/internal/engine"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// forwardArg is what this test binary is run with to stand in for ssh: it listens on the local
// port, answers one connection the way the engine would, and exits, which is a forward that
// drops.
const forwardArg = "stand-in-forward"

// TestMain runs the stand-in forward when asked to, and the tests otherwise.
func TestMain(m *testing.M) {
	if len(os.Args) > 2 && os.Args[1] == forwardArg {
		listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", os.Args[2]))
		if err != nil {
			os.Exit(1)
		}

		connection, err := listener.Accept()
		if err != nil {
			os.Exit(1)
		}

		_, _ = connection.Write([]byte("HTTP/1.0 404 Not Found\r\n\r\n"))
		_ = connection.Close()

		os.Exit(0)
	}

	os.Exit(m.Run())
}

// droppingEngine is an engine whose forward is the stand-in above, on a port of its own so the
// tests never meet each other on it.
func droppingEngine(t *testing.T) (engine.Remote, string) {
	t.Helper()

	remote := engine.Remote{
		Destination: "weave@" + t.Name(),
		Forward:     []string{os.Args[0], forwardArg, engine.LocalPortPlaceholder},
	}

	engineAddr := "127.0.0.1:8787"
	local := net.JoinHostPort("127.0.0.1", strconv.Itoa(remote.TunnelPort(engineAddr)))

	return remote, local
}

func isReachable(address string) bool {
	connection, err := net.DialTimeout("tcp", address, 200*time.Millisecond)
	if err != nil {
		return false
	}

	_ = connection.Close()

	return true
}

// TestDialRunsTheForwardAgainWhenItDrops covers an ssh that exits under the panel, which before
// left the panel dialling a closed port for the rest of its life.
func TestDialRunsTheForwardAgainWhenItDrops(t *testing.T) {
	remote, local := droppingEngine(t)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	tunnel, err := remote.Dial(ctx, "127.0.0.1:8787", 5*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tunnel.Close() })

	assert.Equal(t, local, tunnel.Local)

	assert.Eventually(t, func() bool { return isReachable(local) }, 5*time.Second, 50*time.Millisecond,
		"the forward Dial waited on took its one connection and went, so a second one is up")

	require.NoError(t, tunnel.Close())

	assert.Eventually(t, func() bool { return !isReachable(local) }, 5*time.Second, 50*time.Millisecond)
	assert.Never(t, func() bool { return isReachable(local) }, 1500*time.Millisecond, 100*time.Millisecond,
		"a closed tunnel runs nothing again")
}

// TestDialStopsRedialingWhenThePanelGoes covers the context the panel dialled with ending the
// keeper the way Close does.
func TestDialStopsRedialingWhenThePanelGoes(t *testing.T) {
	remote, local := droppingEngine(t)

	ctx, cancel := context.WithCancel(t.Context())

	tunnel, err := remote.Dial(ctx, "127.0.0.1:8787", 5*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tunnel.Close() })

	cancel()

	assert.Eventually(t, func() bool { return !isReachable(local) }, 5*time.Second, 50*time.Millisecond)
	assert.Never(t, func() bool { return isReachable(local) }, 1500*time.Millisecond, 100*time.Millisecond)
}

// TestDialBorrowsOnlyAForwardThatAnswers covers a port held by a forward whose far side has gone:
// it accepts the connection and says nothing, and before it was taken for a working tunnel.
func TestDialBorrowsOnlyAForwardThatAnswers(t *testing.T) {
	tests := []struct {
		name     string
		serve    func(net.Listener)
		borrowed bool
	}{
		{
			name: "a forward that accepts and hangs",
			serve: func(listener net.Listener) {
				for {
					connection, err := listener.Accept()
					if err != nil {
						return
					}

					defer func() { _ = connection.Close() }()
				}
			},
			borrowed: false,
		},
		{
			name: "a forward that accepts and closes",
			serve: func(listener net.Listener) {
				for {
					connection, err := listener.Accept()
					if err != nil {
						return
					}

					_ = connection.Close()
				}
			},
			borrowed: false,
		},
		{
			name: "the engine's api",
			serve: func(listener net.Listener) {
				server := httptest.NewUnstartedServer(http.NotFoundHandler())
				server.Listener = listener
				server.Start()
			},
			borrowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remote, local := droppingEngine(t)

			listener, err := net.Listen("tcp", local)
			require.NoError(t, err)
			t.Cleanup(func() { _ = listener.Close() })

			go tt.serve(listener)

			tunnel, err := remote.Dial(t.Context(), "127.0.0.1:8787", time.Second)

			if !tt.borrowed {
				require.Error(t, err, "the stand-in cannot take the port either, so nothing carries the engine")
				assert.Nil(t, tunnel)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, local, tunnel.Local)
			require.NoError(t, tunnel.Close())
			assert.True(t, isReachable(local), "closing a borrowed tunnel leaves it to whoever owns it")
		})
	}
}

// TestDialTakesOverABorrowedForwardThatStopsAnswering covers the panel key replacing a panel
// whose ssh is still dying when the next panel dials, which before left the next panel on a
// port nothing would ever carry again.
func TestDialTakesOverABorrowedForwardThatStopsAnswering(t *testing.T) {
	remote, local := droppingEngine(t)

	listener, err := net.Listen("tcp", local)
	require.NoError(t, err)

	server := httptest.NewUnstartedServer(http.NotFoundHandler())
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)

	tunnel, err := remote.Dial(t.Context(), "127.0.0.1:8787", 5*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tunnel.Close() })

	server.Close()
	require.False(t, isReachable(local), "the borrowed forward has gone")

	assert.Eventually(t, func() bool { return isReachable(local) }, 15*time.Second, 100*time.Millisecond,
		"the tunnel runs a forward of its own once the borrowed one stops answering")
}
