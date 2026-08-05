package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
