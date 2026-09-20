package forge

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"weave/internal/dbtest"
	"weave/internal/git"
	githubapp "weave/internal/github/app"
	"weave/internal/store"
	forgestore "weave/internal/store/forge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeForge answers Verify from what it was built with and records the credential it was
// given.
type fakeForge struct {
	auth    Authenticator
	login   string
	reaches int
	err     error
}

func (f *fakeForge) Kind() string { return KindGitHub }

func (f *fakeForge) Verify(ctx context.Context) (string, error) {
	credential, err := f.auth.Credential(ctx)
	if err != nil {
		return "", err
	}
	if !credential.ExpiresAt.IsZero() {
		return "", ErrUnauthorized.Saying("an installation token is not a user")
	}

	return f.login, f.err
}

// VerifyInstallation answers the count it was built with after proving the credential mints.
func (f *fakeForge) VerifyInstallation(ctx context.Context) (int, error) {
	if _, err := f.auth.Credential(ctx); err != nil {
		return 0, err
	}

	return f.reaches, f.err
}

func (f *fakeForge) ListUpdated(context.Context, ListUpdatedParams) (*ListUpdatedResult, error) {
	return &ListUpdatedResult{}, nil
}

func (f *fakeForge) List(context.Context, ListParams) (*ListResult, error) {
	return &ListResult{}, nil
}

func (f *fakeForge) Get(context.Context, string) (PullRequest, error) { return PullRequest{}, nil }

func (f *fakeForge) GetReviewDecision(context.Context, git.RemoteURL, int) (string, error) {
	return "", nil
}

type fixture struct {
	service     *ForgeService
	store       *forgestore.Store
	credentials *FileStore
	seen        []Credential
	apis        []string
	// app is what the service reads on each call that needs the App, so a test can move it
	// between calls the way a config edit would.
	app App
}

func newFixture(t *testing.T, login string, verifyErr error) *fixture {
	t.Helper()

	pool, err := store.Open(t.Context(), dbtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	f := &fixture{store: forgestore.New(pool.GetDB()), credentials: NewFileStore(t.TempDir())}
	f.service = NewForgeService(NewForgeServiceParams{
		Connections: f.store,
		Credentials: f.credentials,
		Builders: map[string]Builder{
			KindGitHub: func(api string, auth Authenticator) Forge {
				credential, _ := auth.Credential(t.Context())
				f.seen = append(f.seen, credential)
				f.apis = append(f.apis, api)

				return &fakeForge{
					auth:    auth,
					login:   login,
					reaches: 3,
					err:     verifyErr,
				}
			},
		},
		Now: func() time.Time { return time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC) },
	})

	return f
}

// TestConnect covers the happy path: the token is proved, the row carries the login the forge
// answered, and the credential is on disk for the next caller.
func TestConnect(t *testing.T) {
	f := newFixture(t, "harrisoncramer", nil)
	ctx := t.Context()

	connection, err := f.service.Connect(ctx, ConnectParams{Host: "GitHub.com", Token: NewSecret(" ghp_secret\n")})
	require.NoError(t, err)
	assert.Equal(t, "github.com", connection.Host)
	assert.Equal(t, KindGitHub, connection.Kind)
	assert.Equal(t, MethodToken, connection.Method)
	assert.Equal(t, "harrisoncramer", connection.Login)
	require.NotNil(t, connection.VerifiedAt)

	require.Len(t, f.seen, 1)
	assert.Equal(t, Credential{Header: HeaderBearer, Token: NewSecret("ghp_secret")}, f.seen[0])

	token, err := f.credentials.Get(ctx, connection.ID)
	require.NoError(t, err)
	assert.Equal(t, "ghp_secret", token.Expose())

	again, err := f.service.ForConnection(*connection)
	require.NoError(t, err)
	login, err := again.Verify(ctx)
	require.NoError(t, err)
	assert.Equal(t, "harrisoncramer", login)
}

