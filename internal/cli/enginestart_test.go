package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"komrad/internal/server"
	"komrad/lib/clock/clocktest"
	"komrad/lib/xhttp"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// answer is one answer the control plane gives the engine's health path.
type answer struct {
	status int
	health string
}

// fakeControlPlane is a control plane for harry's engine that takes a start or a stop, then
// answers the engine's health path with each answer in turn, holding on the last.
type fakeControlPlane struct {
	mu      sync.Mutex
	runErr  string
	answers []answer
	posted  []string
}

func (f *fakeControlPlane) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodPost {
		f.posted = append(f.posted, r.URL.Path)
		if f.runErr != "" {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": f.runErr}) //nolint:errchkjson // the test hears a cut answer as a failed request

			return
		}

		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "the engine is starting"}) //nolint:errchkjson // the test hears a cut answer as a failed request

		return
	}

	next := f.answers[0]
	if len(f.answers) > 1 {
		f.answers = f.answers[1:]
	}
	w.WriteHeader(next.status)
	_ = json.NewEncoder(w).Encode(server.Health{Status: next.health}) //nolint:errchkjson // the test hears a cut answer as a failed request
}

// TestStartingAndStoppingAnEngineBehindAControlPlane covers komrad engine start and komrad engine
// stop, from the request to the control plane to the answer that says the engine got there.
func TestStartingAndStoppingAnEngineBehindAControlPlane(t *testing.T) {
	tests := []struct {
		name       string
		asked      startOrStop
		runErr     string
		answers    []answer
		wantErr    string
		wantOut    string
		wantSleeps int32
	}{
		{
			name:  "an engine that starts",
			asked: engineStart,
			answers: []answer{
				{status: http.StatusServiceUnavailable, health: server.HealthStopped},
				{status: http.StatusServiceUnavailable, health: server.HealthStarting},
				{status: http.StatusOK, health: "ok"},
			},
			wantOut:    "The engine is running.\n",
			wantSleeps: 2,
		},
		{
			name:  "an engine whose database is down has not started",
			asked: engineStart,
			answers: []answer{
				{status: http.StatusServiceUnavailable, health: "unavailable"},
				{status: http.StatusOK, health: "ok"},
			},
			wantOut:    "The engine is running.\n",
			wantSleeps: 1,
		},
		{
			name:  "an engine that stops",
			asked: engineStop,
			answers: []answer{
				{status: http.StatusOK, health: "ok"},
				{status: http.StatusBadGateway},
				{status: http.StatusServiceUnavailable, health: server.HealthStopped},
			},
			wantOut:    "The engine is stopped.\n",
			wantSleeps: 2,
		},
		{
			name:  "an engine that never starts",
			asked: engineStart,
			answers: []answer{
				{status: http.StatusServiceUnavailable, health: server.HealthStarting},
			},
			wantErr:    "the engine did not start within five minutes",
			wantSleeps: int32(engineStartOrStopWait / engineStartOrStopPoll),
		},
		{
			name:    "a control plane that fails to start the engine",
			asked:   engineStart,
			runErr:  "the instance is out of capacity",
			wantErr: "the instance is out of capacity",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plane := &fakeControlPlane{runErr: tt.runErr, answers: tt.answers}
			served := httptest.NewServer(plane)
			t.Cleanup(served.Close)

			var out bytes.Buffer
			sleeper := &clocktest.Sleeper{}
			err := startOrStopEngine(t.Context(), startOrStopEngineParams{
				URL:       served.URL + "/harry",
				Transport: xhttp.NewTransport(),
				Sleeper:   sleeper,
				Asked:     tt.asked,
				Out:       &out,
			})
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantOut, out.String())
			assert.Equal(t, tt.wantSleeps, sleeper.CountSleeps())
			assert.Equal(t, []string{"/harry" + tt.asked.Path}, plane.posted)
		})
	}
}
