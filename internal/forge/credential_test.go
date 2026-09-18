package forge

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFileStoreRoundTrip covers the file: written with a mode only the owner can read, read
// back trimmed, and gone after deletion.
func TestFileStoreRoundTrip(t *testing.T) {
	t.Parallel()

	store := NewFileStore(t.TempDir())
	ctx := t.Context()
	require.NoError(t, store.Set(ctx, "conn-1", NewSecret("ghp_secret")))

	info, err := os.Stat(store.Path("conn-1"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	token, err := store.Get(ctx, "conn-1")
	require.NoError(t, err)
	assert.Equal(t, "ghp_secret", token.Expose())

	require.NoError(t, store.Delete(ctx, "conn-1"))
	_, err = store.Get(ctx, "conn-1")
	require.ErrorIs(t, err, ErrNoCredential)

	require.NoError(t, store.Delete(ctx, "conn-1"))
}

// TestFileStoreRefusesAnEmptyFile covers a file that exists and holds nothing.
func TestFileStoreRefusesAnEmptyFile(t *testing.T) {
	t.Parallel()

	store := NewFileStore(t.TempDir())
	require.NoError(t, os.MkdirAll(store.Path(""), 0o700))
	require.NoError(t, os.WriteFile(store.Path("conn-1"), []byte("   \n"), 0o600))

	_, err := store.Get(t.Context(), "conn-1")
	require.ErrorIs(t, err, ErrNoCredential)
}

// TestCredentialHeaderValue covers the two ways a token goes on the wire.
func TestCredentialHeaderValue(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Bearer tok", Credential{Header: HeaderBearer, Token: NewSecret("tok")}.HeaderValue())
	assert.Equal(t, "tok", Credential{Header: HeaderPrivateToken, Token: NewSecret("tok")}.HeaderValue())
}

type recordingVerified struct {
	id string
	at *time.Time
}

func (r *recordingVerified) SetVerifiedAt(_ context.Context, id string, at *time.Time) error {
	r.id, r.at = id, at

	return nil
}

// TestStoredCredential covers the Authenticator over the store: it answers with the header it
// was built for, and Invalidate clears the connection's verified_at.
func TestStoredCredential(t *testing.T) {
	t.Parallel()

	store := NewFileStore(t.TempDir())
	require.NoError(t, store.Set(t.Context(), "conn-1", NewSecret("glpat-secret")))

	connections := &recordingVerified{}
	auth := NewStoredCredential(NewStoredCredentialParams{
		Store:        store,
		ConnectionID: "conn-1",
		Header:       HeaderPrivateToken,
		Connections:  connections,
	})

	credential, err := auth.Credential(t.Context())
	require.NoError(t, err)
	assert.Equal(t, Credential{Header: HeaderPrivateToken, Token: NewSecret("glpat-secret")}, credential)

	require.NoError(t, auth.Invalidate(t.Context()))
	assert.Equal(t, "conn-1", connections.id)
	assert.Nil(t, connections.at)
}
