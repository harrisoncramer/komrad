//go:build integration

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestIntegrationPackerValidateWithoutABuild covers the check CI runs, where nothing has cross
// compiled the Linux binary the image is built around.
func TestIntegrationPackerValidateWithoutABuild(t *testing.T) {
	_, err := exec.LookPath("packer")
	require.NoError(t, err, "packer is a tool this repository pins")

	root := repoRoot(t)
	work := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(work, "mise", "tasks"), 0o755))
	copyTree(t, filepath.Join(root, "packer"), filepath.Join(work, "packer"))
	copyTree(t, filepath.Join(root, "mise", "tasks", "packer"), filepath.Join(work, "mise", "tasks", "packer"))

	require.NoDirExists(t, filepath.Join(work, "dist"))

	cmd := exec.CommandContext(t.Context(), "bash", filepath.Join(work, "mise", "tasks", "packer", "validate"))
	cmd.Env = append(os.Environ(), "MISE_CONFIG_ROOT="+work)

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "validate failed with an empty dist:\n%s", out)
	require.Contains(t, string(out), "The configuration is valid.")
	require.NoFileExists(t, filepath.Join(work, "dist", "komrad-linux-arm64"))
}

func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "the test's own path is known")

	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()

	out, err := exec.CommandContext(t.Context(), "cp", "-R", from, to).CombinedOutput()
	require.NoError(t, err, "%s", out)
}
