package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// configTOMLPath is the checked-in file, relative to this package.
const configTOMLPath = "../config.toml"

func TestConfigTOMLIsUpToDate(t *testing.T) {
	rendered, err := RenderConfigTOML(".")
	require.NoError(t, err)

	committed, err := os.ReadFile(configTOMLPath)
	require.NoError(t, err)

	assert.Equal(t, string(rendered), string(committed),
		"config.toml is stale; run 'mise run gen:config' and commit the result")
}

func TestRenderedConfigLoadsAsTheDefaults(t *testing.T) {
	rendered, err := RenderConfigTOML(".")
	require.NoError(t, err)

	// Round-tripping the generated file through Load is what proves the renderer
	// emits keys the decoder actually recognises. Comparing bytes against the
	// committed file only proves the two agree with each other.
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, rendered, 0o644))

	cfg, err := Load([]string{path})
	require.NoError(t, err)
	assert.Empty(t, cfg.Harness.Claude.Flags)
	assert.Equal(t, DefaultHarness, cfg.DefaultHarness)
	assert.Equal(t, DefaultKeybindings(), cfg.Keybindings)
}
