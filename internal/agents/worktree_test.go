package agents

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRenderWorktreeSetupAndRun exercises the generated setup script for real: against a
// throwaway git repo, with no remote, checking the worktree it makes, the branch it is on,
// and that the copy and symlink entries land where they should.
func TestRenderWorktreeSetupAndRun(t *testing.T) {
	repo := initRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".env"), []byte("SECRET=1\n"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(repo, "node_modules"), 0o755))

	worktreesDir := t.TempDir()

	script := renderWorktreeSetupScript(worktreeScriptParams{
		Repo:         repo,
		WorktreesDir: worktreesDir,
		CopyPaths:    []string{".env"},
		SymlinkPaths: []string{"node_modules"},
	})

	stdout, stderr, err := runScript(t, script, scriptEnv{agentID: "agent-a", trigger: "setup"})
	require.NoError(t, err, "stderr: %s", stderr)

	tree := strings.TrimSpace(stdout)
	assert.Equal(t, filepath.Join(worktreesDir, "agent-a"), tree)
	assertIsDir(t, tree)
	assertBranch(t, tree, "agent/agent-a")

	envContent, err := os.ReadFile(filepath.Join(tree, ".env"))
	require.NoError(t, err)
	assert.Equal(t, "SECRET=1\n", string(envContent))

	link, err := os.Readlink(filepath.Join(tree, "node_modules"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(repo, "node_modules"), link)
}

// TestRenderWorktreeSetupIsIdempotentOnResume covers the one thing docs/actions.md asks a
// setup action to get right: rerunning it with WEAVE_TRIGGER=resume against a worktree that
// is still there must not fail or create a second one, and must still print the same path.
func TestRenderWorktreeSetupIsIdempotentOnResume(t *testing.T) {
	repo := initRepo(t)
	worktreesDir := t.TempDir()
	script := renderWorktreeSetupScript(worktreeScriptParams{Repo: repo, WorktreesDir: worktreesDir})

	firstOut, firstErr, err := runScript(t, script, scriptEnv{agentID: "agent-b", trigger: "setup"})
	require.NoError(t, err, "stderr: %s", firstErr)

	secondOut, secondErr, err := runScript(t, script, scriptEnv{agentID: "agent-b", trigger: "resume"})
	require.NoError(t, err, "stderr: %s", secondErr)

	assert.Equal(t, firstOut, secondOut, "the same agent gets the same worktree path both times")
	assert.Contains(t, string(secondErr), "already exists", "a resume says it found the worktree rather than trying to make it again")

	tree := strings.TrimSpace(firstOut)
	listed, err := exec.Command("git", "-C", repo, "worktree", "list").Output()
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(listed), tree), "only one worktree exists after the resume")
}

// TestRenderWorktreeSetupCopyMissingSourceIsNonFatal covers a copy or symlink entry naming a
// file the repo does not have: setup has to warn on stderr and keep going rather than abort,
// since the worktree it already made is worth more than one missing file.
func TestRenderWorktreeSetupCopyMissingSourceIsNonFatal(t *testing.T) {
	repo := initRepo(t)
	worktreesDir := t.TempDir()

	script := renderWorktreeSetupScript(worktreeScriptParams{
		Repo:         repo,
		WorktreesDir: worktreesDir,
		CopyPaths:    []string{"missing.env"},
		SymlinkPaths: []string{"missing-cache"},
	})

	stdout, stderr, err := runScript(t, script, scriptEnv{agentID: "agent-c", trigger: "setup"})
	require.NoError(t, err, "stderr: %s", stderr)

	assertIsDir(t, strings.TrimSpace(stdout))
	assert.Contains(t, string(stderr), "missing.env")
	assert.Contains(t, string(stderr), "missing-cache")
}

// TestRenderWorktreeSetupBaseBranch covers where the new worktree's branch point comes
// from: an explicit base wins, and a blank one falls back to the repo's default rather than
// erroring, so a repo with no configured base still gets a usable worktree.
func TestRenderWorktreeSetupBaseBranch(t *testing.T) {
	tests := []struct {
		name       string
		baseBranch string
	}{
		{
			name: "a blank base falls back to the repo's default branch",
		},
		{
			name:       "an explicit base is used as the branch point",
			baseBranch: "main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := initRepo(t)
			worktreesDir := t.TempDir()

			script := renderWorktreeSetupScript(worktreeScriptParams{
				Repo:         repo,
				WorktreesDir: worktreesDir,
				BaseBranch:   tt.baseBranch,
			})

			stdout, stderr, err := runScript(t, script, scriptEnv{agentID: "agent-d", trigger: "setup"})
			require.NoError(t, err, "stderr: %s", stderr)
			assertIsDir(t, strings.TrimSpace(stdout))
		})
	}
}

// TestRenderWorktreeSetupFetchesFromOrigin covers the repo that does have a remote: setup
// has to fetch the base branch and build the worktree off origin's copy of it rather than
// whatever the local repo happens to already have.
func TestRenderWorktreeSetupFetchesFromOrigin(t *testing.T) {
	upstream := initBareRepo(t)
	repo := cloneRepo(t, upstream)
	worktreesDir := t.TempDir()

	script := renderWorktreeSetupScript(worktreeScriptParams{Repo: repo, WorktreesDir: worktreesDir, BaseBranch: "main"})

	stdout, stderr, err := runScript(t, script, scriptEnv{agentID: "agent-e", trigger: "setup"})
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, string(stderr), "fetching main from origin")
	assertIsDir(t, strings.TrimSpace(stdout))
}

