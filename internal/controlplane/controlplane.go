package controlplane

import (
	"crypto/tls"
	"log/slog"
	"net/http"
	"time"

	"komrad/internal/interceptors"
)

// GitHubURL is where GitHub's device flow endpoints live.
const GitHubURL = "https://github.com"

// NewParams is what the control plane serves engines and people with.
type NewParams struct {
	Logger *slog.Logger
	// Engines is each engine's forge proxy credential keyed by its name.
	Engines map[string]string
	CA      *tls.Certificate
	Logins  loginStore
	Grants  grantChecker
	// Verifier checks people's ID tokens against the identity provider every engine trusts.
	Verifier       interceptors.TokenVerifier
	GitHubClientID string
	// GitHub is where the device flow's endpoints live, GitHubURL outside tests.
	GitHub string
	// Client reaches GitHub's device flow endpoints.
	Client *http.Client
	// Upstream sends the forge proxy's requests to GitHub.
	Upstream http.RoundTripper
	Now      func() time.Time
}

// ControlPlane is the forge proxy and the control plane's own endpoints.
type ControlPlane struct {
	proxy   *forgeProxy
	handler http.Handler
}

// New starts the forge proxy's tunnels and builds the control plane's endpoints.
func New(params NewParams) *ControlPlane {
	proxy := newForgeProxy(newForgeProxyParams{
		Logger:   params.Logger,
		Engines:  params.Engines,
		Logins:   params.Logins,
		CA:       params.CA,
		Upstream: params.Upstream,
		Now:      params.Now,
	})

	endpoints := &handler{
		logger:   params.Logger,
		engines:  params.Engines,
		verifier: params.Verifier,
		grants:   params.Grants,
		device: newDeviceFlow(newDeviceFlowParams{
			Client:   params.Client,
			GitHub:   params.GitHub,
			ClientID: params.GitHubClientID,
			Logins:   params.Logins,
			Now:      params.Now,
		}),
	}

	return &ControlPlane{proxy: proxy, handler: endpoints.routes()}
}

// ForgeProxy is the handler engines reach the forge proxy on.
func (c *ControlPlane) ForgeProxy() http.Handler {
	return c.proxy
}

// Handler serves the control plane's own endpoints.
func (c *ControlPlane) Handler() http.Handler {
	return c.handler
}

// Close stops the forge proxy's open tunnels.
func (c *ControlPlane) Close() error {
	return c.proxy.Close()
}
