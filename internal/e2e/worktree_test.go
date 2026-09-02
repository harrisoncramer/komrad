package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"weave/internal/weavetest"
	workflowv1 "weave/proto/gen/workflow/v1"

	"github.com/stretchr/testify/require"
)

// worktreeRepo is a primary checkout, which is what a workflow that provisions worktrees needs
// as its cwd.
func worktreeRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "--quiet", "-b", "main"},
		{"config", "user.email", "test@weave.test"},
		{"config", "user.name", "weave test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		require.NoError(t, cmd.Run(), "git %v", args)
	}

	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644))
	for _, args := range [][]string{{"add", "README.md"}, {"commit", "--quiet", "-m", "initial"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		require.NoError(t, cmd.Run(), "git %v", args)
	}

	return dir
}

// replaceWorktreeTeardown writes over the built-in that gives an agent's worktree back, which
// is the only deprovision there is now that a workflow cannot name a script of its own.
func replaceWorktreeTeardown(t *testing.T, instance *weavetest.Instance, script string) {
	t.Helper()

	require.NoError(t, os.WriteFile(filepath.Join(instance.GetBuiltinDir, "_worktree_cleanup"), []byte(script), 0o755))
}

// worktreeWorkflow is the request for a workflow whose agents each get a worktree of their own.
func worktreeWorkflow(t *testing.T, name string) *workflowv1.CreateWorkflowRequest {
	t.Helper()

	return &workflowv1.CreateWorkflowRequest{
		Name:     name,
		Cwd:      worktreeRepo(t),
		Worktree: &workflowv1.WorktreeConfig{Enabled: true},
	}
}
