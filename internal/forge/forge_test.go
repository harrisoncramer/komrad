package forge

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"komrad/internal/git"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAuth struct {
	credential  Credential
	err         error
	invalidated int
}

func (f *fakeAuth) Credential(context.Context) (Credential, error) {
	return f.credential, f.err
}

func (f *fakeAuth) Invalidate(context.Context) error {
	f.invalidated++

	return nil
}

// TestTransport covers the one job of the round tripper: the credential goes on every request
// in the header it asked for, and a 401 reaches the Authenticator.
func TestTransport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		credential      Credential
		status          int
		wantHeader      string
		wantValue       string
		wantInvalidated int
	}{
		{
			name:            "a bearer token is sent as Authorization",
			credential:      Credential{Header: HeaderBearer, Token: NewSecret("tok")},
			status:          http.StatusOK,
			wantHeader:      "Authorization",
			wantValue:       "Bearer tok",
			wantInvalidated: 0,
		},
		{
			name:            "a private token is sent bare",
			credential:      Credential{Header: HeaderPrivateToken, Token: NewSecret("tok")},
			status:          http.StatusOK,
			wantHeader:      "PRIVATE-TOKEN",
			wantValue:       "tok",
			wantInvalidated: 0,
		},
		{
			name:            "a 401 invalidates the credential",
			credential:      Credential{Header: HeaderBearer, Token: NewSecret("tok")},
			status:          http.StatusUnauthorized,
			wantHeader:      "Authorization",
			wantValue:       "Bearer tok",
			wantInvalidated: 1,
		},
		{
			name:            "a 403 does not",
			credential:      Credential{Header: HeaderBearer, Token: NewSecret("tok")},
			status:          http.StatusForbidden,
			wantHeader:      "Authorization",
			wantValue:       "Bearer tok",
			wantInvalidated: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var seen http.Header
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen = r.Header.Clone()
				w.WriteHeader(tt.status)
			}))
			t.Cleanup(server.Close)

			auth := &fakeAuth{credential: tt.credential}
			client := &http.Client{Transport: NewTransport(auth, server.Client().Transport)}

			resp, err := client.Get(server.URL)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())

			assert.Equal(t, tt.wantValue, seen.Get(tt.wantHeader))
			assert.Equal(t, tt.wantInvalidated, auth.invalidated)
		})
	}
}

type failingInvalidate struct{ fakeAuth }

func (f *failingInvalidate) Invalidate(context.Context) error { return errors.New("store down") }

// TestTransportAnswersOneThingOnARefusal covers Invalidate failing on a 401: the error comes
// back alone, with the response closed rather than handed over beside it.
func TestTransportAnswersOneThingOnARefusal(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)

	auth := &failingInvalidate{fakeAuth: fakeAuth{credential: Credential{Header: HeaderBearer, Token: NewSecret("tok")}}}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	resp, err := NewTransport(auth, server.Client().Transport).RoundTrip(req)
	require.Error(t, err)
	if resp != nil {
		require.NoError(t, resp.Body.Close())
	}
	assert.Nil(t, resp)
}

// TestTransportRefusesWithoutACredential covers a connection whose file is gone: the request
// never leaves.
func TestTransportRefusesWithoutACredential(t *testing.T) {
	t.Parallel()

	boom := errors.New("no file")

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1:1", nil)
	require.NoError(t, err)

	resp, err := NewTransport(&fakeAuth{err: boom}, nil).RoundTrip(req)
	require.ErrorIs(t, err, boom)
	if resp != nil {
		require.NoError(t, resp.Body.Close())
	}
}

// TestPullRequestIsFinished covers the derivation from two stamps.
func TestPullRequestIsFinished(t *testing.T) {
	t.Parallel()

	now := time.Now()
	assert.False(t, PullRequest{}.IsFinished())
	assert.True(t, PullRequest{ClosedAt: &now}.IsFinished())
	assert.True(t, PullRequest{MergedAt: &now, ClosedAt: &now}.IsFinished())
}

// TestPullRequestIsFromItsOwnRepository covers a branch in the target, in a fork and in none.
func TestPullRequestIsFromItsOwnRepository(t *testing.T) {
	t.Parallel()

	komrad := git.RemoteURL{Scheme: "https", Host: "github.com", Path: "harrisoncramer/komrad"}
	overSSH := git.RemoteURL{Scheme: "ssh", Host: "github.com", Path: "harrisoncramer/komrad"}
	fork := git.RemoteURL{Scheme: "https", Host: "github.com", Path: "someone/komrad"}

	assert.True(t, PullRequest{Repository: komrad, Head: overSSH}.IsFromItsOwnRepository())
	assert.False(t, PullRequest{Repository: komrad, Head: fork}.IsFromItsOwnRepository())
	assert.False(t, PullRequest{Repository: komrad}.IsFromItsOwnRepository())
}

// TestRateLimitIsLow covers the fifth of the budget a reader slows down below.
func TestRateLimitIsLow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		limit RateLimit
		want  bool
	}{
		{name: "unknown", limit: RateLimit{Remaining: 0, Limit: 5000}, want: false},
		{name: "no limit said", limit: RateLimit{Known: true, Remaining: 0}, want: false},
		{name: "plenty left", limit: RateLimit{Known: true, Remaining: 4321, Limit: 5000}, want: false},
		{name: "exactly a fifth", limit: RateLimit{Known: true, Remaining: 1000, Limit: 5000}, want: false},
		{name: "under a fifth", limit: RateLimit{Known: true, Remaining: 999, Limit: 5000}, want: true},
		{name: "spent", limit: RateLimit{Known: true, Remaining: 0, Limit: 2000}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.limit.IsLow())
		})
	}
}
