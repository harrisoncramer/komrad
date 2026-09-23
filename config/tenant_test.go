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
	t.Setenv(tenantVariable, "")
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

// TestGetTenantIDReadsTheVariableWithoutSaving pins WEAVE_TENANT_ID winning and leaving the state directory alone.
func TestGetTenantIDReadsTheVariableWithoutSaving(t *testing.T) {
	t.Setenv(tenantVariable, "01990000-0000-7000-8000-000000000002")
	env := &Environment{StateDir: filepath.Join(t.TempDir(), "state")}

	got, err := env.GetTenantID()
	require.NoError(t, err)
	assert.Equal(t, "01990000-0000-7000-8000-000000000002", got)
	assert.NoDirExists(t, env.StateDir)
}

// TestGetTenantIDRefusesAnIDThatIsNotAUUID covers a hand-edited file and a mistyped variable.
func TestGetTenantIDRefusesAnIDThatIsNotAUUID(t *testing.T) {
	t.Run("the saved file", func(t *testing.T) {
		t.Setenv(tenantVariable, "")
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, tenantFile), []byte("acme\n"), 0o600))

		_, err := (&Environment{StateDir: dir}).GetTenantID()

		require.Error(t, err)
		assert.Contains(t, err.Error(), tenantFile)
	})

	t.Run("the variable", func(t *testing.T) {
		t.Setenv(tenantVariable, "acme")

		_, err := (&Environment{StateDir: t.TempDir()}).GetTenantID()

		require.Error(t, err)
		assert.Contains(t, err.Error(), tenantVariable)
	})
}
