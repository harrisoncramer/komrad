package controlplane_test

import (
	"errors"
	"net/http"
	"testing"

	"komrad/internal/controlplane"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
)

// TestStartingAndStoppingAnEngine covers who may start and stop an engine, and what they hear
// when its runner fails.
func TestStartingAndStoppingAnEngine(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		token       string
		runErr      error
		wantStatus  int
		wantMessage string
		wantStarted []string
		wantStopped []string
	}{
		{
			name:        "someone without the engine's grant",
			path:        controlplane.StartPath,
			token:       adaToken,
			wantStatus:  http.StatusForbidden,
			wantMessage: "nobody has granted this account access to the engine",
		},
		{
			name:        "a runner that fails",
			path:        controlplane.StartPath,
			token:       harryToken,
			runErr:      errors.New("the instance is out of capacity"),
			wantStatus:  http.StatusBadGateway,
			wantMessage: "the instance is out of capacity",
			wantStarted: []string{"grace"},
		},
		{
			name:        "starting an engine",
			path:        controlplane.StartPath,
			token:       harryToken,
			wantStatus:  http.StatusAccepted,
			wantMessage: "the engine is starting",
			wantStarted: []string{"grace"},
		},
		{
			name:        "stopping an engine",
			path:        controlplane.StopPath,
			token:       harryToken,
			wantStatus:  http.StatusAccepted,
			wantMessage: "the engine is stopping",
			wantStopped: []string{"grace"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := startPlane(t)
			p.runner.set(p.engine("grace").name, controlplane.EngineStopped, tt.runErr)

			var answer struct {
				Message string `json:"message"`
			}
			assert.Equal(t, tt.wantStatus, p.call(t, http.MethodPost, "/"+p.engine("grace").name+tt.path, tt.token, &answer))
			assert.Equal(t, tt.wantMessage, answer.Message)

			started, stopped := p.runner.calls()
			assert.ElementsMatch(t, p.names(tt.wantStarted), started)
			assert.ElementsMatch(t, p.names(tt.wantStopped), stopped)
		})
	}
}

// names are the engines the people's names stand for.
func (p *plane) names(people []string) []string {
	return xslice.Map(people, func(person string) string { return p.engine(person).name })
}
