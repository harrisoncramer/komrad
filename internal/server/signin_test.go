package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"komrad/config"
	"komrad/internal/engine"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPublicHandler pins what the network reaches without signing in: the provider to sign in
// with, and none of the routes only this machine may call.
func TestPublicHandler(t *testing.T) {
	type publicHandlerCase struct {
		name       string
		method     string
		path       string
		wantStatus int
	}

	tests := []publicHandlerCase{
		{
			name:       "a stop from the network",
			method:     http.MethodPost,
			path:       StopPath,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "the mcp endpoint from the network",
			method:     http.MethodPost,
			path:       "/mcp",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "an api call with no token",
			method:     http.MethodPost,
			path:       "/agents.v1.AgentService/ListAgents",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "health with no token",
			method:     http.MethodGet,
			path:       "/healthz",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "the provider to sign in with",
			method:     http.MethodGet,
			path:       engine.SignInPath,
			wantStatus: http.StatusOK,
		},
	}

	signIn := config.SignIn{
		Issuer:       "https://accounts.example.com",
		ClientID:     "komrad",
		ClientSecret: "secret",
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var reached bool
			handler := newPublicHandler(newPublicHandlerParams{
				Logger: slog.New(slog.DiscardHandler),
				SignIn: signIn,
				Mux: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
					reached = true
				}),
			})

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(tt.method, tt.path, nil))

			assert.Equal(t, tt.wantStatus, recorder.Code)
			assert.False(t, reached)

			if tt.path == engine.SignInPath {
				var provider engine.SignInProvider
				require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &provider))
				assert.Equal(t, engine.SignInProvider{
					Issuer:       signIn.Issuer,
					ClientID:     signIn.ClientID,
					ClientSecret: signIn.ClientSecret,
				}, provider)
			}
		})
	}
}
