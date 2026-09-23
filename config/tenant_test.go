package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetTenantIDMintsOnceAndKeepsIt pins an engine keeping the id it minted on its first start.
func TestGetTenantIDMintsOnceAndKeepsIt(t *testing.T) {
	t.Parallel()

	env := &Environment{StateDir: filepath.Join(t.TempDir(), "state")}

	first, err := env.GetTenantID()
	require.NoError(t, err)
	_, err = uuid.Parse(first)
	require.NoError(t, err)

	again, err := env.GetTenantID()
	require.NoError(t, err)
	assert.Equal(t, first, again)

	saved, err := os.ReadFile(filepath.Join(env.StateDir, tenantFile))
	require.NoError(t, err)
	assert.Equal(t, first+"\n", string(saved))

	entries, err := os.ReadDir(env.StateDir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "the temp file the id was written through is gone")
}

// TestGetTenantIDRefusesAnIDThatIsNotAUUID covers a hand-edited file.
func TestGetTenantIDRefusesAnIDThatIsNotAUUID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, tenantFile), []byte("acme\n"), 0o600))

	_, err := (&Environment{StateDir: dir}).GetTenantID()

	require.Error(t, err)
	assert.Contains(t, err.Error(), tenantFile)
}
