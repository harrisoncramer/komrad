package cli

import (
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"komrad/config"
	"komrad/internal/controlplane"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTheLocalRunnerSaysWhereEachEngineIs covers the state a control plane on this machine
// reads for an engine, while komrad start runs for it and after.
func TestTheLocalRunnerSaysWhereEachEngineIs(t *testing.T) {
	running := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(running.Close)

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	stopped := listener.Addr().String()
	require.NoError(t, listener.Close())

	dir := t.TempDir()
	release := filepath.Join(dir, "release")
	ran := filepath.Join(dir, "ran")
	self := filepath.Join(dir, "komrad")
	script := "#!/bin/sh\necho \"$@\" >> '" + ran + "'\nwhile [ ! -e '" + release + "' ]; do sleep 0.05; done\n"
	require.NoError(t, os.WriteFile(self, []byte(script), 0o700))

	runner := newLocalRunner(newLocalRunnerParams{
		Logger: slog.New(slog.DiscardHandler),
		Self:   self,
		Engines: map[string]config.ControlPlaneEngine{
			"harry": {Addr: running.Listener.Addr().String()},
			"ada":   {Addr: stopped},
		},
	})

	state := func(engine string) controlplane.EngineState {
		got, err := runner.State(t.Context(), engine)
		require.NoError(t, err)

		return got
	}

	assert.Equal(t, controlplane.EngineRunning, state("harry"), "anything answering at the address is a running engine")
	assert.Equal(t, controlplane.EngineStopped, state("ada"))

	require.NoError(t, runner.Start(t.Context(), "ada"))
	require.NoError(t, runner.Start(t.Context(), "ada"))
	assert.Equal(t, controlplane.EngineStarting, state("ada"))

	require.NoError(t, os.WriteFile(release, nil, 0o600))
	assert.Eventually(t, func() bool { return state("ada") == controlplane.EngineStopped }, 5*time.Second, 20*time.Millisecond,
		"once komrad start exits the address says where the engine is")

	recorded, err := os.ReadFile(ran) //nolint:gosec // the test's own file
	require.NoError(t, err)
	assert.Equal(t, "start --engine ada\n", string(recorded), "a second start while one runs runs nothing")

	require.NoError(t, os.Remove(release))
	require.NoError(t, runner.Stop(t.Context(), "harry"))
	assert.Equal(t, controlplane.EngineStopping, state("harry"))
	require.NoError(t, os.WriteFile(release, nil, 0o600))
	assert.Eventually(t, func() bool { return state("harry") == controlplane.EngineRunning }, 5*time.Second, 20*time.Millisecond)
}
