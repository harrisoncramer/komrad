package forge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"komrad/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gitLabAccount is the one personal token a fake GitLab knows, shared by every forge built
// over it.
type gitLabAccount struct {
	token     string
	scopes    []string
	expiresAt time.Time
	rotateErr error
	revokeErr error
	issued    int
	rotatedTo []time.Time
	revoked   int
	seen      []Credential
}

// fakeGitLab answers for a personal token the way GitLab does, refusing any token but the
// live one.
type fakeGitLab struct {
	fakeForge
	account *gitLabAccount
}

func (f *fakeGitLab) Kind() string {
	return KindGitLab
}

func (f *fakeGitLab) Verify(ctx context.Context) (string, error) {
	if err := f.authorize(ctx); err != nil {
		return "", err
	}

	return f.login, nil
}

func (f *fakeGitLab) ReadToken(ctx context.Context) (*TokenInfo, error) {
	if err := f.authorize(ctx); err != nil {
		return nil, err
	}

	return &TokenInfo{Scopes: f.account.scopes, ExpiresAt: f.account.expiresAt}, nil
}

func (f *fakeGitLab) RotateToken(ctx context.Context, expiresAt time.Time) (Secret, error) {
	if err := f.authorize(ctx); err != nil {
		return Secret{}, err
	}
	if f.account.rotateErr != nil {
		if errors.Is(f.account.rotateErr, ErrUnauthorized) {
			return Secret{}, errors.Join(f.account.rotateErr, f.auth.Invalidate(ctx))
		}

		return Secret{}, f.account.rotateErr
	}

	f.account.issued++
	f.account.token = fmt.Sprintf("glpat-%d", f.account.issued)
	f.account.expiresAt = expiresAt
	f.account.rotatedTo = append(f.account.rotatedTo, expiresAt)

	return NewSecret(f.account.token), nil
}

func (f *fakeGitLab) RevokeToken(ctx context.Context) error {
	if err := f.authorize(ctx); err != nil {
		return err
	}
	if f.account.revokeErr != nil {
		return f.account.revokeErr
	}

	f.account.token = ""
	f.account.revoked++

	return nil
}

// authorize refuses a credential that is not the live token, clearing the stamp as the
// transport does.
func (f *fakeGitLab) authorize(ctx context.Context) error {
	credential, err := f.auth.Credential(ctx)
	if err != nil {
		return err
	}
	f.account.seen = append(f.account.seen, credential)

	if credential.Token.Expose() != f.account.token {
		return errors.Join(ErrUnauthorized.Saying("401 Unauthorized"), f.auth.Invalidate(ctx))
	}

	return nil
}

// newGitLabFixture is a fixture whose gitlab.com answers for account.
func newGitLabFixture(t *testing.T, account *gitLabAccount) *fixture {
	t.Helper()

	f := newFixture(t, "harrisoncramer", nil)
	f.service.builders[KindGitLab] = func(_ string, auth Authenticator) Forge {
		return &fakeGitLab{fakeForge: fakeForge{auth: auth, login: "harrisoncramer"}, account: account}
	}

	return f
}

// pastedToken is a fresh GitLab account holding the token a person pastes.
func pastedToken() *gitLabAccount {
	return &gitLabAccount{
		token:     "glpat-pasted",
		scopes:    []string{"api", "write_repository"},
		expiresAt: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
	}
}

// connectRotating connects gitlab.com with the pasted token.
func connectRotating(t *testing.T, f *fixture) error {
	t.Helper()

	_, err := f.service.Connect(t.Context(), ConnectParams{Host: "gitlab.com", Method: MethodRotatingToken, Token: NewSecret("glpat-pasted")})

	return err
}

