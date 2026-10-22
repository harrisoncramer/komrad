package forge

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFileStoreRoundTrip covers the file: written with a mode only the owner can read, read
// back trimmed, replaced whole, and gone after deletion.
func TestFileStoreRoundTrip(t *testing.T) {
	t.Parallel()

	store := NewFileStore(t.TempDir())
	ctx := t.Context()
	connectionID := uuid.New()
	require.NoError(t, store.Set(ctx, connectionID, NewSecret("ghp_secret")))

	info, err := os.Stat(store.Path(connectionID))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	token, err := store.Get(ctx, connectionID)
	require.NoError(t, err)
	assert.Equal(t, "ghp_secret", token.Expose())

	require.NoError(t, store.Set(ctx, connectionID, NewSecret("ghp_replaced")))
	token, err = store.Get(ctx, connectionID)
	require.NoError(t, err)
	assert.Equal(t, "ghp_replaced", token.Expose())
	entries, err := os.ReadDir(filepath.Dir(store.Path(connectionID)))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "a replaced token leaves no temporary file behind")

	require.NoError(t, store.Delete(ctx, connectionID))
	_, err = store.Get(ctx, connectionID)
	require.ErrorIs(t, err, ErrNoCredential)

	require.NoError(t, store.Delete(ctx, connectionID))
}

// TestFileStoreRefusesAnEmptyFile covers a file that exists and holds nothing.
func TestFileStoreRefusesAnEmptyFile(t *testing.T) {
	t.Parallel()

	store := NewFileStore(t.TempDir())
	connectionID := uuid.New()
	require.NoError(t, os.MkdirAll(filepath.Dir(store.Path(connectionID)), 0o700))
	require.NoError(t, os.WriteFile(store.Path(connectionID), []byte("   \n"), 0o600))

	_, err := store.Get(t.Context(), connectionID)
	require.ErrorIs(t, err, ErrNoCredential)
}

// TestCredentialHeaderValue covers the two ways a token goes on the wire.
func TestCredentialHeaderValue(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Bearer tok", Credential{Header: HeaderBearer, Token: NewSecret("tok")}.HeaderValue())
	assert.Equal(t, "tok", Credential{Header: HeaderPrivateToken, Token: NewSecret("tok")}.HeaderValue())
}
