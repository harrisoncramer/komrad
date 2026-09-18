package forge

import (
	"context"
	"testing"
	"time"

	"weave/internal/dbtest"
	"weave/internal/git"
	"weave/internal/store"
	forgestore "weave/internal/store/forge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeForge answers Verify from what it was built with and records the credential it was
// given.
type fakeForge struct {
	auth  Authenticator
	login string
	err   error
}

func (f *fakeForge) Kind() string { return KindGitHub }

func (f *fakeForge) Verify(ctx context.Context) (string, error) {
	if _, err := f.auth.Credential(ctx); err != nil {
		return "", err
	}

	return f.login, f.err
}

func (f *fakeForge) ListOnBranch(context.Context, git.RemoteURL, string) ([]PullRequest, error) {
	return nil, nil
}

func (f *fakeForge) Get(context.Context, string) (PullRequest, error) { return PullRequest{}, nil }

type fixture struct {
	service *Service
	store   *forgestore.Store
	seen    []Credential
}

func newFixture(t *testing.T, login string, verifyErr error) *fixture {
	t.Helper()

	pool, err := store.Open(t.Context(), dbtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	f := &fixture{store: forgestore.New(pool.GetDB())}
	f.service = NewService(NewServiceParams{
		Connections: f.store,
		StateDir:    t.TempDir(),
		Builders: map[string]Builder{
			KindGitHub: func(_ string, auth Authenticator) Forge {
				credential, _ := auth.Credential(t.Context())
				f.seen = append(f.seen, credential)

				return &fakeForge{
					auth:  auth,
					login: login,
					err:   verifyErr,
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

	connection, err := f.service.Connect(ctx, ConnectParams{Host: "GitHub.com", Token: " ghp_secret\n"})
	require.NoError(t, err)
	assert.Equal(t, "github.com", connection.Host)
	assert.Equal(t, KindGitHub, connection.Kind)
	assert.Equal(t, MethodToken, connection.Method)
	assert.Equal(t, "harrisoncramer", connection.Login)
	require.NotNil(t, connection.VerifiedAt)

	require.Len(t, f.seen, 1)
	assert.Equal(t, Credential{Header: HeaderBearer, Token: "ghp_secret"}, f.seen[0])

	token, err := ReadCredential(f.service.stateDir, connection.ID)
	require.NoError(t, err)
	assert.Equal(t, "ghp_secret", token)

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
			params:  ConnectParams{Token: "tok"},
			wantErr: nil,
		},
		{
			name:    "no token",
			params:  ConnectParams{Host: "github.com"},
			wantErr: nil,
		},
		{
			name:    "an enterprise host with no kind",
			params:  ConnectParams{Host: "ghe.example.com", Token: "tok"},
			wantErr: ErrKindUnknownForHost,
		},
		{
			name: "a kind this build does not have",
			params: ConnectParams{
				Host:  "ghe.example.com",
				Kind:  "gitea",
				Token: "tok",
			},
			wantErr: ErrUnknownKind,
		},
		{
			name:      "a token the forge refuses",
			verifyErr: ErrUnauthorized,
			params:    ConnectParams{Host: "github.com", Token: "bad"},
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

// TestConnectRefusesAConnectedHost covers the second connect to one host.
func TestConnectRefusesAConnectedHost(t *testing.T) {
	f := newFixture(t, "harrisoncramer", nil)
	ctx := t.Context()

	_, err := f.service.Connect(ctx, ConnectParams{Host: "github.com", Token: "one"})
	require.NoError(t, err)

	_, err = f.service.Connect(ctx, ConnectParams{Host: "github.com", Token: "two"})
	require.ErrorIs(t, err, forgestore.ErrHostTaken)
	assert.Len(t, f.seen, 1, "the second token is never sent anywhere")
}

// TestDisconnect covers the row and the file going together, and a host that was never
// connected.
func TestDisconnect(t *testing.T) {
	f := newFixture(t, "harrisoncramer", nil)
	ctx := t.Context()

	connection, err := f.service.Connect(ctx, ConnectParams{Host: "github.com", Token: "tok"})
	require.NoError(t, err)

	require.NoError(t, f.service.Disconnect(ctx, "github.com"))

	_, err = f.store.GetByHost(ctx, "github.com")
	require.ErrorIs(t, err, store.ErrNotFound)
	_, err = ReadCredential(f.service.stateDir, connection.ID)
	require.ErrorIs(t, err, ErrNoCredential)

	require.ErrorIs(t, f.service.Disconnect(ctx, "github.com"), store.ErrNotFound)
}

// TestVerifyRestoresAndClearsTheStamp covers the doctor's read: a forge that answers stamps
// the row, and one that refuses through the transport clears it.
func TestVerifyRestoresAndClearsTheStamp(t *testing.T) {
	f := newFixture(t, "harrisoncramer", nil)
	ctx := t.Context()

	connection, err := f.service.Connect(ctx, ConnectParams{Host: "github.com", Token: "tok"})
	require.NoError(t, err)
	require.NoError(t, f.store.SetVerifiedAt(ctx, connection.ID, nil))

	login, err := f.service.Verify(ctx, *connection)
	require.NoError(t, err)
	assert.Equal(t, "harrisoncramer", login)

	restored, err := f.store.Get(ctx, connection.ID)
	require.NoError(t, err)
	assert.NotNil(t, restored.VerifiedAt)

	require.NoError(t, RemoveCredential(f.service.stateDir, connection.ID))
	_, err = f.service.Verify(ctx, *connection)
	require.ErrorIs(t, err, ErrNoCredential)
}