// TestConnectRefusals covers everything Connect will not do, none of which leaves a row or a
// file behind.
func TestConnectRefusals(t *testing.T) {
	tests := []struct {
		name      string
		login     string
		verifyErr error
		params    ConnectParams
		wantErr   error
	}{
		{
			name:    "no host",
			params:  ConnectParams{Token: NewSecret("tok")},
			wantErr: nil,
		},
		{
			name:    "no token",
			params:  ConnectParams{Host: "github.com"},
			wantErr: nil,
		},
		{
			name:    "an enterprise host with no kind",
			params:  ConnectParams{Host: "ghe.example.com", Token: NewSecret("tok")},
			wantErr: ErrKindUnknownForHost,
		},
		{
			name: "a kind this build does not have",
			params: ConnectParams{
				Host:  "ghe.example.com",
				Kind:  "gitea",
				Token: NewSecret("tok"),
			},
			wantErr: ErrUnknownKind,
		},
		{
			name:      "a token the forge refuses",
			verifyErr: ErrUnauthorized,
			params:    ConnectParams{Host: "github.com", Token: NewSecret("bad")},
			wantErr:   ErrUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, tt.login, tt.verifyErr)

			_, err := f.service.Connect(t.Context(), tt.params)
			require.Error(t, err)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			}

			listed, err := f.store.List(t.Context())
			require.NoError(t, err)
			assert.Empty(t, listed)
		})
	}
}

