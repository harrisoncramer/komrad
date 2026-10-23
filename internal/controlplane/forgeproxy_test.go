package controlplane_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"komrad/config"
	"komrad/internal/controlplanetest"
	"komrad/internal/forge/github"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// engineClient reaches GitHub the way the named engine does, through the forge proxy.
func (p *plane) engineClient(t *testing.T, engine engine) *http.Client {
	t.Helper()

	transport, err := github.NewForgeProxyTransport(github.NewForgeProxyTransportParams{
		ProxyURL: "http://" + engine.name + ":" + engine.credential + "@" + p.proxy.Listener.Addr().String(),
		CAFile:   p.ca.CertFile,
	})
	require.NoError(t, err)
	t.Cleanup(transport.CloseIdleConnections)

	return &http.Client{Transport: transport}
}

// TestTheForgeProxySendsEachEngineAsItsOwnPerson covers the placeholder an engine sends being
// swapped for its own person's login, in the form each GitHub host takes it.
func TestTheForgeProxySendsEachEngineAsItsOwnPerson(t *testing.T) {
	p := startPlane(t)

	tests := []struct {
		name              string
		engine            string
		url               string
		authorization     string
		wantStatus        int
		wantAuthorization string
		wantMessage       string
	}{
		{
			name:              "harry's engine reaches the api as harry",
			engine:            "harry",
			url:               "https://api.github.com/user",
			authorization:     "Bearer " + config.ForgeProxyPlaceholder,
			wantStatus:        http.StatusOK,
			wantAuthorization: "Bearer gho_harry",
		},
		{
			name:              "ada's engine reaches the api as ada",
			engine:            "ada",
			url:               "https://api.github.com/user",
			authorization:     "token " + config.ForgeProxyPlaceholder,
			wantStatus:        http.StatusOK,
			wantAuthorization: "Bearer gho_ada",
		},
		{
			name:              "git's basic auth on github.com carries the login as its password",
			engine:            "harry",
			url:               "https://github.com/user",
			authorization:     "Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+config.ForgeProxyPlaceholder)),
			wantStatus:        http.StatusOK,
			wantAuthorization: "Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:gho_harry")),
		},
		{
			name:              "a request with no credential of its own still goes as the engine's person",
			engine:            "ada",
			url:               "https://raw.githubusercontent.com/user",
			wantStatus:        http.StatusOK,
			wantAuthorization: "Bearer gho_ada",
		},
		{
			name:        "an engine whose person has not authorized GitHub hears how to",
			engine:      "grace",
			url:         "https://api.github.com/user",
			wantStatus:  http.StatusUnauthorized,
			wantMessage: "This engine has no GitHub login. Authorize GitHub from the panel, then try again.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := len(p.github.Requests())

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, tt.url, nil)
			require.NoError(t, err)
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}

			res, err := p.engineClient(t, p.engine(tt.engine)).Do(req)
			require.NoError(t, err)
			defer func() { _ = res.Body.Close() }()

			assert.Equal(t, tt.wantStatus, res.StatusCode)

			var body struct {
				Message string `json:"message"`
			}
			require.NoError(t, json.NewDecoder(res.Body).Decode(&body))
			assert.Equal(t, tt.wantMessage, body.Message)

			reached := p.github.Requests()[before:]
			if tt.wantAuthorization == "" {
				assert.Empty(t, reached, "nothing reaches GitHub")

				return
			}

			require.Len(t, reached, 1)
			host := strings.TrimPrefix(tt.url, "https://")
			host, _, _ = strings.Cut(host, "/")
			assert.Equal(t, controlplanetest.Request{Host: host, Path: "/user", Authorization: tt.wantAuthorization}, reached[0])
		})
	}
}

// TestTheForgeProxyRefusesWhatIsNotAnEngineReachingGitHub covers the requests turned away
// before any tunnel opens.
func TestTheForgeProxyRefusesWhatIsNotAnEngineReachingGitHub(t *testing.T) {
	p := startPlane(t)

	basic := func(user, password string) string {
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(p.engine(user).name+":"+password))
	}

	tests := []struct {
		name          string
		method        string
		target        string
		authorization string
		wantStatus    int
	}{
		{
			name:       "no forge proxy credential",
			method:     http.MethodConnect,
			target:     "api.github.com:443",
			wantStatus: http.StatusProxyAuthRequired,
		},
		{
			name:          "another engine's credential",
			method:        http.MethodConnect,
			target:        "api.github.com:443",
			authorization: basic("harry", p.engine("ada").credential),
			wantStatus:    http.StatusProxyAuthRequired,
		},
		{
			name:          "an engine the control plane does not serve",
			method:        http.MethodConnect,
			target:        "api.github.com:443",
			authorization: basic("mallory", p.engine("harry").credential),
			wantStatus:    http.StatusProxyAuthRequired,
		},
		{
			name:          "a plain request rather than a tunnel",
			method:        http.MethodGet,
			target:        "http://api.github.com/user",
			authorization: basic("harry", p.engine("harry").credential),
			wantStatus:    http.StatusMethodNotAllowed,
		},
		{
			name:          "a host that is not GitHub's",
			method:        http.MethodConnect,
			target:        "example.com:443",
			authorization: basic("harry", p.engine("harry").credential),
			wantStatus:    http.StatusForbidden,
		},
		{
			name:          "GitHub on a port other than https",
			method:        http.MethodConnect,
			target:        "api.github.com:80",
			authorization: basic("harry", p.engine("harry").credential),
			wantStatus:    http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.target, nil)
			if tt.authorization != "" {
				req.Header.Set("Proxy-Authorization", tt.authorization)
			}

			recorder := httptest.NewRecorder()
			p.proxy.Config.Handler.ServeHTTP(recorder, req)

			assert.Equal(t, tt.wantStatus, recorder.Code)
			if tt.wantStatus == http.StatusProxyAuthRequired {
				assert.NotEmpty(t, recorder.Header().Get("Proxy-Authenticate"))
			}
			assert.Empty(t, p.github.Requests(), "nothing reaches GitHub")
		})
	}
}
