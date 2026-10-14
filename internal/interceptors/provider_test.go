package interceptors

import (
	"maps"
	"testing"
	"time"

	"komrad/internal/oidctest"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProviderVerifier pins which ID tokens the engine takes and who each one names.
func TestProviderVerifier(t *testing.T) {
	provider := oidctest.Start(t)
	other := oidctest.Start(t)

	type providerVerifierCase struct {
		name              string
		isSignedElsewhere bool
		overrides         map[string]any
		wantCode          connect.Code
		wantVerified      bool
	}

	tests := []providerVerifierCase{
		{
			name:      "a token for another client",
			overrides: map[string]any{"aud": "someone-else"},
			wantCode:  connect.CodeUnauthenticated,
		},
		{
			name:      "a token that ran out",
			overrides: map[string]any{"exp": time.Now().Add(-time.Minute).Unix()},
			wantCode:  connect.CodeUnauthenticated,
		},
		{
			name:              "a token signed by another key",
			isSignedElsewhere: true,
			wantCode:          connect.CodeUnauthenticated,
		},
		{
			name:      "a token from another issuer",
			overrides: map[string]any{"iss": "https://elsewhere.example"},
			wantCode:  connect.CodeUnauthenticated,
		},
		{
			name:      "a token naming nobody",
			overrides: map[string]any{"sub": ""},
			wantCode:  connect.CodeUnauthenticated,
		},
		{
			name:      "an email the provider has not verified",
			overrides: map[string]any{"email_verified": false},
		},
		{
			name:         "an email verified as the string some providers send",
			wantVerified: true,
		},
		{
			name:         "an email verified as a boolean",
			overrides:    map[string]any{"email_verified": true},
			wantVerified: true,
		},
	}

	verifier := NewProviderVerifier(NewProviderVerifierParams{Issuer: provider.URL, ClientID: oidctest.ClientID})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := provider.Claims(map[string]any{"email": "Harry@Example.com", "email_verified": "true"})
			maps.Copy(claims, tt.overrides)

			signer := provider
			if tt.isSignedElsewhere {
				signer = other
			}

			signedIn, err := verifier.VerifyToken(t.Context(), signer.Sign(claims))
			if tt.wantCode != 0 {
				require.Error(t, err)
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))

				return
			}

			require.NoError(t, err)
			assert.Equal(t, "Harry@Example.com", signedIn.Caller.Email, "the database folds the case, the same way it folded the grant")
			assert.Equal(t, provider.URL, signedIn.Caller.Issuer)
			assert.Equal(t, oidctest.Subject, signedIn.Caller.Subject)
			assert.Equal(t, tt.wantVerified, signedIn.EmailVerified)
			assert.WithinDuration(t, time.Now().Add(time.Hour), signedIn.Expiry, time.Minute)
		})
	}
}

// TestProviderVerifierLooksTheProviderUpOnceItAnswers covers an engine started while its provider
// was down, which would otherwise refuse everyone until it restarted.
func TestProviderVerifierLooksTheProviderUpOnceItAnswers(t *testing.T) {
	provider := oidctest.Start(t)
	provider.SetDown(true)

	verifier := NewProviderVerifier(NewProviderVerifierParams{Issuer: provider.URL, ClientID: oidctest.ClientID})
	token := provider.Sign(provider.Claims(nil))

	_, err := verifier.VerifyToken(t.Context(), token)
	require.Error(t, err)
	assert.Equal(t, connect.CodeUnavailable, connect.CodeOf(err))

	provider.SetDown(false)

	signedIn, err := verifier.VerifyToken(t.Context(), token)
	require.NoError(t, err)
	assert.Equal(t, oidctest.Subject, signedIn.Caller.Subject)
}
