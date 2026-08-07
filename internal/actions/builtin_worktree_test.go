package actions

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuiltinWorktreeSetup exercises the shipped _worktree_setup action for real, the way
// a workflow's setup action actually runs: installed to disk, found by name, and run
// through the same Runner every other action uses.
func TestBuiltinWorktreeSetup(t *testing.T) {
	repo := initRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".env"), []byte("SECRET=1\n"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(repo, "node_modules"), 0o755))

	worktreesDir := t.TempDir()

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      "agent-a",
		repo:         repo,
		worktreesDir: worktreesDir,
		copyPaths:    []string{".env"},
		symlinkPaths: []string{"node_modules"},
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	tree := strings.TrimSpace(string(result.Stdout))
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

// TestBuiltinWorktreeSetupIsIdempotentOnResume covers the one thing a setup action has to
// get right: rerunning it against a worktree that is still there must not fail or make a
// second one, and must print the same path both times.
func TestBuiltinWorktreeSetupIsIdempotentOnResume(t *testing.T) {
	repo := initRepo(t)
	worktreesDir := t.TempDir()
	env := worktreeEnv{agentID: "agent-b", repo: repo, worktreesDir: worktreesDir}

	first, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", first.Stderr)

	second, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", second.Stderr)

	assert.Equal(t, first.Stdout, second.Stdout, "the same agent gets the same worktree path both times")
	assert.Contains(t, string(second.Stderr), "already exists", "a resume says it found the worktree rather than trying to make it again")

	tree := strings.TrimSpace(string(first.Stdout))
	listed, err := exec.Command("git", "-C", repo, "worktree", "list").Output()
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(listed), tree), "only one worktree exists after the resume")
}

// TestBuiltinWorktreeSetupOnResumeStillWorksAgainstTheRepo covers the environment a resume
// actually hands the setup action: the agent's cwd is the worktree by then, since setup
// itself replaced it at creation. The repo has to come from the workflow instead, or a
// symlink path resolves to the same file on both sides and is relinked to point at itself.
func TestBuiltinWorktreeSetupOnResumeStillWorksAgainstTheRepo(t *testing.T) {
	repo := initRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".env"), []byte("SECRET=1\n"), 0o644))

	env := worktreeEnv{
		agentID:      "agent-resume-link",
		repo:         repo,
		agentCwd:     repo,
		worktreesDir: t.TempDir(),
		symlinkPaths: []string{".env"},
	}

	first, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", first.Stderr)
	tree := strings.TrimSpace(string(first.Stdout))

	env.agentCwd = tree
	second, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", second.Stderr)

	link, err := os.Readlink(filepath.Join(tree, ".env"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(repo, ".env"), link, "the symlink still points into the repo rather than at itself")

	content, err := os.ReadFile(filepath.Join(tree, ".env"))
	require.NoError(t, err)
	assert.Equal(t, "SECRET=1\n", string(content))
}

// TestBuiltinWorktreeSetupOnResumeLeavesAppliedPathsAlone covers the copy and symlink lists
// being applied only to a worktree this run created. Copying a directory over one that is
// already there nests it a level deeper every time, and an agent's own edits to a copied
// path are its work rather than something a resume gets to overwrite.
func TestBuiltinWorktreeSetupOnResumeLeavesAppliedPathsAlone(t *testing.T) {
	repo := initRepo(t)
	require.NoError(t, os.Mkdir(filepath.Join(repo, "node_modules"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "node_modules", "mod.txt"), []byte("dep\n"), 0o644))

	env := worktreeEnv{
		agentID:      "agent-resume-copy",
		repo:         repo,
		agentCwd:     repo,
		worktreesDir: t.TempDir(),
		copyPaths:    []string{"node_modules"},
	}

	first, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", first.Stderr)
	tree := strings.TrimSpace(string(first.Stdout))

	edited := filepath.Join(tree, "node_modules", "mod.txt")
	require.NoError(t, os.WriteFile(edited, []byte("patched\n"), 0o644))

	env.agentCwd = tree
	second, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", second.Stderr)

	assert.NoDirExists(t, filepath.Join(tree, "node_modules", "node_modules"), "a resume does not copy the directory inside itself")

	content, err := os.ReadFile(edited)
	require.NoError(t, err)
	assert.Equal(t, "patched\n", string(content), "a resume does not overwrite what the agent changed")
}

