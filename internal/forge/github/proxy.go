package github

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"

	"komrad/internal/forge"
	"komrad/lib/xhttp"
)

// ForgeProxyHosts are GitHub's hosts that an engine sends through the forge proxy.
var ForgeProxyHosts = []string{
	"github.com",
	"api.github.com",
	"uploads.github.com",
	"codeload.github.com",
	"raw.githubusercontent.com",
}

// NewForgeProxyTransportParams names the forge proxy and the CA its certificates are signed with.
type NewForgeProxyTransportParams struct {
	// ProxyURL carries the engine's name and forge proxy credential as its user and password.
	ProxyURL string
	CAFile   string
}

// NewForgeProxyTransport sends requests for GitHub's hosts through the forge proxy and every
// other request the way the environment says.
func NewForgeProxyTransport(params NewForgeProxyTransportParams) (*http.Transport, error) {
	proxy, err := url.Parse(params.ProxyURL)
	if err != nil {
		return nil, fmt.Errorf("failed to read the forge proxy's url: %w", errors.Unwrap(err))
	}

	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("failed to read the system's CA certificates: %w", err)
	}

	pem, err := os.ReadFile(params.CAFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read the control plane's CA certificate: %w", err)
	}
	if !roots.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("%s holds no PEM certificate", params.CAFile)
	}

	transport := xhttp.NewTransport()
	transport.Proxy = func(r *http.Request) (*url.URL, error) {
		if r.URL.Scheme == "https" && slices.Contains(ForgeProxyHosts, strings.ToLower(r.URL.Hostname())) {
			return proxy, nil
		}

		return http.ProxyFromEnvironment(r)
	}
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}

	return transport, nil
}

// BuildThrough is the Builder for GitHub over next.
func BuildThrough(next http.RoundTripper) forge.Builder {
	return func(api string, auth forge.Authenticator) forge.Forge {
		return NewForge(NewForgeParams{Auth: auth, API: api, Next: next})
	}
}
