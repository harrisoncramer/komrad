package controlplane_test

import (
	"errors"
	"net/http"
	"testing"

	"komrad/internal/controlplane"
	"komrad/internal/server"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTheControlPlaneSendsEachEnginesPathToTheEngine covers which requests under an engine's
// path reach the engine, and which the control plane answers itself.
func TestTheControlPlaneSendsEachEnginesPathToTheEngine(t *testing.T) {
	p := startPlane(t)
	p.runner.set(p.engine("harry").name, controlplane.EngineRunning, nil)

	t.Run("a call under harry's path reaches harry's engine without the path, carrying the token", func(t *testing.T) {
		var answer reached
		require.Equal(t, http.StatusOK, p.call(t, http.MethodPost, "/"+p.engine("harry").name+"/agent.v1.AgentService/ListAgents", harryToken, &answer))
		assert.Equal(t, reached{
			Path:          "/agent.v1.AgentService/ListAgents",
			Authorization: "Bearer " + harryToken,
			Proto:         "HTTP/2.0",
		}, answer)
	})

	t.Run("an engine the control plane does not serve", func(t *testing.T) {
		var answer struct {
			Message string `json:"message"`
		}
		require.Equal(t, http.StatusNotFound, p.call(t, http.MethodGet, "/"+p.engine("mallory").name+server.HealthPath, harryToken, &answer))
		assert.Equal(t, "the control plane has no engine by that name", answer.Message)
	})

	t.Run("the sign-in path answers the provider without reaching the engine", func(t *testing.T) {
		var answer struct {
			Issuer   string `json:"issuer"`
			ClientID string `json:"client_id"`
		}
		require.Equal(t, http.StatusOK, p.call(t, http.MethodGet, "/"+p.engine("harry").name+"/sign-in", "", &answer))
		assert.Equal(t, signIn.Issuer, answer.Issuer)
		assert.Equal(t, signIn.ClientID, answer.ClientID)
	})
}

// TestTheControlPlaneAnswersForAnEngineItCannotReach covers what a person hears from an engine
// that does not answer, by where its runner says it is.
func TestTheControlPlaneAnswersForAnEngineItCannotReach(t *testing.T) {
	tests := []struct {
		name        string
		state       controlplane.EngineState
		stateErr    error
		path        string
		token       string
		wantStatus  int
		wantHealth  string
		wantMessage string
	}{
		{
			name:       "a stopped engine's health",
			state:      controlplane.EngineStopped,
			path:       server.HealthPath,
			token:      harryToken,
			wantStatus: http.StatusServiceUnavailable,
			wantHealth: server.HealthStopped,
		},
		{
			name:       "a starting engine's health",
			state:      controlplane.EngineStarting,
			path:       server.HealthPath,
			token:      harryToken,
			wantStatus: http.StatusServiceUnavailable,
			wantHealth: server.HealthStarting,
		},
		{
			name:        "a call to a stopped engine",
			state:       controlplane.EngineStopped,
			path:        "/agent.v1.AgentService/ListAgents",
			token:       harryToken,
			wantStatus:  http.StatusServiceUnavailable,
			wantMessage: "the engine is stopped, so start it from the panel or with komrad engine start",
		},
		{
			name:        "a call to a starting engine",
			state:       controlplane.EngineStarting,
			path:        "/agent.v1.AgentService/ListAgents",
			token:       harryToken,
			wantStatus:  http.StatusServiceUnavailable,
			wantMessage: "the engine is starting, so try again in a moment",
		},
		{
			name:        "a stopped engine's health to someone without its grant",
			state:       controlplane.EngineStopped,
			path:        server.HealthPath,
			token:       adaToken,
			wantStatus:  http.StatusForbidden,
			wantMessage: "nobody has granted this account access to the engine",
		},
		{
			name:        "an engine its runner says is running",
			state:       controlplane.EngineRunning,
			path:        server.HealthPath,
			token:       harryToken,
			wantStatus:  http.StatusBadGateway,
			wantMessage: "the control plane failed to reach the engine",
		},
		{
			name:        "an engine that is stopping",
			state:       controlplane.EngineStopping,
			path:        server.HealthPath,
			token:       harryToken,
			wantStatus:  http.StatusBadGateway,
			wantMessage: "the control plane failed to reach the engine",
		},
		{
			name:        "an engine whose runner cannot say",
			stateErr:    errors.New("the instance would not describe itself"),
			path:        server.HealthPath,
			token:       harryToken,
			wantStatus:  http.StatusBadGateway,
			wantMessage: "the control plane failed to reach the engine",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := startPlane(t)
			p.runner.set(p.engine("ada").name, tt.state, tt.stateErr)

			var answer struct {
				Status  string `json:"status"`
				Message string `json:"message"`
			}
			assert.Equal(t, tt.wantStatus, p.call(t, http.MethodGet, "/"+p.engine("ada").name+tt.path, tt.token, &answer))
			assert.Equal(t, tt.wantHealth, answer.Status)
			assert.Equal(t, tt.wantMessage, answer.Message)
		})
	}
}

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
			name:        "someone without the engine's grant",
			path:        controlplane.StartPath,
			token:       adaToken,
			wantStatus:  http.StatusForbidden,
			wantMessage: "nobody has granted this account access to the engine",
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
