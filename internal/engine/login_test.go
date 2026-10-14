package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"komrad/internal/oidctest"
	"komrad/lib/xhttp"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

func startTestEngine(t *testing.T, provider *oidctest.Provider) string {
	t.Helper()

	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != SignInPath || provider == nil {
			http.NotFound(w, r)

			return
		}

		_ = json.NewEncoder(w).Encode(SignInProvider{Issuer: provider.URL, ClientID: oidctest.ClientID})
	}))
	t.Cleanup(engine.Close)

	return engine.URL
}

// TestLogin covers `komrad login` from asking the engine for its provider to keeping the sign-in.
func TestLogin(t *testing.T) {
	keyring.MockInit()

	type loginCase struct {
		name         string
		hasProvider  bool
		refusal      string
		isStateWrong bool
		wantErr      error
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
			name:         "a browser back from some other sign-in",
			hasProvider:  true,
			isStateWrong: true,
			wantErr:      errLoginMismatch,
		},
		{
			name:        "a person who signs in",
			hasProvider: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := oidctest.Start(t)
			provider.RefuseSignIn(tt.refusal)
			if tt.isStateWrong {
				provider.MismatchState()
			}

			var named *oidctest.Provider
			if tt.hasProvider {
				named = provider
			}
			engineURL := startTestEngine(t, named)
			keeper := NewSignInKeeper(t.TempDir())

			loggedIn, err := Login(t.Context(), LoginParams{
				URL:    engineURL,
				Keeper: keeper,
				Opener: oidctest.Browser{},
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

// TestTokenRefresher covers the token each command sends, renewed only when it is near its end and never again with a refresh token the provider turned down.
func TestTokenRefresher(t *testing.T) {
	keyring.MockInit()

	type tokenRefresherCase struct {
		name           string
		signIn         *signIn
		isProviderDown bool
		arrange        func(provider *oidctest.Provider)
		wantErr        error
		wantRefreshes  int
		wantRenewed    bool
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
			name: "a provider that cannot be reached",
			signIn: &signIn{
				IDToken:      "old",
				RefreshToken: "refresh-1",
				Expiry:       time.Now().Add(time.Minute),
			},
			isProviderDown: true,
			wantErr:        ErrProviderUnreachable,
		},
		{
			name: "a refresh token the provider turned down",
			signIn: &signIn{
				IDToken:      "old",
				RefreshToken: "refresh-1",
				Expiry:       time.Now().Add(time.Minute),
			},
			arrange: func(provider *oidctest.Provider) { provider.RefuseRefresh() },
			wantErr: errSignInLapsed,
		},
		{
			name: "a token endpoint that cannot be reached",
			signIn: &signIn{
				IDToken:      "old",
				RefreshToken: "refresh-1",
				Expiry:       time.Now().Add(time.Minute),
			},
			arrange: func(provider *oidctest.Provider) { provider.SetTokenDown(true) },
			wantErr: ErrProviderUnreachable,
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
			provider := oidctest.Start(t)
			keeper := NewSignInKeeper(t.TempDir())
			engineURL := "https://engine-" + t.Name() + ".example"
			if tt.arrange != nil {
				tt.arrange(provider)
			}

			if tt.signIn != nil {
				tt.signIn.Provider = SignInProvider{Issuer: provider.URL, ClientID: oidctest.ClientID}
				if tt.isProviderDown {
					tt.signIn.Provider.Issuer = "http://" + deadAddr(t)
				}
				require.NoError(t, keeper.SaveSignIn(engineURL, *tt.signIn))
			}

			refresher := NewTokenRefresher(keeper, engineURL)
			token, err := refresher.GetToken(t.Context())
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				asked := provider.CountRefreshes()
				_, err = refresher.GetToken(t.Context())
				require.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, asked, provider.CountRefreshes())

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantRefreshes, provider.CountRefreshes())
			assert.Equal(t, tt.wantRenewed, token != "old")

			again, err := refresher.GetToken(t.Context())
			require.NoError(t, err)
			assert.Equal(t, token, again)
			assert.Equal(t, tt.wantRefreshes, provider.CountRefreshes())

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

// TestSignInKeeperDeletesASignIn covers dropping a sign-in from the keychain or the file, and dropping one that was never kept.
func TestSignInKeeperDeletesASignIn(t *testing.T) {
	t.Cleanup(keyring.MockInit)

	type deleteSignInCase struct {
		name        string
		hasKeychain bool
		isSignedIn  bool
	}

	tests := []deleteSignInCase{
		{
			name:        "a machine that never signed in",
			hasKeychain: true,
		},
		{
			name:        "a sign-in kept in the keychain",
			hasKeychain: true,
			isSignedIn:  true,
		},
		{
			name:       "a sign-in kept in a file",
			isSignedIn: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.hasKeychain {
				keyring.MockInit()
			} else {
				keyring.MockInitWithError(keyring.ErrUnsupportedPlatform)
			}

			keeper := NewSignInKeeper(t.TempDir())
			if tt.isSignedIn {
				require.NoError(t, keeper.SaveSignIn("https://engine.example", signIn{IDToken: "token"}))
			}

			require.NoError(t, keeper.DeleteSignIn("https://engine.example"))

			_, err := keeper.GetSignIn("https://engine.example")
			assert.ErrorIs(t, err, errNotSignedIn)
		})
	}
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

func deadAddr(t *testing.T) string {
	t.Helper()

	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closed.Close()

	return strings.TrimPrefix(closed.URL, "http://")
}