// TestAPIURLFor covers where the API is looked for: nowhere special for a public host, the
// self-hosted convention for any other, an https address as given, and anything else refused.
func TestAPIURLFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		kind    string
		host    string
		given   string
		want    string
		wantErr error
	}{
		{
			name: "github.com needs nothing",
			kind: KindGitHub,
			host: "github.com",
			want: "",
		},
		{
			name: "gitlab.com needs nothing",
			kind: KindGitLab,
			host: "gitlab.com",
			want: "",
		},
		{
			name: "an enterprise github host derives api/v3",
			kind: KindGitHub,
			host: "ghe.example.com",
			want: "https://ghe.example.com/api/v3",
		},
		{
			name: "a self-hosted gitlab derives api/v4",
			kind: KindGitLab,
			host: "git.example.com",
			want: "https://git.example.com/api/v4",
		},
		{
			name:  "an https address is kept without its trailing slash",
			kind:  KindGitHub,
			host:  "ghe.example.com",
			given: "https://api.ghe.example.com/",
			want:  "https://api.ghe.example.com",
		},
		{
			name:    "an http address is refused",
			kind:    KindGitHub,
			host:    "ghe.example.com",
			given:   "http://ghe.example.com/api/v3",
			wantErr: ErrInsecureAPI,
		},
		{
			name:    "an address with no host is refused",
			kind:    KindGitHub,
			host:    "ghe.example.com",
			given:   "https:///api/v3",
			wantErr: ErrInsecureAPI,
		},
		{
			name:    "not a url is refused",
			kind:    KindGitHub,
			host:    "ghe.example.com",
			given:   "::nope::",
			wantErr: ErrInsecureAPI,
		},
		{
			name:    "an unknown kind on an unknown host is refused",
			kind:    "gitea",
			host:    "git.example.com",
			wantErr: ErrUnknownKind,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := APIURLFor(tt.kind, tt.host, tt.given)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestConnectSendsTheTokenWhereTheHostSays covers an enterprise connect: the probe is built
// for the derived address and never for the public forge, and the row records that address.
func TestConnectSendsTheTokenWhereTheHostSays(t *testing.T) {
	f := newFixture(t, "harrisoncramer", nil)

	connection, err := f.service.Connect(t.Context(), ConnectParams{
		Host:  "ghe.example.com",
		Kind:  KindGitHub,
		Token: NewSecret("tok"),
	})
	require.NoError(t, err)
	assert.Equal(t, "https://ghe.example.com/api/v3", connection.APIURL)
	assert.Equal(t, []string{"https://ghe.example.com/api/v3"}, f.apis)

	_, err = f.service.Connect(t.Context(), ConnectParams{
		Host:   "other.example.com",
		Kind:   KindGitHub,
		APIURL: "http://other.example.com/api/v3",
		Token:  NewSecret("tok"),
	})
	require.ErrorIs(t, err, ErrInsecureAPI)
	assert.Len(t, f.apis, 1, "an insecure address is refused before any token is sent")
}

// TestConnectRefusesAConnectedHost covers the second connect to one host.
func TestConnectRefusesAConnectedHost(t *testing.T) {
	f := newFixture(t, "harrisoncramer", nil)
	ctx := t.Context()

	_, err := f.service.Connect(ctx, ConnectParams{Host: "github.com", Token: NewSecret("one")})
	require.NoError(t, err)

	_, err = f.service.Connect(ctx, ConnectParams{Host: "github.com", Token: NewSecret("two")})
	require.ErrorIs(t, err, forgestore.ErrHostTaken)
	assert.Len(t, f.seen, 1, "the second token is never sent anywhere")
}

// TestDisconnect covers the row and the file going together, and a host that was never
// connected.
func TestDisconnect(t *testing.T) {
	f := newFixture(t, "harrisoncramer", nil)
	ctx := t.Context()

	connection, err := f.service.Connect(ctx, ConnectParams{Host: "github.com", Token: NewSecret("tok")})
	require.NoError(t, err)

	require.NoError(t, f.service.Disconnect(ctx, "github.com"))

	_, err = f.store.GetByHost(ctx, "github.com")
	require.ErrorIs(t, err, store.ErrNotFound)
	_, err = f.credentials.Get(ctx, connection.ID)
	require.ErrorIs(t, err, ErrNoCredential)

	require.ErrorIs(t, f.service.Disconnect(ctx, "github.com"), store.ErrNotFound)
}

// TestHasCredential covers a row whose file is there, and one whose file is gone.
func TestHasCredential(t *testing.T) {
	f := newFixture(t, "harrisoncramer", nil)
	ctx := t.Context()

	connection, err := f.service.Connect(ctx, ConnectParams{Host: "github.com", Token: NewSecret("tok")})
	require.NoError(t, err)

	held, err := f.service.HasCredential(ctx, *connection)
	require.NoError(t, err)
	assert.True(t, held)

	require.NoError(t, f.credentials.Delete(ctx, connection.ID))
	held, err = f.service.HasCredential(ctx, *connection)
	require.NoError(t, err)
	assert.False(t, held)

	f2, err := f.service.ForConnection(*connection)
	require.NoError(t, err)
	_, err = f2.Verify(ctx)
	require.ErrorIs(t, err, ErrNoCredential)
}

// TestForConnectionRecordsWhereItIsTold covers Verified: a service built with NoRecord leaves
// the row alone when the forge refuses, and one built without it clears verified_at.
func TestForConnectionRecordsWhereItIsTold(t *testing.T) {
	f := newFixture(t, "harrisoncramer", nil)
	ctx := t.Context()

	connection, err := f.service.Connect(ctx, ConnectParams{Host: "github.com", Token: NewSecret("tok")})
	require.NoError(t, err)

	reporting := NewForgeService(NewForgeServiceParams{
		Connections: f.store,
		Credentials: f.credentials,
		Builders:    map[string]Builder{KindGitHub: func(_ string, auth Authenticator) Forge { return &fakeForge{auth: auth, err: ErrUnauthorized} }},
		Verified:    NoRecord{},
	})
	reported, err := reporting.ForConnection(*connection)
	require.NoError(t, err)
	require.NoError(t, reported.(*fakeForge).auth.Invalidate(ctx))

	untouched, err := f.store.Get(ctx, connection.ID)
	require.NoError(t, err)
	assert.NotNil(t, untouched.VerifiedAt)

	recording, err := f.service.ForConnection(*connection)
	require.NoError(t, err)
	require.NoError(t, recording.(*fakeForge).auth.Invalidate(ctx))

	cleared, err := f.store.Get(ctx, connection.ID)
	require.NoError(t, err)
	assert.Nil(t, cleared.VerifiedAt)
}

// appServer is a GitHub that mints an installation token and describes the App, counting the
// mints so a test can say whether the cache was used.
func appServer(t *testing.T, tls bool) (*httptest.Server, *int) {
	t.Helper()

	mints := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/access_tokens"):
			mints++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"ghs_minted","expires_at":"` + time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"}`))
		case r.URL.Path == "/app":
			_, _ = w.Write([]byte(`{"slug":"weave-engine"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	server := httptest.NewServer(handler)
	if tls {
		server = httptest.NewTLSServer(handler)
	}
	t.Cleanup(server.Close)

	return server, &mints
}

func withApp(t *testing.T, f *fixture, server *httptest.Server) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	keyPath := filepath.Join(t.TempDir(), "app.pem")
	require.NoError(t, os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600))

	f.app = App{
		Cache: githubapp.Cache{
			Path: filepath.Join(t.TempDir(), "token"),
			Identity: githubapp.Identity{
				ID:             "1",
				InstallationID: "2",
				PrivateKeyPath: keyPath,
				API:            server.URL,
			},
		},
		Client: server.Client(),
	}
	f.service.loadApp = func() (App, error) {
		return f.app, nil
	}
}

