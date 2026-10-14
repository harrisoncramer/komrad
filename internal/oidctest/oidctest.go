// Package oidctest runs an identity provider in the test's own process, answering discovery, the
// browser's sign-in, and the token trades the way a real provider does.
package oidctest

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"komrad/lib/xhttp"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// ClientID is the id komrad is registered under at the provider.
	ClientID = "komrad"
	// Subject is the provider's id for the one person who signs in.
	Subject = "sub-1"
	// Email is the address of the one person who signs in.
	Email = "harry@example.com"
)

const keyID = "k1"

// Provider is an identity provider served on a loopback address for one test.
type Provider struct {
	URL           string
	t             *testing.T
	mu            sync.Mutex
	key           *rsa.PrivateKey
	down          bool
	isTokenDown   bool
	refusal       string
	isStateWrong  bool
	isRefreshDead bool
	lifetime      time.Duration
	nonce         string
	challenge     string
	refreshes     int
}

// Start serves a provider until the test ends.
func Start(t *testing.T) *Provider {
	t.Helper()

	provider := &Provider{
		t:        t,
		key:      generateKey(t),
		lifetime: time.Hour,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", provider.discover)
	mux.HandleFunc("/keys", provider.publishKeys)
	mux.HandleFunc("/auth", provider.authorize)
	mux.HandleFunc("/token", provider.issue)

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	provider.URL = server.URL

	return provider
}

// SetDown makes the provider answer every request with an error, as one that is down does.
func (p *Provider) SetDown(down bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.down = down
}

// SetTokenDown makes only the token endpoint answer with an error, as one behind a failing
// backend does while discovery still answers.
func (p *Provider) SetTokenDown(down bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.isTokenDown = down
}

// RefuseSignIn sends the browser back with the error a provider gives when the person declines.
func (p *Provider) RefuseSignIn(reason string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.refusal = reason
}

// MismatchState sends the browser back carrying a state from some other sign-in.
func (p *Provider) MismatchState() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.isStateWrong = true
}

// RefuseRefresh turns down every refresh token, as a provider does once the person signs out.
func (p *Provider) RefuseRefresh() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.isRefreshDead = true
}

// SetTokenLifetime sets how long each ID token the provider issues from now on lasts.
func (p *Provider) SetTokenLifetime(lifetime time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.lifetime = lifetime
}

// RotateKey signs with a new key from now on, so every ID token issued before fails to verify.
func (p *Provider) RotateKey() {
	key := generateKey(p.t)

	p.mu.Lock()
	defer p.mu.Unlock()

	p.key = key
}

// CountRefreshes is how many refresh tokens the provider has traded.
func (p *Provider) CountRefreshes() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.refreshes
}

// Claims is an ID token's claims for the one person, issued now for komrad, with overrides set.
func (p *Provider) Claims(overrides map[string]any) map[string]any {
	p.mu.Lock()
	lifetime := p.lifetime
	p.mu.Unlock()

	claims := map[string]any{
		"iss":            p.URL,
		"aud":            ClientID,
		"sub":            Subject,
		"email":          Email,
		"email_verified": true,
		"iat":            time.Now().Unix(),
		"exp":            time.Now().Add(lifetime).Unix(),
	}
	for name, value := range overrides {
		claims[name] = value
	}

	return claims
}

// Sign signs claims as an ID token with the provider's current key.
func (p *Provider) Sign(claims map[string]any) string {
	p.t.Helper()

	p.mu.Lock()
	key := p.key
	p.mu.Unlock()

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", keyID),
	)
	require.NoError(p.t, err)

	payload, err := json.Marshal(claims)
	require.NoError(p.t, err)

	signed, err := signer.Sign(payload)
	require.NoError(p.t, err)

	raw, err := signed.CompactSerialize()
	require.NoError(p.t, err)

	return raw
}

func (p *Provider) isDown(w http.ResponseWriter) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.down {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}

	return p.down
}

func (p *Provider) isTokenEndpointDown(w http.ResponseWriter) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.isTokenDown {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}

	return p.isTokenDown
}

func (p *Provider) discover(w http.ResponseWriter, _ *http.Request) {
	if p.isDown(w) {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	assert.NoError(p.t, json.NewEncoder(w).Encode(map[string]any{
		"issuer":                                p.URL,
		"jwks_uri":                              p.URL + "/keys",
		"authorization_endpoint":                p.URL + "/auth",
		"token_endpoint":                        p.URL + "/token",
		"scopes_supported":                      []string{"openid", "email", "profile", "offline_access"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
	}))
}

func (p *Provider) publishKeys(w http.ResponseWriter, _ *http.Request) {
	if p.isDown(w) {
		return
	}

	p.mu.Lock()
	key := p.key
	p.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	assert.NoError(p.t, json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key:       &key.PublicKey,
		KeyID:     keyID,
		Algorithm: "RS256",
		Use:       "sig",
	}}}))
}

func (p *Provider) authorize(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	p.mu.Lock()
	p.nonce = query.Get("nonce")
	p.challenge = query.Get("code_challenge")
	refusal, isStateWrong := p.refusal, p.isStateWrong
	p.mu.Unlock()

	back := url.Values{"state": {query.Get("state")}}
	switch {
	case isStateWrong:
		back.Set("code", "code-1")
		back.Set("state", "some-other-state")
	case refusal != "":
		back.Set("error", refusal)
	default:
		back.Set("code", "code-1")
	}

	http.Redirect(w, r, query.Get("redirect_uri")+"?"+back.Encode(), http.StatusFound)
}

func (p *Provider) issue(w http.ResponseWriter, r *http.Request) {
	if p.isDown(w) || p.isTokenEndpointDown(w) {
		return
	}

	if !assert.NoError(p.t, r.ParseForm()) {
		p.refuse(w, "invalid_request")

		return
	}

	nonce, ok := p.trade(r.Form)
	if !ok {
		p.refuse(w, "invalid_grant")

		return
	}

	claims := p.Claims(nil)
	if nonce != "" {
		claims["nonce"] = nonce
	}

	w.Header().Set("Content-Type", "application/json")
	assert.NoError(p.t, json.NewEncoder(w).Encode(map[string]any{
		"access_token":  "access",
		"token_type":    "Bearer",
		"expires_in":    3600,
		"refresh_token": "refresh-2",
		"id_token":      p.Sign(claims),
	}))
}

// trade checks a token request and answers the nonce its ID token carries, or false to refuse it.
func (p *Provider) trade(form url.Values) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch form.Get("grant_type") {
	case "authorization_code":
		sum := sha256.Sum256([]byte(form.Get("code_verifier")))

		return p.nonce, base64.RawURLEncoding.EncodeToString(sum[:]) == p.challenge
	case "refresh_token":
		if p.isRefreshDead || form.Get("refresh_token") == "" {
			return "", false
		}

		p.refreshes++

		return "", true
	default:
		return "", false
	}
}

func (p *Provider) refuse(w http.ResponseWriter, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	assert.NoError(p.t, json.NewEncoder(w).Encode(map[string]string{"error": code}))
}

func generateKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	return key
}

// Browser follows the provider's redirects back to komrad, as a person who signs in does.
type Browser struct{}

// OpenURL visits target and every page it redirects to.
func (Browser) OpenURL(ctx context.Context, target string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("failed to build a request for %s: %w", target, err)
	}

	resp, err := xhttp.NewClient(time.Minute).Do(req)
	if err != nil {
		return fmt.Errorf("failed to visit %s: %w", target, err)
	}

	return resp.Body.Close()
}
