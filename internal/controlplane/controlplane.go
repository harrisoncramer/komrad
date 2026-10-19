package controlplane

import (
	"crypto/tls"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"komrad/config"
	"komrad/internal/engine"
	"komrad/internal/interceptors"
)

// GitHubURL is where GitHub's device flow endpoints live.
const GitHubURL = "https://github.com"

// controlPlane is the forge proxy and the control plane's own endpoints.
type controlPlane struct {
	proxy   *forgeProxy
	handler http.Handler
}

// NewParams is what the control plane serves engines and people with.
type NewParams struct {
	Logger *slog.Logger
	// Engines is each engine's address and forge proxy credential keyed by its name.
	Engines map[string]config.ControlPlaneEngine
	// SignIn is the provider every engine trusts, which the control plane answers each
	// engine's sign-in path with.
	SignIn config.SignIn
	CA     *tls.Certificate
	Logins loginStore
	Grants grantChecker
	// Verifier checks people's ID tokens against the identity provider every engine trusts.
	Verifier interceptors.TokenVerifier
	// Runner starts and stops the engines.
	Runner         Runner
	GitHubClientID string
	// GitHub is where the device flow's endpoints live, GitHubURL outside tests.
	GitHub string
	// GitHubAPI is where GitHub's api lives, GitHubAPIURL outside tests.
	GitHubAPI string
	// Client reaches GitHub's device flow endpoints and its api.
	Client *http.Client
	// Upstream sends the forge proxy's requests to GitHub.
	Upstream http.RoundTripper
	// Engine reaches the engines, NewEngineTransport outside tests.
	Engine http.RoundTripper
	Now    func() time.Time
}

// New starts the forge proxy's tunnels and builds the control plane's endpoints.
func New(params NewParams) *controlPlane {
	credentials := make(map[string]string, len(params.Engines))
	for name, served := range params.Engines {
		credentials[name] = served.Credential
	}

	proxy := newForgeProxy(newForgeProxyParams{
		Logger:   params.Logger,
		Engines:  credentials,
		Logins:   params.Logins,
		CA:       params.CA,
		Upstream: params.Upstream,
		Now:      params.Now,
	})

	endpoints := &handler{
		logger:    params.Logger,
		engines:   params.Engines,
		signIn: engine.SignInProvider{
			Issuer:       params.SignIn.Issuer,
			ClientID:     params.SignIn.ClientID,
			ClientSecret: params.SignIn.ClientSecret,
		},
		verifier:  params.Verifier,
		grants:    params.Grants,
		runner:    params.Runner,
		logins:    params.Logins,
		client:    params.Client,
		githubAPI: strings.TrimSuffix(params.GitHubAPI, "/"),
		device: newDeviceFlow(newDeviceFlowParams{
			Client:   params.Client,
			GitHub:   params.GitHub,
			ClientID: params.GitHubClientID,
			Logins:   params.Logins,
			Now:      params.Now,
		}),
	}
	endpoints.proxy = endpoints.newEngineProxy(params.Engine)

	return &controlPlane{proxy: proxy, handler: endpoints.routes()}
}

// ForgeProxy is the handler engines reach the forge proxy on.
func (c *controlPlane) ForgeProxy() http.Handler {
	return c.proxy
}

// Handler serves the control plane's own endpoints and each engine's path.
func (c *controlPlane) Handler() http.Handler {
	return c.handler
}

// Close stops the forge proxy's open tunnels.
func (c *controlPlane) Close() error {
	return c.proxy.Close()
}
