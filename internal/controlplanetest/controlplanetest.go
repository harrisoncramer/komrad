package controlplanetest

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"komrad/internal/forge/github"
	"komrad/lib/xhttp"

	"github.com/stretchr/testify/require"
)

// UserCode is the code the GitHub hands out for a person to enter.
const UserCode = "WDJB-MJHT"

// VerificationURI is where the GitHub tells a person to enter the code.
const VerificationURI = "https://github.com/login/device"

// CA is the control plane's CA, written to PEM files.
type CA struct {
	CertFile string
	KeyFile  string
	Cert     *tls.Certificate
	Pool     *x509.CertPool
}

// NewCA makes a CA and writes it under a temp dir.
func NewCA(tb testing.TB) *CA {
	tb.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(tb, err)

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "komrad control plane"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	require.NoError(tb, err)

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(tb, err)

	dir := tb.TempDir()
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	certFile, keyFile := filepath.Join(dir, "ca.pem"), filepath.Join(dir, "ca-key.pem")
	require.NoError(tb, os.WriteFile(certFile, certPEM, 0o600))
	require.NoError(tb, os.WriteFile(keyFile, keyPEM, 0o600))

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(tb, err)

	pool := x509.NewCertPool()
	pool.AddCert(cert.Leaf)

	return &CA{CertFile: certFile, KeyFile: keyFile, Cert: &cert, Pool: pool}
}

// sign is a certificate for GitHub's hosts and the loopback address, signed by the CA.
func (c *CA) sign(tb testing.TB) tls.Certificate {
	tb.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(tb, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "github.com"},
		DNSNames:     github.ForgeProxyHosts,
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, c.Cert.Leaf, key.Public(), c.Cert.PrivateKey)
	require.NoError(tb, err)

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// Request is one request the GitHub took.
type Request struct {
	Host          string
	Path          string
	Authorization string
}

// GitHub answers on every one of the forge proxy's hosts, naming whoever sends a token after the token.
type GitHub struct {
	// URL is where the device flow's endpoints are.
	URL      string
	ca       *CA
	addr     string
	mu       sync.Mutex
	requests []Request
	poll     map[string]any
}

// StartGitHub serves a GitHub on a certificate the CA signed, until the test ends.
func StartGitHub(tb testing.TB, ca *CA) *GitHub {
	tb.Helper()

	fake := &GitHub{ca: ca, poll: map[string]any{"error": "authorization_pending"}}

	server := httptest.NewUnstartedServer(http.HandlerFunc(fake.serve))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{ca.sign(tb)}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	tb.Cleanup(server.Close)

	fake.URL = server.URL
	fake.addr = server.Listener.Addr().String()

	return fake
}

// Authorize has the next poll hand out token as the person's login.
func (g *GitHub) Authorize(token string) {
	g.setPoll(map[string]any{"access_token": token, "token_type": "bearer", "scope": "repo,read:org,workflow"})
}

// Refuse has the next poll answer with an OAuth error, such as slow_down or access_denied.
func (g *GitHub) Refuse(code string, interval int) {
	g.setPoll(map[string]any{"error": code, "interval": interval})
}

func (g *GitHub) setPoll(answer map[string]any) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.poll = answer
}

// Requests are the requests the GitHub took, oldest first.
func (g *GitHub) Requests() []Request {
	g.mu.Lock()
	defer g.mu.Unlock()

	return append([]Request(nil), g.requests...)
}

// Upstream reaches the GitHub for every host a request names, trusting the CA.
func (g *GitHub) Upstream() *http.Transport {
	transport := xhttp.NewTransport()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, g.addr)
	}
	transport.TLSClientConfig = &tls.Config{RootCAs: g.ca.Pool, MinVersion: tls.VersionTLS12}

	return transport
}

// Client reaches the GitHub's device flow endpoints.
func (g *GitHub) Client() *http.Client {
	return xhttp.NewClientOver(g.Upstream(), 10*time.Second)
}

func (g *GitHub) serve(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	g.requests = append(g.requests, Request{Host: r.Host, Path: r.URL.Path, Authorization: r.Header.Get("Authorization")})
	poll := g.poll
	g.mu.Unlock()

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/login/device/code":
		writeJSON(w, map[string]any{
			"device_code":      "3584d83530557fdd1f46af8289938c8ef79f9dc5",
			"user_code":        UserCode,
			"verification_uri": VerificationURI,
			"interval":         5,
			"expires_in":       900,
		})
	case r.Method == http.MethodPost && r.URL.Path == "/login/oauth/access_token":
		writeJSON(w, poll)
	case r.URL.Path == "/user":
		writeJSON(w, map[string]any{"login": tokenOf(r), "id": 7})
	case r.Method == http.MethodPost && r.URL.Path == "/graphql":
		writeJSON(w, map[string]any{"data": map[string]any{"viewer": map[string]any{"login": tokenOf(r)}}})
	default:
		http.NotFound(w, r)
	}
}

// tokenOf is the token a request carries, as a bearer token or as git's basic auth password.
func tokenOf(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if token, ok := strings.CutPrefix(header, "Bearer "); ok {
		return token
	}
	if token, ok := strings.CutPrefix(header, "token "); ok {
		return token
	}
	if encoded, ok := strings.CutPrefix(header, "Basic "); ok {
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err == nil {
			_, password, _ := strings.Cut(string(decoded), ":")

			return password
		}
	}

	return ""
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body) //nolint:errchkjson // the forge proxy hears a cut answer as a failed request
}
