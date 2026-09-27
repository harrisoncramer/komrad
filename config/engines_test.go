package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnginePathRefusesAPath covers a name that would reach outside the engines directory.
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
	for _, file := range []string{"remote.toml", "staging.toml", "notes.md", "local.toml"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(""), 0o600))
	}
	require.NoError(t, os.Mkdir(filepath.Join(dir, "nested.toml"), 0o755))

	assert.Equal(t, []string{"remote", "staging"}, engineNames(dir),
		"only the toml files, without their extension, and neither a directory named like one nor the local name")
	assert.Contains(t, knownEngines(dir), "remote, staging")
	assert.Contains(t, knownEngines(t.TempDir()), "no engines yet")
}

// TestEngineLaidOverAnAbsentDefault covers naming an engine on a machine that never wrote a config
// file.
func TestEngineLaidOverAnAbsentDefault(t *testing.T) {
	clearConnectionEnvironment(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	engines := filepath.Join(home, ".config", "komrad", "engines")
	require.NoError(t, os.MkdirAll(engines, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(engines, "remote.toml"),
		[]byte("[connection]\naddr = \"127.0.0.1:8799\"\n"), 0o600))

	layers, err := ResolveLayers(Stack{Engine: "remote"})
	require.NoError(t, err)

	require.Len(t, layers, 2)
	assert.Equal(t, filepath.Join(home, ".config", "komrad", "config.toml"), layers[0].Path)
	assert.False(t, layers[0].Named, "the default path is no more required than it was without an engine")
	assert.Equal(t, filepath.Join(engines, "remote.toml"), layers[1].Path)
	assert.True(t, layers[1].Named, "an engine asked for by name has to be there")

	cfg, err := LoadStack(Stack{Engine: "remote"})
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1:8799", cfg.Connection.Addr)
}

// TestEngineOverANamedFile covers an engine laid over a config named by hand, where both files
// were asked for, both have to be there, and the engine is the one that wins.
func TestEngineOverANamedFile(t *testing.T) {
	clearConnectionEnvironment(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	engines := filepath.Join(home, ".config", "komrad", "engines")
	require.NoError(t, os.MkdirAll(engines, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(engines, "remote.toml"),
		[]byte("[connection]\naddr = \"127.0.0.1:8799\"\n"), 0o600))

	named := filepath.Join(home, "named.toml")
	require.NoError(t, os.WriteFile(named,
		[]byte("[connection]\naddr = \"127.0.0.1:9111\"\ntmux_socket = \"named\"\n"), 0o600))

	layers, err := ResolveLayers(Stack{Paths: []string{named}, Engine: "remote"})
	require.NoError(t, err)

	require.Len(t, layers, 2)
	assert.Equal(t, named, layers[0].Path)
	assert.True(t, layers[0].Named)
	assert.True(t, layers[1].Named)

	cfg, err := LoadStack(Stack{Paths: []string{named}, Engine: "remote"})
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1:8799", cfg.Connection.Addr, "the engine is read after the config it is laid over")
	assert.Equal(t, "named", cfg.Connection.TmuxSocket, "what the engine leaves alone still comes from underneath it")

	_, err = ResolveLayers(Stack{Engine: "missing"})
	require.Error(t, err, "an engine that is not there is named in the error rather than ignored")

	_, err = ResolveLayers(Stack{Engine: "   "})
	require.Error(t, err, "a name made of spaces is a name, not a request for the local engine")
}

// TestEngineNamesReadsTheEnginesDir covers the listing the picker is built from.
func TestEngineNamesReadsTheEnginesDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(enginesDirVariable, "")

	names, err := EngineNames()
	require.NoError(t, err)
	assert.Empty(t, names, "a machine with no engines dir knows no engines")

	engines := filepath.Join(home, ".config", "komrad", "engines")
	require.NoError(t, os.MkdirAll(engines, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(engines, "remote.toml"), []byte(""), 0o600))

	names, err = EngineNames()
	require.NoError(t, err)
	assert.Equal(t, []string{"remote"}, names)

	t.Setenv(enginesDirVariable, t.TempDir())
	names, err = EngineNames()
	require.NoError(t, err)
	assert.Empty(t, names, "a directory named in the environment stands in for the user's")
}
