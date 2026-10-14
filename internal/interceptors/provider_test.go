package interceptors

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testProvider struct {
	url  string
	key  *rsa.PrivateKey
	down atomic.Bool
}

func startTestProvider(t *testing.T) *testProvider {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	provider := &testProvider{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		if provider.down.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)

			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                provider.url,
			"jwks_uri":                              provider.url + "/keys",
			"authorization_endpoint":                provider.url + "/auth",
			"token_endpoint":                        provider.url + "/token",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key:       &key.PublicKey,
			KeyID:     "k1",
			Algorithm: "RS256",
			Use:       "sig",
		}}})
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	provider.url = server.URL

	return provider
}

func (p *testProvider) sign(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"),
	)
	require.NoError(t, err)

	payload, err := json.Marshal(claims)
	require.NoError(t, err)

	signed, err := signer.Sign(payload)
	require.NoError(t, err)

	raw, err := signed.CompactSerialize()
	require.NoError(t, err)

	return raw
}

func (p *testProvider) claims(overrides map[string]any) map[string]any {
	claims := map[string]any{
		"iss":            p.url,
		"aud":            "komrad",
		"sub":            "sub-1",
		"email":          "Harry@Example.com",
		"email_verified": "true",
		"iat":            time.Now().Unix(),
		"exp":            time.Now().Add(time.Hour).Unix(),
	}
	for name, value := range overrides {
		claims[name] = value
	}

	return claims
}

// TestProviderVerifier pins which ID tokens the engine takes and who each one names.
func TestProviderVerifier(t *testing.T) {
	provider := startTestProvider(t)
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	type providerVerifierCase struct {
		name         string
		key          *rsa.PrivateKey
		overrides    map[string]any
		wantCode     connect.Code
		wantVerified bool
	}

	tests := []providerVerifierCase{
		{
			name:      "a token for another client",
			key:       provider.key,
			overrides: map[string]any{"aud": "someone-else"},
			wantCode:  connect.CodeUnauthenticated,
		},
		{
			name:      "a token that ran out",
			key:       provider.key,
			overrides: map[string]any{"exp": time.Now().Add(-time.Minute).Unix()},
			wantCode:  connect.CodeUnauthenticated,
		},
		{
			name:     "a token signed by another key",
			key:      otherKey,
			wantCode: connect.CodeUnauthenticated,
		},
		{
			name:      "a token from another issuer",
			key:       provider.key,
			overrides: map[string]any{"iss": "https://elsewhere.example"},
			wantCode:  connect.CodeUnauthenticated,
		},
		{
			name:      "a token naming nobody",
			key:       provider.key,
			overrides: map[string]any{"sub": ""},
			wantCode:  connect.CodeUnauthenticated,
		},
		{
			name:      "an email the provider has not verified",
			key:       provider.key,
			overrides: map[string]any{"email_verified": false},
		},
		{
			name:         "an email verified as the string some providers send",
			key:          provider.key,
			wantVerified: true,
		},
		{
			name:         "an email verified as a boolean",
			key:          provider.key,
			overrides:    map[string]any{"email_verified": true},
			wantVerified: true,
		},
	}

	verifier := NewProviderVerifier(NewProviderVerifierParams{Issuer: provider.url, ClientID: "komrad"})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signedIn, err := verifier.VerifyToken(t.Context(), provider.sign(t, tt.key, provider.claims(tt.overrides)))
			if tt.wantCode != 0 {
				require.Error(t, err)
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))

				return
			}

			require.NoError(t, err)
			assert.Equal(t, "harry@example.com", signedIn.Caller.Email)
			assert.Equal(t, provider.url, signedIn.Caller.Issuer)
			assert.Equal(t, "sub-1", signedIn.Caller.Subject)
			assert.Equal(t, tt.wantVerified, signedIn.EmailVerified)
			assert.WithinDuration(t, time.Now().Add(time.Hour), signedIn.Expiry, time.Minute)
		})
	}
}

// TestProviderVerifierLooksTheProviderUpOnceItAnswers covers an engine started while its provider
// was down, which would otherwise refuse everyone until it restarted.
func TestProviderVerifierLooksTheProviderUpOnceItAnswers(t *testing.T) {
	provider := startTestProvider(t)
	provider.down.Store(true)

	verifier := NewProviderVerifier(NewProviderVerifierParams{Issuer: provider.url, ClientID: "komrad"})
	token := provider.sign(t, provider.key, provider.claims(nil))

	_, err := verifier.VerifyToken(t.Context(), token)
	require.Error(t, err)
	assert.Equal(t, connect.CodeUnavailable, connect.CodeOf(err))

	provider.down.Store(false)

	signedIn, err := verifier.VerifyToken(t.Context(), token)
	require.NoError(t, err)
	assert.Equal(t, "sub-1", signedIn.Caller.Subject)
}