// TestConnectWithARotatingToken covers the pasted token traded at once for one that expires two
// days out, and only the new one kept.
func TestConnectWithARotatingToken(t *testing.T) {
	account := pastedToken()
	f := newGitLabFixture(t, account)
	ctx := t.Context()

	err := connectRotating(t, f)
	require.NoError(t, err)

	connection, err := f.store.GetByHost(ctx, "gitlab.com")
	require.NoError(t, err)
	assert.Equal(t, KindGitLab, connection.Kind)
	assert.Equal(t, MethodRotatingToken, connection.Method)
	assert.Equal(t, "harrisoncramer", connection.Login)
	require.NotNil(t, connection.VerifiedAt)

	assert.Equal(t, []time.Time{time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)}, account.rotatedTo)
	token, err := f.credentials.Get(ctx, connection.ID)
	require.NoError(t, err)
	assert.Equal(t, "glpat-1", token.Expose())

	for _, credential := range account.seen {
		assert.Equal(t, HeaderPrivateToken, credential.Header)
	}

	login, err := f.service.Check(ctx, *connection)
	require.NoError(t, err)
	assert.Equal(t, "harrisoncramer", login)
	assert.Equal(t, HeaderPrivateToken, account.seen[len(account.seen)-1].Header)
}

// TestConnectWithARotatingTokenRefusals covers what a rotating token will not connect with,
// none of which leaves a row or a file behind.
func TestConnectWithARotatingTokenRefusals(t *testing.T) {
	tests := []struct {
		name        string
		host        string
		scopes      []string
		rotateErr   error
		wantErr     error
		wantRotated int
	}{
		{
			name:    "a github host",
			host:    "github.com",
			scopes:  []string{"api"},
			wantErr: ErrRotatingNotGitLab,
		},
		{
			name:    "a token without the api scope",
			host:    "gitlab.com",
			scopes:  []string{"read_api", "write_repository"},
			wantErr: ErrTokenCannotRotate,
		},
		{
			name:      "a rotation gitlab refuses",
			host:      "gitlab.com",
			scopes:    []string{"api"},
			rotateErr: errors.New("500 Internal Server Error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := pastedToken()
			account.scopes = tt.scopes
			account.rotateErr = tt.rotateErr
			f := newGitLabFixture(t, account)
			ctx := t.Context()

			_, err := f.service.Connect(ctx, ConnectParams{Host: tt.host, Method: MethodRotatingToken, Token: NewSecret("glpat-pasted")})
			require.Error(t, err)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			}
			assert.Empty(t, account.rotatedTo)

			listed, err := f.store.List(ctx)
			require.NoError(t, err)
			assert.Empty(t, listed)

			files, err := os.ReadDir(f.credentials.Path(""))
			if !errors.Is(err, os.ErrNotExist) {
				require.NoError(t, err)
			}
			assert.Empty(t, files)
		})
	}
}

// TestCheckRotatesADueToken covers the rotation rule: a token expiring tomorrow or sooner is
// traded for one two days out, and any other is left alone.
func TestCheckRotatesADueToken(t *testing.T) {
	connectedExpiry := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		expiresAt time.Time
		wantToken string
	}{
		{
			name:      "expiring today",
			expiresAt: time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC),
			wantToken: "glpat-2",
		},
		{
			name:      "expiring tomorrow",
			expiresAt: time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC),
			wantToken: "glpat-2",
		},
		{
			name:      "expiring the day after",
			expiresAt: connectedExpiry,
			wantToken: "glpat-1",
		},
		{
			name:      "never expiring",
			wantToken: "glpat-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := pastedToken()
			f := newGitLabFixture(t, account)
			ctx := t.Context()

			err := connectRotating(t, f)
			require.NoError(t, err)
			account.expiresAt = tt.expiresAt

			connection, err := f.store.GetByHost(ctx, "gitlab.com")
			require.NoError(t, err)
			_, err = f.service.Check(ctx, *connection)
			require.NoError(t, err)

			token, err := f.credentials.Get(ctx, connection.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantToken, token.Expose())
			assert.Equal(t, tt.wantToken, account.token)
			assert.Equal(t, connectedExpiry, account.rotatedTo[len(account.rotatedTo)-1])
		})
	}
}

