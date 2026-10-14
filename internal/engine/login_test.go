package engine

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
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
	"github.com/zalando/go-keyring"
)

type testProvider struct {
	url       string
	key       *rsa.PrivateKey
	mu        sync.Mutex
	refusal   string
	nonce     string
	challenge string
	refreshes int
}

func startTestProvider(t *testing.T) *testProvider {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	provider := &testProvider{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                provider.url,
			"jwks_uri":                              provider.url + "/keys",
			"authorization_endpoint":                provider.url + "/auth",
			"token_endpoint":                        provider.url + "/token",
			"scopes_supported":                      []string{"openid", "email", "profile", offlineScope},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		assert.NoError(t, json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key:       &key.PublicKey,
			KeyID:     "k1",
			Algorithm: "RS256",
			Use:       "sig",
		}}}))
	})
	mux.HandleFunc("/auth", provider.authorize)
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		provider.issue(t, w, r)
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	provider.url = server.URL

	return provider
}

func (p *testProvider) authorize(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	p.mu.Lock()
	p.nonce = query.Get("nonce")
	p.challenge = query.Get("code_challenge")
	refusal := p.refusal
	p.mu.Unlock()

	back := url.Values{"state": {query.Get("state")}}
	switch refusal {
	case "":
		back.Set("code", "code-1")
	case "state":
		back.Set("code", "code-1")
		back.Set("state", "some-other-state")
	default:
		back.Set("error", refusal)
	}

	http.Redirect(w, r, query.Get("redirect_uri")+"?"+back.Encode(), http.StatusFound)
}

func (p *testProvider) issue(t *testing.T, w http.ResponseWriter, r *http.Request) {
	require.NoError(t, r.ParseForm())

	p.mu.Lock()
	defer p.mu.Unlock()

	nonce := ""
	switch r.Form.Get("grant_type") {
	case "authorization_code":
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(sum[:]) != p.challenge {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)

			return
		}

		nonce = p.nonce
	case "refresh_token":
		if r.Form.Get("refresh_token") != "refresh-1" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)

			return
		}

		p.refreshes++
	}

	w.Header().Set("Content-Type", "application/json")
	assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
		"access_token":  "access",
		"token_type":    "Bearer",
		"expires_in":    3600,
		"refresh_token": "refresh-2",
		"id_token":      p.sign(t, nonce),
	}))
}

func (p *testProvider) sign(t *testing.T, nonce string) string {
	t.Helper()

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: p.key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"),
	)
	require.NoError(t, err)

	claims := map[string]any{
		"iss":   p.url,
		"aud":   "komrad",
		"sub":   "sub-1",
		"email": "harry@example.com",
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(time.Hour).Unix(),
	}
	if nonce != "" {
		claims["nonce"] = nonce
	}

	payload, err := json.Marshal(claims)
	require.NoError(t, err)

	signed, err := signer.Sign(payload)
	require.NoError(t, err)

	raw, err := signed.CompactSerialize()
	require.NoError(t, err)

	return raw
}

func (p *testProvider) countRefreshes() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.refreshes
}

func startTestEngine(t *testing.T, provider *testProvider) string {
	t.Helper()

	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != SignInPath || provider == nil {
			http.NotFound(w, r)

			return
		}

		_ = json.NewEncoder(w).Encode(SignInProvider{Issuer: provider.url, ClientID: "komrad"})
	}))
	t.Cleanup(engine.Close)

	return engine.URL
}

type followingOpener struct{}

func (followingOpener) OpenURL(ctx context.Context, target string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}

	resp, err := xhttp.NewClient(time.Minute).Do(req)
	if err != nil {
		return err
	}

	return resp.Body.Close()
}

// TestLogin covers `komrad login` from asking the engine for its provider to keeping the sign-in.
func TestLogin(t *testing.T) {
	keyring.MockInit()

	type loginCase struct {
		name        string
		hasProvider bool
		refusal     string
		wantErr     error
	}

	tests := []loginCase{
		{
			name:    "an engine that names no provider",
			wantErr: errNoProvider,
		},
		{
			name:        "a provider that refuses the sign-in",
			hasProvider: true,
			refusal:     "access_denied",
			wantErr:     errLoginRefused,
		},
		{
			name:        "a browser back from some other sign-in",
			hasProvider: true,
			refusal:     "state",
			wantErr:     errLoginMismatch,
		},
		{
			name:        "a person who signs in",
			hasProvider: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := startTestProvider(t)
			provider.refusal = tt.refusal

			var named *testProvider
			if tt.hasProvider {
				named = provider
			}
			engineURL := startTestEngine(t, named)
			keeper := NewSignInKeeper(t.TempDir())

			loggedIn, err := Login(t.Context(), LoginParams{
				URL:    engineURL,
				Keeper: keeper,
				Opener: followingOpener{},
				Listen: "127.0.0.1:0",
				Prompt: func(string) {},
			})
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, "harry@example.com", loggedIn.Email)
			assert.Equal(t, "sub-1", loggedIn.Subject)
			assert.Equal(t, "refresh-2", loggedIn.RefreshToken)

			kept, err := keeper.GetSignIn(engineURL)
			require.NoError(t, err)
			assert.Equal(t, loggedIn, kept)
		})
	}
}

