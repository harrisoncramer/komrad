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

// TestStopHandlerRefusesAnythingButPost keeps the route shut against a GET, which would let
// a link preview or a crawler take the service down.
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

// TestStopHandlerAnswersBeforeStopping covers a handler that would wait on itself, shutdown
// waiting for the requests in flight and this being one of them.
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

// TestStopHandlerDoesNotBlockOnASecondAsk covers an impatient caller asking twice, where
// nothing is reading the channel any more and a blocking send would hang.
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