// TestConnectWithTheApp covers the engine's path: no token is pasted, the App mints, the
// installation is checked to reach something, the row records the App's name, and no
// credential file is written.
func TestConnectWithTheApp(t *testing.T) {
	f := newFixture(t, "", nil)
	server, mints := appServer(t, false)
	withApp(t, f, server)
	ctx := t.Context()

	connection, err := f.service.Connect(ctx, ConnectParams{Host: "github.com", Method: MethodGitHubApp})
	require.NoError(t, err)
	assert.Equal(t, MethodGitHubApp, connection.Method)
	assert.Equal(t, "weave-engine", connection.Login)
	assert.Equal(t, 1, *mints)
	require.Len(t, f.seen, 1)
	assert.Equal(t, "ghs_minted", f.seen[0].Token.Expose())
	assert.False(t, f.seen[0].ExpiresAt.IsZero())

	_, err = f.credentials.Get(ctx, connection.ID)
	require.ErrorIs(t, err, ErrNoCredential)
	held, err := f.service.HasCredential(ctx, *connection)
	require.NoError(t, err)
	assert.True(t, held)

	again, err := f.service.ForConnection(*connection)
	require.NoError(t, err)
	_, err = again.(*fakeForge).auth.Credential(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, *mints, "the cached token serves the next call")

	require.NoError(t, again.(*fakeForge).auth.Invalidate(ctx))
	_, err = again.(*fakeForge).auth.Credential(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, *mints, "a refusal drops the cache so the next call mints")
	cleared, err := f.store.Get(ctx, connection.ID)
	require.NoError(t, err)
	assert.Nil(t, cleared.VerifiedAt)
}

