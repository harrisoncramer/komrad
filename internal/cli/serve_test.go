package cli

import (
	"context"
	"syscall"
	"testing"
	"time"

	"weave/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServeLoggerRejectsAnUnknownLevel covers a level the config carries that slog has no
// answer for. It fails rather than falling back, so a misspelled level is reported instead
// of quietly leaving the log at a verbosity nobody chose.
func TestServeLoggerRejectsAnUnknownLevel(t *testing.T) {
	_, err := serveLogger("verbose")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "verbose")
}

// TestShutdownSignalFollowsItsParent covers the context handed to serve still being the
// command's own, so a parent that goes away stops the server as a signal would.
func TestShutdownSignalFollowsItsParent(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())

	ctx, stop := shutdownSignal(parent)
	defer stop()

	assert.NoError(t, ctx.Err())

	cancelParent()

	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling the parent did not stop the server")
	}
}

// TestShutdownSignalStopsOnASignal covers the wiring itself, that the signals a reload
// and a Ctrl-C actually send are the ones being listened for.
func TestShutdownSignalStopsOnASignal(t *testing.T) {
	ctx, stop := shutdownSignal(context.Background())
	defer stop()

	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGTERM))

	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the signal did not stop the server")
	}
}

// TestServeListensWhereEverythingElseTalks covers the address the service binds. Every
// other command reads it out of WEAVE_ADDR, and the panel tells anyone whose server is
// down to run "weave serve", so a serve that ignores the variable binds an address nothing
// is looking at and the panel goes on saying the server cannot be reached.
func TestServeListensWhereEverythingElseTalks(t *testing.T) {
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

// TestServeAddrFlagCarriesNoDefault covers the flag itself. A default spelled into it is
// indistinguishable from one the user typed, so the fallback above would never be reached.
func TestServeAddrFlagCarriesNoDefault(t *testing.T) {
	flag := serveCmd(&app{env: &config.Environment{Addr: "127.0.0.1:9999"}}).Flags().Lookup("addr")

	require.NotNil(t, flag)
	assert.Empty(t, flag.DefValue, "a default here shadows WEAVE_ADDR")
}
