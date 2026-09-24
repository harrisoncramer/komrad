package forge

import (
	"os"
	"testing"

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
