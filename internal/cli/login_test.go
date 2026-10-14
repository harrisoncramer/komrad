//go:build integration

package cli

import (
	"testing"
	"time"

	"komrad/config"
	"komrad/internal/engine"
	"komrad/internal/komradtest"
	"komrad/internal/oidctest"
	"komrad/lib/xhttp"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

// TestIntegrationPanelSignIn covers the answers a panel on an engine at a url gets from the real
// engine and a provider in the test's own process, from before any login to a granted sign-in.
func TestIntegrationPanelSignIn(t *testing.T) {
	keyring.MockInit()

	type panelSignInCase struct {
		name       string
		isGranted  bool
		isLogin    bool
		arrange    func(provider *oidctest.Provider)
		disturb    func(provider *oidctest.Provider)
		wantLogin  string
		wantErr    error
		wantReason string
	}

	tests := []panelSignInCase{
		{
			name:       "a machine that never signed in",
			isGranted:  true,
			wantErr:    engine.ErrSignInNeeded,
			wantReason: "has not signed in",
		},
		{
			name:      "a browser back from some other sign-in",
			isGranted: true,
			isLogin:   true,
			arrange:   func(provider *oidctest.Provider) { provider.MismatchState() },
			wantLogin: "some other sign-in",
			wantErr:   engine.ErrSignInNeeded,
		},
		{
			name:       "a token the engine cannot verify",
			isGranted:  true,
			isLogin:    true,
			disturb:    func(provider *oidctest.Provider) { provider.RotateKey() },
			wantErr:    engine.ErrSignInNeeded,
			wantReason: "not one the identity provider signed for komrad",
		},
		{
			name:      "a sign-in the provider would not renew",
			isGranted: true,
			isLogin:   true,
			arrange:   func(provider *oidctest.Provider) { provider.SetTokenLifetime(time.Minute) },
			disturb:   func(provider *oidctest.Provider) { provider.RefuseRefresh() },
			wantErr:   engine.ErrSignInNeeded,
		},
		{
			name:      "a provider that cannot be reached to renew the sign-in",
			isGranted: true,
			isLogin:   true,
			arrange:   func(provider *oidctest.Provider) { provider.SetTokenLifetime(time.Minute) },
			disturb:   func(provider *oidctest.Provider) { provider.SetDown(true) },
			wantErr:   engine.ErrProviderUnreachable,
		},
		{
			name:      "a token endpoint that cannot be reached to renew the sign-in",
			isGranted: true,
			isLogin:   true,
			arrange:   func(provider *oidctest.Provider) { provider.SetTokenLifetime(time.Minute) },
			disturb:   func(provider *oidctest.Provider) { provider.SetTokenDown(true) },
			wantErr:   engine.ErrProviderUnreachable,
		},
		{
			name:       "a person nobody granted access",
			isLogin:    true,
			wantErr:    engine.ErrAccessDenied,
			wantReason: "nobody has granted this account access",
		},
		{
			name:      "a person signed in with access",
			isGranted: true,
			isLogin:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := oidctest.Start(t)
			instance := komradtest.Start(t, komradtest.Params{SignIn: config.SignIn{
				Issuer:   provider.URL,
				ClientID: oidctest.ClientID,
			}})
			if tt.isGranted {
				instance.Stores.SeedAccess(t, oidctest.Email)
			}
			if tt.arrange != nil {
				tt.arrange(provider)
			}

			keeper := engine.NewSignInKeeper(t.TempDir())
			refresher := engine.NewTokenRefresher(keeper, instance.PublicURL)
			signIn := &panelSignIn{
				url:       instance.PublicURL,
				transport: engine.NewBearerTransport(refresher, xhttp.NewTransport()),
				refresher: refresher,
				keeper:    keeper,
				opener:    oidctest.Browser{},
				listen:    "127.0.0.1:0",
			}

			if tt.isLogin {
				var prompted string
				err := signIn.Login(t.Context(), func(authURL string) { prompted = authURL })
				assert.Contains(t, prompted, provider.URL, "the panel is told where to sign in")
				if tt.wantLogin != "" {
					require.ErrorContains(t, err, tt.wantLogin)
				} else {
					require.NoError(t, err)
				}
			}
			if tt.disturb != nil {
				tt.disturb(provider)
			}

			err := signIn.CheckAccess(t.Context())
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.ErrorContains(t, err, tt.wantReason, "the panel shows why the engine turned the person away")
				assert.NotContains(t, err.Error(), "/healthz", "the reason reads as the panel's, not the request's")

				return
			}

			require.NoError(t, err)
		})
	}
}
