package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"weave/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeDoctorCookbook writes a cookbook.toml holding manifest under dir/name, for the
// Cookbooks check tests below.
func writeDoctorCookbook(t *testing.T, dir, name, manifest string) {
	t.Helper()

	cookbookDir := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(cookbookDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cookbookDir, "cookbook.toml"), []byte(manifest), 0o644))
}

// TestCookbooksCheck covers the same problems weave cookbook install itself refuses on,
// caught here before anyone runs install at all.
func TestCookbooksCheck(t *testing.T) {
	t.Run("a manifest that will not parse", func(t *testing.T) {
		dir := t.TempDir()
		writeDoctorCookbook(t, dir, "broken", "not valid toml [[[\n")
		cfg := config.Defaults()
		cfg.Cookbooks.Dir = dir

		result := Cookbooks().Run(t.Context(), configEnv(cfg, nil, true))

		assert.Equal(t, StatusFail, result.Status)
		assert.Equal(t, "cookbook_invalid", CodeOf(result.Err))
		assert.Contains(t, result.Err.Error(), "broken")
	})

	t.Run("setup_action naming no sibling file", func(t *testing.T) {
		dir := t.TempDir()
		writeDoctorCookbook(t, dir, "broken-setup", "[workflow]\nsetup_action = \"nope\"\n")
		cfg := config.Defaults()
		cfg.Cookbooks.Dir = dir

		result := Cookbooks().Run(t.Context(), configEnv(cfg, nil, true))

		assert.Equal(t, StatusFail, result.Status)
		assert.Contains(t, result.Err.Error(), "broken-setup")
		assert.Contains(t, result.Err.Error(), "nope")
	})

	t.Run("no cookbooks directory at all", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.Cookbooks.Dir = filepath.Join(t.TempDir(), "does-not-exist")

		result := Cookbooks().Run(t.Context(), configEnv(cfg, nil, true))

		assert.Equal(t, StatusOK, result.Status)
	})

	t.Run("a cookbook that install would accept", func(t *testing.T) {
		dir := t.TempDir()
		writeDoctorCookbook(t, dir, "worktree-agent", "description = \"fine\"\n")
		cfg := config.Defaults()
		cfg.Cookbooks.Dir = dir

		result := Cookbooks().Run(t.Context(), configEnv(cfg, nil, true))

		assert.Equal(t, StatusOK, result.Status)
	})
}