// TestTokenRefresher covers the token each command sends, renewed only when it is near its end.
func TestTokenRefresher(t *testing.T) {
	keyring.MockInit()

	type tokenRefresherCase struct {
		name          string
		signIn        *signIn
		wantErr       error
		wantRefreshes int
		wantRenewed   bool
	}

	tests := []tokenRefresherCase{
		{
			name:    "a machine that never signed in",
			wantErr: errNotSignedIn,
		},
		{
			name: "a sign-in that ran out with no refresh token",
			signIn: &signIn{
				IDToken: "old",
				Expiry:  time.Now().Add(time.Minute),
			},
			wantErr: errSignInLapsed,
		},
		{
			name: "a sign-in near its end",
			signIn: &signIn{
				IDToken:      "old",
				RefreshToken: "refresh-1",
				Expiry:       time.Now().Add(time.Minute),
			},
			wantRefreshes: 1,
			wantRenewed:   true,
		},
		{
			name: "a sign-in with time left",
			signIn: &signIn{
				IDToken:      "old",
				RefreshToken: "refresh-1",
				Expiry:       time.Now().Add(time.Hour),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := startTestProvider(t)
			keeper := NewSignInKeeper(t.TempDir())
			engineURL := "https://engine-" + t.Name() + ".example"

			if tt.signIn != nil {
				tt.signIn.Provider = SignInProvider{Issuer: provider.url, ClientID: "komrad"}
				require.NoError(t, keeper.SaveSignIn(engineURL, *tt.signIn))
			}

			refresher := NewTokenRefresher(keeper, engineURL)
			token, err := refresher.GetToken(t.Context())
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantRefreshes, provider.countRefreshes())
			assert.Equal(t, tt.wantRenewed, token != "old")

			again, err := refresher.GetToken(t.Context())
			require.NoError(t, err)
			assert.Equal(t, token, again)
			assert.Equal(t, tt.wantRefreshes, provider.countRefreshes())

			if tt.wantRenewed {
				kept, err := keeper.GetSignIn(engineURL)
				require.NoError(t, err)
				assert.Equal(t, token, kept.IDToken)
				assert.Equal(t, "refresh-2", kept.RefreshToken)
			}
		})
	}
}

// TestSignInKeeperFallsBackToAFile covers a machine with no keychain, such as a headless box.
func TestSignInKeeperFallsBackToAFile(t *testing.T) {
	keyring.MockInitWithError(keyring.ErrUnsupportedPlatform)
	t.Cleanup(keyring.MockInit)

	keeper := NewSignInKeeper(t.TempDir())
	want := signIn{
		Email:   "harry@example.com",
		IDToken: "token",
		Expiry:  time.Now().Add(time.Hour).UTC(),
	}

	require.NoError(t, keeper.SaveSignIn("https://engine.example", want))

	kept, err := keeper.GetSignIn("https://engine.example")
	require.NoError(t, err)
	assert.Equal(t, want, *kept)

	_, err = keeper.GetSignIn("https://another.example")
	assert.ErrorIs(t, err, errNotSignedIn)
}

// TestBearerTransportSignsEachRequest covers the token reaching the engine without touching the
// caller's own request, which a retry would send again.
func TestBearerTransportSignsEachRequest(t *testing.T) {
	keyring.MockInit()

	keeper := NewSignInKeeper(t.TempDir())
	require.NoError(t, keeper.SaveSignIn("https://engine.example", signIn{
		IDToken: "token",
		Expiry:  time.Now().Add(time.Hour),
	}))

	var authorization string
	engine := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
	}))
	t.Cleanup(engine.Close)

	client := xhttp.NewClientOver(NewBearerTransport(NewTokenRefresher(keeper, "https://engine.example"), xhttp.NewTransport()), time.Minute)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, engine.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	assert.Equal(t, "Bearer token", authorization)
	assert.Empty(t, req.Header.Get("Authorization"))
}
