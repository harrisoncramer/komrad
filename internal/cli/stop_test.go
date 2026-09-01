package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRequestStopOnNothing covers a stop aimed at an address nothing is serving: the work
// is already done, so a reload carries on rather than failing.
func TestRequestStopOnNothing(t *testing.T) {
	pid, err := requestStop(t.Context(), freeAddr(t))

	require.NoError(t, err)
	assert.Zero(t, pid, "there was nothing to stop")
}

// TestRequestStopReadsThePid covers the round trip against a server that answers the way
// serve does, since the pid is what the wait afterwards is built on.
func TestRequestStopReadsThePid(t *testing.T) {
	addr := fakeServer(t, fakeServerParams{stopPid: os.Getpid()})

	pid, err := requestStop(t.Context(), addr)

	require.NoError(t, err)
	assert.Equal(t, os.Getpid(), pid)
}

// TestRequestStopOnAServerWithoutTheRoute covers a serve too old to know how to stop: it
// is still running afterwards, so reporting success would have a reload respawn the panel
// against the process it meant to replace.
func TestRequestStopOnAServerWithoutTheRoute(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	_, err := requestStop(t.Context(), strippedHost(t, server.URL))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "would not stop")
}

// TestAwaitStoppedGivesUp covers a server that accepted the request and then carried on
// running: the command says so rather than returning as though the reload can proceed.
func TestAwaitStoppedGivesUp(t *testing.T) {
	err := awaitStopped(os.Getpid(), 300*time.Millisecond)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "still running")
}

// TestAwaitStoppedWaitsForTheProcessRatherThanTheAddress is the point of watching a pid: the
// listener closes first, so a server partway through draining is already refusing
// connections while its process is very much still there.
func TestAwaitStoppedWaitsForTheProcessRatherThanTheAddress(t *testing.T) {
	sleeper := exec.Command("sleep", "30")
	require.NoError(t, sleeper.Start())

	pid := sleeper.Process.Pid
	t.Cleanup(func() { _ = sleeper.Process.Kill() })

	require.Error(t, awaitStopped(pid, 200*time.Millisecond), "a live process is waited on")

	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = sleeper.Process.Kill()
		_ = sleeper.Wait()
	}()

	require.NoError(t, awaitStopped(pid, 10*time.Second), "and the wait ends when it goes")
}

func strippedHost(t *testing.T, url string) string {
	t.Helper()

	return url[len("http://"):]
}
