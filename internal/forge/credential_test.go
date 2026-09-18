package forge

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCredentialFileRoundTrip covers the file: written with a mode only the owner can read,
// read back trimmed, and gone after removal.
func TestCredentialFileRoundTrip(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	require.NoError(t, WriteCredential(stateDir, "conn-1", "ghp_secret"))

	info, err := os.Stat(CredentialPath(stateDir, "conn-1"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	token, err := ReadCredential(stateDir, "conn-1")
	require.NoError(t, err)
	assert.Equal(t, "ghp_secret", token)

	require.NoError(t, RemoveCredential(stateDir, "conn-1"))
	_, err = ReadCredential(stateDir, "conn-1")
	require.ErrorIs(t, err, ErrNoCredential)

	require.NoError(t, RemoveCredential(stateDir, "conn-1"))
}

// TestReadCredentialRefusesAnEmptyFile covers a file that exists and holds nothing.
func TestReadCredentialRefusesAnEmptyFile(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	require.NoError(t, WriteCredential(stateDir, "conn-1", "   "))

	_, err := ReadCredential(stateDir, "conn-1")
	require.ErrorIs(t, err, ErrNoCredential)
}

// TestCredentialHeaderValue covers the two ways a token goes on the wire.
func TestCredentialHeaderValue(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Bearer tok", Credential{Header: HeaderBearer, Token: "tok"}.HeaderValue())
	assert.Equal(t, "tok", Credential{Header: HeaderPrivateToken, Token: "tok"}.HeaderValue())
}

type recordingVerified struct {
	id string
	at *time.Time
}

func (r *recordingVerified) SetVerifiedAt(_ context.Context, id string, at *time.Time) error {
	r.id, r.at = id, at

	return nil
}

// TestFileCredential covers the Authenticator over a file: it answers with the header it was
// built for, and Invalidate clears the connection's verified_at.
func TestFileCredential(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	require.NoError(t, WriteCredential(stateDir, "conn-1", "glpat-secret"))

	connections := &recordingVerified{}
	auth := NewFileCredential(NewFileCredentialParams{
		StateDir:     stateDir,
		ConnectionID: "conn-1",
		Header:       HeaderPrivateToken,
		Connections:  connections,
	})

	credential, err := auth.Credential(t.Context())
	require.NoError(t, err)
	assert.Equal(t, Credential{Header: HeaderPrivateToken, Token: "glpat-secret"}, credential)

	require.NoError(t, auth.Invalidate(t.Context()))
	assert.Equal(t, "conn-1", connections.id)
	assert.Nil(t, connections.at)
}