// TestBuiltinWorktreeSetupFallsBackToTheRepoOwnBranchNotAHardcodedMain covers a repo with
// no origin and a default branch that is not main: a blank base has nothing to read
// origin/HEAD from, so it must fall back to whatever branch the repo is actually on rather
// than a literal "main" that would not exist there.
func TestBuiltinWorktreeSetupFallsBackToTheRepoOwnBranchNotAHardcodedMain(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--quiet", "-b", "trunk")
	runGit(t, dir, "config", "user.email", "test@weave.test")
	runGit(t, dir, "config", "user.name", "weave test")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644))
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "--quiet", "-m", "initial")

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      "agent-trunk",
		repo:         dir,
		worktreesDir: t.TempDir(),
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)
	assertIsDir(t, strings.TrimSpace(string(result.Stdout)))
}

// TestBuiltinWorktreeSetupResumesAfterTheDirectoryIsGoneButTheBranchIsNot covers cleanup
// having removed the worktree directory but left the branch behind, or anything else
// deleting just the directory: setup has to be able to rebuild the worktree rather than
// failing with "branch already exists".
func TestBuiltinWorktreeSetupResumesAfterTheDirectoryIsGoneButTheBranchIsNot(t *testing.T) {
	repo := initRepo(t)
	worktreesDir := t.TempDir()
	env := worktreeEnv{agentID: "agent-g", repo: repo, agentCwd: repo, worktreesDir: worktreesDir}

	first, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", first.Stderr)
	tree := strings.TrimSpace(string(first.Stdout))

	runGit(t, repo, "worktree", "remove", "--force", tree)
	env.agentCwd = tree

	branches, err := exec.Command("git", "-C", repo, "branch", "--list", "agent/agent-g").Output()
	require.NoError(t, err)
	require.NotEmpty(t, strings.TrimSpace(string(branches)), "the branch is still there after only the directory was removed")

	second, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", second.Stderr)
	assertIsDir(t, strings.TrimSpace(string(second.Stdout)))
}

// TestBuiltinWorktreeSetupCopiesADirectory covers a copy path naming a directory rather
// than a file: cp without -r would fail and silently drop the rest of the list.
func TestBuiltinWorktreeSetupCopiesADirectory(t *testing.T) {
	repo := initRepo(t)
	require.NoError(t, os.Mkdir(filepath.Join(repo, "assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "assets", "logo.png"), []byte("fake-png"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "after.env"), []byte("AFTER=1\n"), 0o644))

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      "agent-h",
		repo:         repo,
		worktreesDir: t.TempDir(),
		copyPaths:    []string{"assets", "after.env"},
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	tree := strings.TrimSpace(string(result.Stdout))
	copied, err := os.ReadFile(filepath.Join(tree, "assets", "logo.png"))
	require.NoError(t, err)
	assert.Equal(t, "fake-png", string(copied))

	after, err := os.ReadFile(filepath.Join(tree, "after.env"))
	require.NoError(t, err, "a directory copy must not stop the entries listed after it")
	assert.Equal(t, "AFTER=1\n", string(after))
}

// TestBuiltinWorktreeCleanupRefusesAPlainCheckout covers WEAVE_AGENT_CWD pointing at a
// checkout that is not a linked worktree at all, such as the repo weave itself lives in:
// cleanup must not run worktree remove or branch -D against it.
func TestBuiltinWorktreeCleanupRefusesAPlainCheckout(t *testing.T) {
	repo := initRepo(t)
	runGit(t, repo, "branch", "agent/agent-i")

	result, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{agentID: "agent-i", agentCwd: repo})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	assertIsDir(t, repo)
	branches, err := exec.Command("git", "-C", repo, "branch", "--list", "agent/agent-i").Output()
	require.NoError(t, err)
	assert.NotEmpty(t, strings.TrimSpace(string(branches)), "the branch survives since this was never a linked worktree")
}

// TestBuiltinWorktreeSetupCopyMissingSourceIsNonFatal covers a copy or symlink entry
// naming a file the repo does not have: setup warns on stderr and keeps going rather than
// aborting, since the worktree it already made is worth more than one missing file.
func TestBuiltinWorktreeSetupCopyMissingSourceIsNonFatal(t *testing.T) {
	repo := initRepo(t)

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      "agent-c",
		repo:         repo,
		worktreesDir: t.TempDir(),
		copyPaths:    []string{"missing.env"},
		symlinkPaths: []string{"missing-cache"},
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	assertIsDir(t, strings.TrimSpace(string(result.Stdout)))
	assert.Contains(t, string(result.Stderr), "missing.env")
	assert.Contains(t, string(result.Stderr), "missing-cache")
}

// TestBuiltinWorktreeSetupBaseBranch covers where the new worktree's branch point comes
// from: an explicit base wins, and a blank one falls back to the repo's default rather
// than erroring, so a repo with no configured base still gets a usable worktree.
func TestBuiltinWorktreeSetupBaseBranch(t *testing.T) {
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
			result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
				agentID:      "agent-d",
				repo:         initRepo(t),
				worktreesDir: t.TempDir(),
				baseBranch:   tt.baseBranch,
			})
			require.NoError(t, err, "stderr: %s", result.Stderr)
			assertIsDir(t, strings.TrimSpace(string(result.Stdout)))
		})
	}
}

