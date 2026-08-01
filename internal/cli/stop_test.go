package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStopHandlerRefusesAnythingButPost covers the route being reachable by anything that
// wanders over the address. A service that stops itself on a GET is one a link preview or
// a crawler can take down.
func TestStopHandlerRefusesAnythingButPost(t *testing.T) {
	requested := make(chan struct{}, 1)
	handler := newStopHandler(requested)

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(method, StopPath, nil))

			assert.Equal(t, http.StatusMethodNotAllowed, recorder.Code)
			assert.Equal(t, http.MethodPost, recorder.Header().Get("Allow"))
			assert.Empty(t, requested, "nothing was asked to stop")
		})
	}
}

// TestStopHandlerAnswersBeforeStopping covers the handler saying it heard rather than
// tearing down while the caller waits. Shutdown waits for the requests in flight and this
// is one of them, so a handler that stopped from in here would wait on itself.
//
// It answers with its own pid, since that is what the caller waits on. The listener closes
// at the top of the teardown, so an address that has stopped answering says only that the
// shutdown started, not that it finished.
func TestStopHandlerAnswersBeforeStopping(t *testing.T) {
	requested := make(chan struct{}, 1)
	recorder := httptest.NewRecorder()

	newStopHandler(requested).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, StopPath, nil))

	assert.Equal(t, http.StatusAccepted, recorder.Code)
	assert.Equal(t, strconv.Itoa(os.Getpid()), recorder.Body.String(), "it says which process to wait on")

	select {
	case <-requested:
	default:
		t.Fatal("the shutdown was never asked for")
	}
}

// TestStopHandlerDoesNotBlockOnASecondAsk covers an impatient caller asking twice. The
// first ask is already being acted on and nothing is reading the channel any more, so a
// second one has to be answered rather than left hanging on a send.
func TestStopHandlerDoesNotBlockOnASecondAsk(t *testing.T) {
	requested := make(chan struct{}, 1)
	handler := newStopHandler(requested)

	answered := make(chan int, 2)
	go func() {
		for range 2 {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, StopPath, nil))
			answered <- recorder.Code
		}
	}()

	for range 2 {
		select {
		case code := <-answered:
			assert.Equal(t, http.StatusAccepted, code)
		case <-time.After(5 * time.Second):
			t.Fatal("a second ask was left hanging")
		}
	}
}

// TestRequestStopOnNothing covers a stop aimed at an address nothing is serving. The work
// is already done, so a reload carries on rather than failing.
func TestRequestStopOnNothing(t *testing.T) {
	pid, err := requestStop(freeAddr(t))

	require.NoError(t, err)
	assert.Zero(t, pid, "there was nothing to stop")
}

// TestRequestStopReadsThePid covers the round trip against a server that answers the way
// serve does, since the pid is what the wait afterwards is built on.
func TestRequestStopReadsThePid(t *testing.T) {
	server := httptest.NewServer(newStopHandler(make(chan struct{}, 1)))
	defer server.Close()

	pid, err := requestStop(strippedHost(t, server.URL))

	require.NoError(t, err)
	assert.Equal(t, os.Getpid(), pid)
}

// TestRequestStopOnAServerWithoutTheRoute covers a serve too old to know how to stop. It
// is still running afterwards, so reporting success would have a reload respawn the panel
// against the process it meant to replace.
func TestRequestStopOnAServerWithoutTheRoute(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	_, err := requestStop(strippedHost(t, server.URL))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "would not stop")
}

// TestAwaitStoppedGivesUp covers a server that accepted the request and then carried on
// running. The command says so rather than returning as though the reload can proceed.
func TestAwaitStoppedGivesUp(t *testing.T) {
	err := awaitStopped(os.Getpid(), 300*time.Millisecond)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "still running")
}

// TestAwaitStoppedWaitsForTheProcessRatherThanTheAddress is the point of watching a pid.
// The listener closes first, so a server partway through draining is already refusing
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

// strippedHost is a test server's address without the scheme, which is the shape the
// commands take an address in.
func strippedHost(t *testing.T, url string) string {
	t.Helper()

	return url[len("http://"):]
}
