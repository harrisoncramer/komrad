package interceptors

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"komrad/internal/caller"
	"komrad/internal/rpcerr"
	"komrad/internal/store"
	"komrad/internal/storetest"
	"komrad/lib/clock"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testIssuer = "https://issuer.example"

type fakeVerifier map[string]*signedIn

func (v fakeVerifier) VerifyToken(_ context.Context, raw string) (*signedIn, error) {
	if raw == "provider-down" {
		return nil, rpcerr.Unavailable(errProviderUnreachable)
	}

	signedIn, found := v[raw]
	if !found {
		return nil, rpcerr.Unauthenticated(errTokenRejected)
	}

	copied := *signedIn

	return &copied, nil
}

func signedInAs(subject, email string, verified bool, expiry time.Time) *signedIn {
	return &signedIn{
		Caller: caller.Caller{
			Email:   email,
			Issuer:  testIssuer,
			Subject: subject,
		},
		EmailVerified: verified,
		Expiry:        expiry,
	}
}

// openGranted is the stores of an engine that granted email access and saw subject claim it.
func openGranted(t *testing.T, email, subject string) *storetest.Stores {
	t.Helper()

	stores := storetest.Open(t)
	stores.SeedAccess(t, email)

	_, err := stores.Access.ClaimAccess(t.Context(), store.ClaimAccessParams{
		Email:   email,
		Issuer:  testIssuer,
		Subject: subject,
	})
	require.NoError(t, err)

	return stores
}

// TestSignInGuard pins who the guard lets through to the api, and the status a caller it turns
// away hears.
func TestSignInGuard(t *testing.T) {
	type signInGuardCase struct {
		name          string
		authorization string
		isUnreadable  bool
		wantStatus    int
		wantSubject   string
	}

	tests := []signInGuardCase{
		{
			name:          "a request with no token",
			authorization: "",
			wantStatus:    http.StatusUnauthorized,
		},
		{
			name:          "a token the provider did not sign",
			authorization: "Bearer forged",
			wantStatus:    http.StatusUnauthorized,
		},
		{
			name:          "a provider that cannot be reached",
			authorization: "Bearer provider-down",
			wantStatus:    http.StatusServiceUnavailable,
		},
		{
			name:          "a database that cannot be read",
			authorization: "Bearer granted",
			isUnreadable:  true,
			wantStatus:    http.StatusServiceUnavailable,
		},
		{
			name:          "an unverified email claiming a grant",
			authorization: "Bearer unverified",
			wantStatus:    http.StatusUnauthorized,
		},
		{
			name:          "a verified email nobody granted",
			authorization: "Bearer denied",
			wantStatus:    http.StatusForbidden,
		},
		{
			name:          "a verified email signing in for the first time",
			authorization: "Bearer claimer",
			wantStatus:    http.StatusOK,
			wantSubject:   "sub-claimer",
		},
		{
			name:          "an account already granted",
			authorization: "Bearer granted",
			wantStatus:    http.StatusOK,
			wantSubject:   "sub-granted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stores := openGranted(t, "granted@example.com", "sub-granted")
			stores.SeedAccess(t, "claimer@example.com")
			stores.SeedAccess(t, "unverified@example.com")
			if tt.isUnreadable {
				require.NoError(t, stores.Pool.Close())
			}

			later := time.Now().Add(time.Hour)
			var served caller.Caller
			var agentHeader string
			guard := NewSignInGuard(NewSignInGuardParams{
				Logger: slog.New(slog.DiscardHandler),
				Verifier: fakeVerifier{
					"granted":    signedInAs("sub-granted", "granted@example.com", true, later),
					"unverified": signedInAs("sub-unverified", "unverified@example.com", false, later),
					"claimer":    signedInAs("sub-claimer", "claimer@example.com", true, later),
					"denied":     signedInAs("sub-denied", "denied@example.com", true, later),
				},
				Access: stores.Access,
				Next: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
					var err error
					served, err = caller.FromRequest(r.Context(), r.Header)
					assert.NoError(t, err)
					agentHeader = r.Header.Get(caller.AgentHeader)
				}),
			})

			req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			req.Header.Set(caller.AgentHeader, "agent-one")
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}

			recorder := httptest.NewRecorder()
			guard.ServeHTTP(recorder, req)

			assert.Equal(t, tt.wantStatus, recorder.Code)
			assert.Equal(t, tt.wantSubject, served.Subject)
			assert.Empty(t, agentHeader, "a caller from the network never speaks as an agent")
		})
	}
}

// TestSignInGuardEndsARequestWhenItsTokenRunsOut covers a stream held open past the token it
// signed in with, which would otherwise outlive a revoke for as long as it stays open.
func TestSignInGuardEndsARequestWhenItsTokenRunsOut(t *testing.T) {
	stores := openGranted(t, "short@example.com", "sub-short")

	ended := make(chan error, 1)
	guard := NewSignInGuard(NewSignInGuardParams{
		Logger: slog.New(slog.DiscardHandler),
		Verifier: fakeVerifier{
			"short": signedInAs("sub-short", "short@example.com", true, time.Now().Add(50*time.Millisecond)),
		},
		Access: stores.Access,
		Next: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
			ended <- context.Cause(r.Context())
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/watch", nil)
	req.Header.Set("Authorization", "Bearer short")
	guard.ServeHTTP(httptest.NewRecorder(), req)

	assert.ErrorIs(t, <-ended, errTokenExpired)
}

// TestSignInGuardRenewal covers a held request taking a fresh token, which only the caller who
// opened it may hand over.
func TestSignInGuardRenewal(t *testing.T) {
	type renewalCase struct {
		name    string
		token   string
		wantErr error
	}

	tests := []renewalCase{
		{
			name:    "a token naming someone else",
			token:   "other",
			wantErr: errAnotherSignIn,
		},
		{
			name:    "a token the provider did not sign",
			token:   "forged",
			wantErr: errTokenRejected,
		},
		{
			name:  "a fresh token for the same caller",
			token: "fresh",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stores := openGranted(t, "short@example.com", "sub-short")

			renewed := make(chan error, 2)
			guard := NewSignInGuard(NewSignInGuardParams{
				Logger: slog.New(slog.DiscardHandler),
				Verifier: fakeVerifier{
					"short": signedInAs("sub-short", "short@example.com", true, time.Now().Add(100*time.Millisecond)),
					"fresh": signedInAs("sub-short", "short@example.com", true, time.Now().Add(time.Hour)),
					"other": signedInAs("sub-other", "other@example.com", true, time.Now().Add(time.Hour)),
				},
				Access: stores.Access,
				Next: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
					renewed <- caller.Renew(r.Context(), tt.token)

					clock.Sleep(t.Context(), 200*time.Millisecond)
					renewed <- context.Cause(r.Context())
				}),
			})

			req := httptest.NewRequest(http.MethodGet, "/terminal", nil)
			req.Header.Set("Authorization", "Bearer short")
			guard.ServeHTTP(httptest.NewRecorder(), req)

			renewErr := <-renewed
			if tt.wantErr != nil {
				require.ErrorIs(t, renewErr, tt.wantErr)
				assert.ErrorIs(t, <-renewed, errTokenExpired)

				return
			}

			require.NoError(t, renewErr)
			assert.NoError(t, <-renewed)
		})
	}
}