// TestBuiltinWorktreeSetupFetchesFromOrigin covers the repo that has a remote: setup has to
// fetch the base branch and build the worktree off origin's copy of it rather than
// whatever the local repo happens to already have.
func TestBuiltinWorktreeSetupFetchesFromOrigin(t *testing.T) {
	upstream := initBareRepo(t)
	repo := cloneRepo(t, upstream)

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      "agent-e",
		repo:         repo,
		worktreesDir: t.TempDir(),
		baseBranch:   "main",
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)
	assert.Contains(t, string(result.Stderr), "fetching main from origin")
	assertIsDir(t, strings.TrimSpace(string(result.Stdout)))
}

// TestBuiltinWorktreeCleanup covers the ordinary case: the worktree and its branch both
// exist, and cleanup removes both.
func TestBuiltinWorktreeCleanup(t *testing.T) {
	repo := initRepo(t)
	setup, err := runBuiltin(t, "_worktree_setup", worktreeEnv{agentID: "agent-f", repo: repo, worktreesDir: t.TempDir()})
	require.NoError(t, err, "stderr: %s", setup.Stderr)
	tree := strings.TrimSpace(string(setup.Stdout))

	cleanup, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{agentID: "agent-f", agentCwd: tree})
	require.NoError(t, err, "stderr: %s", cleanup.Stderr)

	_, statErr := os.Stat(tree)
	assert.True(t, os.IsNotExist(statErr), "the worktree directory is gone")

	branches, err := exec.Command("git", "-C", repo, "branch", "--list", "agent/agent-f").Output()
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(string(branches)), "the branch is gone")
}

// TestBuiltinWorktreeCleanupIsBestEffortWhenAlreadyGone covers what docs/actions.md requires
// of every cleanup action: a workflow's cleanup must not block a delete whatever state its
// target is in, so running cleanup against a directory that was never a worktree, or was
// already removed, must still exit zero.
func TestBuiltinWorktreeCleanupIsBestEffortWhenAlreadyGone(t *testing.T) {
	result, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{agentID: "agent-never-existed", agentCwd: t.TempDir()})
	require.NoError(t, err, "stderr: %s", result.Stderr)
}

// worktreeEnv is the slice of the action environment the two worktree builtins read, per
// the contract docs/actions.md documents.
type worktreeEnv struct {
	agentID      string
	agentCwd     string
	repo         string
	worktreesDir string
	baseBranch   string
	copyPaths    []string
	symlinkPaths []string
}

func (e worktreeEnv) toEnv() []string {
	return []string{
		"WEAVE_AGENT_ID=" + e.agentID,
		"WEAVE_AGENT_CWD=" + e.agentCwd,
		"WEAVE_WORKTREE_REPO=" + e.repo,
		"WEAVE_WORKTREES_DIR=" + e.worktreesDir,
		"WEAVE_WORKTREE_BASE_BRANCH=" + e.baseBranch,
		"WEAVE_WORKTREE_COPY_PATHS=" + strings.Join(e.copyPaths, "\n"),
		"WEAVE_WORKTREE_SYMLINK_PATHS=" + strings.Join(e.symlinkPaths, "\n"),
	}
}

// runBuiltin installs this build's builtins to a temp directory, finds name among them,
// and runs it to completion the way weave's own action runner does.
func runBuiltin(t *testing.T, name string, env worktreeEnv) (Result, error) {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, InstallBuiltins(dir))

	action, err := Discover(dir)
	require.NoError(t, err)

	var found *Action
	for i, a := range action {
		if a.Name == name {
			found = &action[i]
		}
	}
	require.NotNil(t, found, "%s is not a builtin action", name)

	return NewRunner().RunAndWait(context.Background(), RunParams{
		Action: *found,
		Dir:    found.GetDir(),
		Env:    env.toEnv(),
	})
}

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

func initBareRepo(t *testing.T) string {
	t.Helper()

	seed := initRepo(t)
	bare := t.TempDir()
	runGit(t, seed, "clone", "--quiet", "--bare", seed, bare)

	return bare
}

func cloneRepo(t *testing.T, upstream string) string {
	t.Helper()

	dir := t.TempDir()
	runGitClone(t, "--quiet", upstream, dir)

	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %v: %s", args, output)
}

func runGitClone(t *testing.T, args ...string) {
	t.Helper()

	cmd := exec.Command("git", append([]string{"clone"}, args...)...)
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git clone %v: %s", args, output)
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
