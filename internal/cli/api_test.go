package cli

import (
	"testing"

	"komrad/config"
	"komrad/internal/caller"

	"github.com/stretchr/testify/assert"
)

// TestAPIBaseIsWhereTheEngineIsServed covers which address a command talks to.
func TestAPIBaseIsWhereTheEngineIsServed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "the engine is this machine, so the configured address is dialled",
			url:  "",
			want: "http://127.0.0.1:8787",
		},
		{
			name: "the engine is served at a url, so the url is dialled",
			url:  "https://komrad.example.com",
			want: "https://komrad.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			state := &app{env: &config.Environment{
				Addr: "127.0.0.1:8787",
				URL:  tt.url,
			}}

			assert.Equal(t, tt.want, state.apiBase())
		})
	}
}

// TestCallerIsDroppedForAnotherEngine covers an agent's own id travelling to its own panel and
// not to an engine that has no such agent.
func TestCallerIsDroppedForAnotherEngine(t *testing.T) {
	t.Parallel()

	local := &app{env: &config.Environment{AgentID: "a1", SessionID: "s1"}}
	assert.Equal(t, caller.Caller{AgentID: "a1", SessionID: "s1"}, local.caller())

	remote := &app{env: &config.Environment{
		AgentID:   "a1",
		SessionID: "s1",
		URL:       "https://komrad.example.com",
	}}
	assert.Equal(t, caller.Caller{}, remote.caller())
}
