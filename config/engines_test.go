package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnginePathRefusesAPath covers a name that would reach outside the engines directory, since
// an engine is chosen by name from a hotkey rather than typed as a path.
func TestEnginePathRefusesAPath(t *testing.T) {
	for _, name := range []string{"../secrets", "a/b", ".", "..", "  "} {
		_, err := EnginePath(name)
		require.Error(t, err, name)
	}
}

// TestEngineNamesListsWhatIsThere covers the listing an unknown name is answered with, which is
// the only way to find out what this machine can be pointed at.
func TestEngineNamesListsWhatIsThere(t *testing.T) {
	dir := t.TempDir()
	for _, file := range []string{"remote.toml", "staging.toml", "notes.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(""), 0o600))
	}
	require.NoError(t, os.Mkdir(filepath.Join(dir, "nested.toml"), 0o755))

	assert.Equal(t, []string{"remote", "staging"}, engineNames(dir),
		"only the toml files, without their extension, and not a directory named like one")
	assert.Contains(t, knownEngines(dir), "remote, staging")
	assert.Contains(t, knownEngines(t.TempDir()), "no engines yet")
}