// TestRenderWorktreeCleanupRun covers the ordinary case: the worktree and its branch both
// exist, and cleanup removes both.
func TestRenderWorktreeCleanupRun(t *testing.T) {
	repo := initRepo(t)
	worktreesDir := t.TempDir()

	setup := renderWorktreeSetupScript(worktreeScriptParams{Repo: repo, WorktreesDir: worktreesDir})
	stdout, stderr, err := runScript(t, setup, scriptEnv{agentID: "agent-f", trigger: "setup"})
	require.NoError(t, err, "stderr: %s", stderr)
	tree := strings.TrimSpace(stdout)

	cleanup := renderWorktreeCleanupScript(worktreeScriptParams{Repo: repo, WorktreesDir: worktreesDir})
	_, cleanupErr, err := runScript(t, cleanup, scriptEnv{agentID: "agent-f", trigger: "cleanup"})
	require.NoError(t, err, "stderr: %s", cleanupErr)

	_, statErr := os.Stat(tree)
	assert.True(t, os.IsNotExist(statErr), "the worktree directory is gone")

	branches, err := exec.Command("git", "-C", repo, "branch", "--list", "agent/agent-f").Output()
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(string(branches)), "the branch is gone")
}

// TestRenderWorktreeCleanupIsBestEffortWhenAlreadyGone covers the case docs/actions.md
// requires of every cleanup action: a workflow's cleanup must not block a delete whatever
// state its target is in, so running cleanup against a worktree and branch that were never
// made, or were already removed, must still exit zero.
func TestRenderWorktreeCleanupIsBestEffortWhenAlreadyGone(t *testing.T) {
	repo := initRepo(t)
	worktreesDir := t.TempDir()

	cleanup := renderWorktreeCleanupScript(worktreeScriptParams{Repo: repo, WorktreesDir: worktreesDir})

	_, stderr, err := runScript(t, cleanup, scriptEnv{agentID: "agent-never-existed", trigger: "cleanup"})
	require.NoError(t, err, "stderr: %s", stderr)
}

// TestShellQuote checks the one property the generated scripts depend on: whatever comes
// back can be spliced into shell source and read back as exactly the original string, single
// quotes, spaces, and dollar signs included.
func TestShellQuote(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "a plain path",
			input: "/repo/dir",
		},
		{
			name:  "a path with a space",
			input: "/repo/my dir",
		},
		{
			name:  "an embedded single quote",
			input: "it's/here",
		},
		{
			name:  "shell metacharacters that must not be evaluated",
			input: "$(rm -rf /); echo hi `whoami`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script := "#!/bin/sh\nprintf '%s' " + shellQuote(tt.input) + "\n"
			stdout, stderr, err := runScript(t, script, scriptEnv{})
			require.NoError(t, err, "stderr: %s", stderr)
			assert.Equal(t, tt.input, stdout)
		})
	}
}

// scriptEnv is the slice of the action environment a generated worktree script actually
// reads, per the contract in docs/actions.md.
type scriptEnv struct {
	agentID string
	trigger string
}

// runScript writes script to a temp executable file and runs it, returning stdout and
// stderr separately the way weave's own action runner does, since a setup action's stdout
// is meaningful and its stderr is not.
func runScript(t *testing.T, script string, env scriptEnv) (string, string, error) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "script.sh")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))

	cmd := exec.Command(path)
	cmd.Env = append(os.Environ(),
		"WEAVE_AGENT_ID="+env.agentID,
		"WEAVE_TRIGGER="+env.trigger,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	return stdout.String(), stderr.String(), err
}

// initRepo makes a throwaway git repo on a branch called main with one commit, since a
// worktree cannot be added off a branch with no history.
func initRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	runGit(t, dir, "init", "--quiet", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@weave.test")
	runGit(t, dir, "config", "user.name", "weave test")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644))
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "--quiet", "-m", "initial")

	return dir
}

// initBareRepo makes a bare repo to stand in as a remote, seeded with one commit on main so
// a clone of it has a real origin/main to fetch.
func initBareRepo(t *testing.T) string {
	t.Helper()

	seed := initRepo(t)
	bare := t.TempDir()
	runGit(t, seed, "clone", "--quiet", "--bare", seed, bare)

	return bare
}

// cloneRepo clones upstream into a fresh directory, standing in for the checkout a
// worktree-enabled workflow's cwd would name.
func cloneRepo(t *testing.T, upstream string) string {
	t.Helper()

	dir := t.TempDir()
	runGit(t, dir, "clone", "--quiet", upstream, dir)

	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	if args[0] != "clone" {
		cmd = exec.Command("git", append([]string{"-C", dir}, args...)...)
	}

	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %v: %s", args, output)
}

func assertIsDir(t *testing.T, path string) {
	t.Helper()

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func assertBranch(t *testing.T, tree, want string) {
	t.Helper()

	out, err := exec.Command("git", "-C", tree, "rev-parse", "--abbrev-ref", "HEAD").Output()
	require.NoError(t, err)
	assert.Equal(t, want, strings.TrimSpace(string(out)))
}