// TestConnectWithTheAppSpeaksToTheEnterpriseHost covers an App row on a GitHub Enterprise
// host: the mint and the describe go to that host's API, not to api.github.com.
func TestConnectWithTheAppSpeaksToTheEnterpriseHost(t *testing.T) {
	f := newFixture(t, "", nil)
	server, mints := appServer(t, true)
	withApp(t, f, server)
	f.app.Cache.Identity.API = ""

	_, err := f.service.Connect(t.Context(), ConnectParams{
		Host:   "ghe.example.com",
		Kind:   KindGitHub,
		Method: MethodGitHubApp,
		APIURL: server.URL,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, *mints, "the mint reached the address the connection names")
	assert.Equal(t, []string{server.URL}, f.apis)
}

// TestConnectWithTheAppRefusals covers no App configured, an App the config cannot be read
// as, an App on a forge that is not GitHub, and an App installed on nothing, none of which
// leaves a row.
func TestConnectWithTheAppRefusals(t *testing.T) {
	t.Run("no app configured", func(t *testing.T) {
		f := newFixture(t, "", nil)

		_, err := f.service.Connect(t.Context(), ConnectParams{Host: "github.com", Method: MethodGitHubApp})
		require.ErrorIs(t, err, ErrNoApp)
	})

	t.Run("an app the config cannot be read as", func(t *testing.T) {
		f := newFixture(t, "", nil)
		f.service.loadApp = func() (App, error) {
			return App{}, errors.New("installation_id: not a number")
		}

		_, err := f.service.Connect(t.Context(), ConnectParams{Host: "github.com", Method: MethodGitHubApp})
		require.ErrorIs(t, err, ErrBadApp)
		assert.Contains(t, err.Error(), "installation_id")
	})

	t.Run("an app reaching no repository", func(t *testing.T) {
		f := newFixture(t, "", nil)
		server, _ := appServer(t, false)
		withApp(t, f, server)
		f.service.builders[KindGitHub] = func(_ string, auth Authenticator) Forge {
			return &fakeForge{auth: auth, reaches: 0}
		}

		_, err := f.service.Connect(t.Context(), ConnectParams{Host: "github.com", Method: MethodGitHubApp})
		require.ErrorIs(t, err, ErrAppReachesNothing)
	})

	t.Run("a way to connect that does not exist", func(t *testing.T) {
		f := newFixture(t, "", nil)

		_, err := f.service.Connect(t.Context(), ConnectParams{Host: "github.com", Method: "oauth"})
		require.Error(t, err)
	})

}

// TestCheck covers what the doctor reads per row: a token row is asked who it is, an App row
// is asked what its installation reaches and answers with the login the row holds, a row whose
// file is gone says so without a request, and an App reaching nothing is refused.
func TestCheck(t *testing.T) {
	f := newFixture(t, "harrisoncramer", nil)
	ctx := t.Context()

	token, err := f.service.Connect(ctx, ConnectParams{Host: "github.com", Token: NewSecret("tok")})
	require.NoError(t, err)
	login, err := f.service.Check(ctx, *token)
	require.NoError(t, err)
	assert.Equal(t, "harrisoncramer", login)

	require.NoError(t, f.credentials.Delete(ctx, token.ID))
	_, err = f.service.Check(ctx, *token)
	require.ErrorIs(t, err, ErrNoCredential)
	require.NoError(t, f.service.Disconnect(ctx, "github.com"))

	server, _ := appServer(t, false)
	withApp(t, f, server)
	app, err := f.service.Connect(ctx, ConnectParams{Host: "github.com", Method: MethodGitHubApp})
	require.NoError(t, err)
	seenBefore := len(f.seen)
	login, err = f.service.Check(ctx, *app)
	require.NoError(t, err)
	assert.Equal(t, "weave-engine", login)
	assert.Len(t, f.seen, seenBefore+1, "the App row is checked through its installation, not /user")

	f.service.builders[KindGitHub] = func(_ string, auth Authenticator) Forge {
		return &fakeForge{
			auth:    auth,
			login:   "should-not-be-read",
			reaches: 0,
		}
	}
	_, err = f.service.Check(ctx, *app)
	require.ErrorIs(t, err, ErrAppReachesNothing)
}

// TestCheckReportsARefusedMint covers the App's key or installation no longer being accepted:
// GitHub's 401 or 404 on the mint comes back as a refusal, and the row's verified_at is cleared.
func TestCheckReportsARefusedMint(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			f := newFixture(t, "", nil)
			server, _ := appServer(t, false)
			withApp(t, f, server)
			ctx := t.Context()

			app, err := f.service.Connect(ctx, ConnectParams{Host: "github.com", Method: MethodGitHubApp})
			require.NoError(t, err)
			row, err := f.store.Get(ctx, app.ID)
			require.NoError(t, err)
			require.NotNil(t, row.VerifiedAt)

			refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"message":"A JSON web token could not be decoded"}`))
			}))
			t.Cleanup(refusing.Close)
			f.app.Cache.Identity.API = refusing.URL
			require.NoError(t, f.app.Cache.Forget())

			_, err = f.service.Check(ctx, *app)
			require.ErrorIs(t, err, ErrUnauthorized)
			assert.Contains(t, err.Error(), "A JSON web token could not be decoded")

			row, err = f.store.Get(ctx, app.ID)
			require.NoError(t, err)
			assert.Nil(t, row.VerifiedAt, "a refused mint clears verified_at the way a refused request does")
		})
	}
}

// TestCheckPassesAMintOutageThrough covers GitHub failing to mint rather than refusing to: a
// 5xx is not a refusal.
func TestCheckPassesAMintOutageThrough(t *testing.T) {
	f := newFixture(t, "", nil)
	server, _ := appServer(t, false)
	withApp(t, f, server)
	ctx := t.Context()

	app, err := f.service.Connect(ctx, ConnectParams{Host: "github.com", Method: MethodGitHubApp})
	require.NoError(t, err)

	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(down.Close)
	f.app.Cache.Identity.API = down.URL
	require.NoError(t, f.app.Cache.Forget())

	_, err = f.service.Check(ctx, *app)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrUnauthorized)

	row, err := f.store.Get(ctx, app.ID)
	require.NoError(t, err)
	assert.NotNil(t, row.VerifiedAt)
}
