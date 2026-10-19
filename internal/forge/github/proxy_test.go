package github

import (
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewForgeProxyTransportSendsOnlyGitHubsHostsThroughTheForgeProxy covers which requests an
// engine sends to the forge proxy and which it sends directly.
func TestNewForgeProxyTransportSendsOnlyGitHubsHostsThroughTheForgeProxy(t *testing.T) {
	t.Parallel()

	signed := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(signed.Close)

	caFile := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: signed.Certificate().Raw}), 0o600))

	transport, err := NewForgeProxyTransport(NewForgeProxyTransportParams{
		ProxyURL: "http://harry:s3cret@10.0.1.5:8789",
		CAFile:   caFile,
	})
	require.NoError(t, err)

	tests := []struct {
		name      string
		url       string
		wantProxy string
	}{
		{name: "the api", url: "https://api.github.com/user", wantProxy: "http://harry:s3cret@10.0.1.5:8789"},
		{name: "git on github.com, whatever its case", url: "https://GitHub.com/harrisoncramer/komrad.git/info/refs", wantProxy: "http://harry:s3cret@10.0.1.5:8789"},
		{name: "raw files", url: "https://raw.githubusercontent.com/harrisoncramer/komrad/main/README.md", wantProxy: "http://harry:s3cret@10.0.1.5:8789"},
		{name: "a signed download GitHub redirects to", url: "https://objects.githubusercontent.com/release"},
		{name: "a host that is not GitHub's", url: "https://gitlab.com/api/v4/user"},
		{name: "GitHub over plain http", url: "http://github.com/user"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			proxy, err := transport.Proxy(req)
			require.NoError(t, err)

			if tt.wantProxy == "" {
				assert.Nil(t, proxy)

				return
			}
			require.NotNil(t, proxy)
			assert.Equal(t, tt.wantProxy, proxy.String())
		})
	}
}

// TestNewForgeProxyTransportRefusesWhatItCannotRead covers a forge proxy url that does not parse,
// whose error keeps the forge proxy credential out, and a ca_file missing or holding no certificate.
func TestNewForgeProxyTransportRefusesWhatItCannotRead(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.pem")
	require.NoError(t, os.WriteFile(empty, []byte("not a certificate\n"), 0o600))

	signed := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(signed.Close)

	caFile := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: signed.Certificate().Raw}), 0o600))

	tests := []struct {
		name     string
		proxyURL string
		caFile   string
	}{
		{
			name:     "a url that does not parse",
			proxyURL: "http://harry:s3cret@10.0.1.5:87 89",
			caFile:   caFile,
		},
		{
			name:     "a missing ca_file",
			proxyURL: "http://harry:s3cret@10.0.1.5:8789",
			caFile:   filepath.Join(dir, "missing.pem"),
		},
		{
			name:     "a ca_file holding no certificate",
			proxyURL: "http://harry:s3cret@10.0.1.5:8789",
			caFile:   empty,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := NewForgeProxyTransport(NewForgeProxyTransportParams{ProxyURL: tt.proxyURL, CAFile: tt.caFile})
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "s3cret")
		})
	}
}
