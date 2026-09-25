//go:build integration

package cli

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"syscall"
	"testing"
	"time"

	"komrad/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationServeLoggerRejectsAnUnknownLevel covers a level the config carries that slog has no
// answer for: it fails rather than falling back, so a misspelled level is reported instead of
// quietly leaving the log at a verbosity nobody chose.
func TestIntegrationServeLoggerRejectsAnUnknownLevel(t *testing.T) {
	_, err := serveLogger("verbose")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "verbose")
}

// TestIntegrationShutdownSignalFollowsItsParent covers the context handed to serve still being the
// command's own, so a parent that goes away stops the server as a signal would.
func TestIntegrationShutdownSignalFollowsItsParent(t *testing.T) {
	parent, cancelParent := context.WithCancel(t.Context())

	ctx, stop := shutdownSignal(parent)
	defer stop()

	require.NoError(t, ctx.Err())

	cancelParent()

	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling the parent did not stop the server")
	}
}

// TestIntegrationShutdownSignalStopsOnASignal covers the wiring itself, that the signals a reload
// and a Ctrl-C actually send are the ones being listened for.
func TestIntegrationShutdownSignalStopsOnASignal(t *testing.T) {
	ctx, stop := shutdownSignal(t.Context())
	defer stop()

	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGTERM))

	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the signal did not stop the server")
	}
}

// TestIntegrationServeListensWhereEverythingElseTalks covers the address the service binds: every
// other command reads it out of KOMRAD_ADDR, so a serve that ignores the variable binds an
// address nothing is looking at and the panel goes on saying the server cannot be reached.
func TestIntegrationServeListensWhereEverythingElseTalks(t *testing.T) {
	tests := []struct {
		name       string
		flag       string
		configured string
		want       string
	}{
		{
			name:       "with no flag the configured address is what gets bound",
			flag:       "",
			configured: "127.0.0.1:9999",
			want:       "127.0.0.1:9999",
		},
		{
			name:       "a flag still wins, since it was typed for this run",
			flag:       "127.0.0.1:7000",
			configured: "127.0.0.1:9999",
			want:       "127.0.0.1:7000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, resolveServeAddr(tt.flag, tt.configured))
		})
	}
}

// TestIntegrationServeAddrFlagCarriesNoDefault covers the flag itself: a default spelled into it is
// indistinguishable from one the user typed, so the fallback above would never be reached.
func TestIntegrationServeAddrFlagCarriesNoDefault(t *testing.T) {
	flag := serveCmd(&app{env: &config.Environment{Addr: "127.0.0.1:9999"}}).Flags().Lookup("addr")

	require.NotNil(t, flag)
	assert.Empty(t, flag.DefValue, "a default here shadows KOMRAD_ADDR")
}

// TestIntegrationServeRefusesAnAddressTheFileWouldRefuse covers the flag.
func TestIntegrationServeRefusesAnAddressTheFileWouldRefuse(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		wantErr bool
	}{
		{
			name:    "a host on the network, which the api has no auth for",
			addr:    "0.0.0.0:8787",
			wantErr: true,
		},
		{
			name:    "a port no listener would take",
			addr:    "127.0.0.1:99999",
			wantErr: true,
		},
		{
			name: "a loopback host",
			addr: "127.0.0.1:8787",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := config.ValidateAddr(resolveServeAddr(tt.addr, "127.0.0.1:8787"))

			if tt.wantErr {
				require.Error(t, err, "the flag reaches the same rule the file does")

				return
			}

			require.NoError(t, err)
		})
	}
}

// TestIntegrationCloseWithinSaysWhichHandleHungRatherThanWaiting pins the timeout around shutdown.
func TestIntegrationCloseWithinSaysWhichHandleHungRatherThanWaiting(t *testing.T) {
	var logged bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logged, nil))

	closed := make(chan struct{})
	closeWithin(logger, "the pool", func() { close(closed) })

	select {
	case <-closed:
	default:
		t.Fatal("a handle that closes at once is closed")
	}
	assert.Empty(t, logged.String(), "and nothing is said about it")

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	closeWithin(logger, "the wedged pool", func() { <-release })

	assert.Contains(t, logged.String(), "did not close in time")
	assert.Contains(t, logged.String(), "the wedged pool", "the log names the handle that hung")
}

// TestIntegrationHookBinaryPathIsThisBuild pins that agents are launched with the hooks of the binary
// serving them.
func TestIntegrationHookBinaryPathIsThisBuild(t *testing.T) {
	path := getHookBinaryPath(t.Context())
	require.NotEmpty(t, path)

	self, err := os.Executable()
	require.NoError(t, err)
	assert.Equal(t, self, path)
}
