package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnsureTenantIDMintsOnceAndKeepsIt pins an engine keeping the id it minted on its first start.
func TestEnsureTenantIDMintsOnceAndKeepsIt(t *testing.T) {
	t.Parallel()

	env := &Environment{StateDir: filepath.Join(t.TempDir(), "state")}

	first, err := env.EnsureTenantID()
	require.NoError(t, err)
	_, err = uuid.Parse(first)
	require.NoError(t, err)

	again, err := env.EnsureTenantID()
	require.NoError(t, err)
	assert.Equal(t, first, again)

	read, err := env.GetTenantID()
	require.NoError(t, err)
	assert.Equal(t, first, read)
}

// TestGetTenantIDNeverMints pins every reader but the service leaving a never-started engine alone.
func TestGetTenantIDNeverMints(t *testing.T) {
	t.Parallel()

	env := &Environment{StateDir: filepath.Join(t.TempDir(), "state")}

	_, err := env.GetTenantID()

	require.ErrorIs(t, err, ErrNoTenantID)
	assert.NoDirExists(t, env.StateDir)
}

// TestGetTenantIDRefusesAnIDThatIsNotAUUID covers a hand-edited file.
func TestGetTenantIDRefusesAnIDThatIsNotAUUID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, tenantFile), []byte("acme\n"), 0o600))

	_, err := (&Environment{StateDir: dir}).EnsureTenantID()

	require.Error(t, err)
	assert.Contains(t, err.Error(), tenantFile)
}