// TestRotateTokens covers the loop's pass: a due token rotated and the stamp restored, a
// rotation GitLab refuses leaving the connection unverified, and a plain token not checked.
func TestRotateTokens(t *testing.T) {
	checkedAt := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name         string
		rotateErr    error
		wantErr      error
		wantToken    string
		wantVerified *time.Time
	}{
		{
			name:         "a due token is rotated",
			wantToken:    "glpat-2",
			wantVerified: &checkedAt,
		},
		{
			name:      "a rotation that answers 401 leaves the connection unverified",
			rotateErr: ErrUnauthorized.Saying("401 Unauthorized"),
			wantErr:   ErrUnauthorized,
			wantToken: "glpat-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := pastedToken()
			f := newGitLabFixture(t, account)
			ctx := t.Context()

			err := connectRotating(t, f)
			require.NoError(t, err)
			_, err = f.service.Connect(ctx, ConnectParams{Host: "github.com", Token: NewSecret("ghp_plain")})
			require.NoError(t, err)
			builtForGitHub := len(f.seen)

			account.expiresAt = time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
			account.rotateErr = tt.rotateErr

			err = f.service.RotateTokens(ctx)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Len(t, f.seen, builtForGitHub)

			connection, err := f.store.GetByHost(ctx, "gitlab.com")
			require.NoError(t, err)
			assertSameStamp(t, tt.wantVerified, connection.VerifiedAt)

			token, err := f.credentials.Get(ctx, connection.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantToken, token.Expose())
		})
	}
}

// TestDisconnectRevokesARotatingToken covers the token ended at GitLab before the row and file
// go, a token GitLab already refuses let go, and a failed revoke keeping both.
func TestDisconnectRevokesARotatingToken(t *testing.T) {
	tests := []struct {
		name        string
		lostToken   bool
		revokeErr   error
		wantRevoked int
		wantErr     bool
	}{
		{
			name:        "gitlab revokes the stored token",
			wantRevoked: 1,
		},
		{
			name:      "a token gitlab already refuses",
			lostToken: true,
		},
		{
			name:      "gitlab fails to revoke, and the connection goes anyway",
			revokeErr: errors.New("500 Internal Server Error"),
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := pastedToken()
			account.revokeErr = tt.revokeErr
			f := newGitLabFixture(t, account)
			ctx := t.Context()

			err := connectRotating(t, f)
			require.NoError(t, err)
			connection, err := f.store.GetByHost(ctx, "gitlab.com")
			require.NoError(t, err)
			if tt.lostToken {
				account.token = "glpat-elsewhere"
			}

			err = f.service.Disconnect(ctx, "gitlab.com")
			assert.Equal(t, tt.wantRevoked, account.revoked)
			assert.Equal(t, "glpat-1", account.seen[len(account.seen)-1].Token.Expose())

			_, rowErr := f.store.GetByHost(ctx, "gitlab.com")
			_, fileErr := f.credentials.Get(ctx, connection.ID)
			if tt.wantErr {
				require.ErrorContains(t, err, "gitlab.com")
			} else {
				require.NoError(t, err)
			}
			require.ErrorIs(t, rowErr, store.ErrNotFound)
			require.ErrorIs(t, fileErr, ErrNoCredential)
		})
	}
}

// TestHeaderFor covers which header each kind and method sends its token in.
func TestHeaderFor(t *testing.T) {
	tests := []struct {
		kind   string
		method string
		want   string
	}{
		{kind: KindGitHub, method: MethodToken, want: HeaderBearer},
		{kind: KindGitHub, method: MethodGitHubApp, want: HeaderBearer},
		{kind: KindGitLab, method: MethodToken, want: HeaderPrivateToken},
		{kind: KindGitLab, method: MethodRotatingToken, want: HeaderPrivateToken},
	}

	for _, tt := range tests {
		t.Run(tt.kind+" "+tt.method, func(t *testing.T) {
			assert.Equal(t, tt.want, HeaderFor(tt.kind, tt.method))
		})
	}
}
