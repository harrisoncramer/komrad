package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// client is the git these tests drive.
var client = NewClient()

func write(t *testing.T, root, path, content string) {
	t.Helper()

	full := filepath.Join(root, filepath.FromSlash(path))
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
}

func run(t *testing.T, root string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

// repo builds a repository with one commit behind it, so a snapshot has somewhere to measure from.
func repo(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	run(t, root, "init", "--initial-branch=main")
	run(t, root, "config", "user.email", "test@example.com")
	run(t, root, "config", "user.name", "test")

	write(t, root, "kept.go", "package main\n")
	write(t, root, "proto/gen/api.pb.go", "package gen\n")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-m", "init")

	return root
}
