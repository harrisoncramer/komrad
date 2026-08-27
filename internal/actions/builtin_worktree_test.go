package actions

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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
	hermeticGit(t)

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

func TestBuiltinWorktreeSetupSkipsATrackedCopyPath(t *testing.T) {
	repo := initRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "settings.json"), []byte("committed\n"), 0o644))
	commitAll(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "settings.json"), []byte("local\n"), 0o644))

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      "agent-tracked-skip",
		repo:         repo,
		worktreesDir: t.TempDir(),
		copyPaths:    []string{"settings.json"},
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	tree := strings.TrimSpace(string(result.Stdout))
	assert.Contains(t, string(result.Stderr), "settings.json is tracked, skipping copy")

	content, err := os.ReadFile(filepath.Join(tree, "settings.json"))
	require.NoError(t, err)
	assert.Equal(t, "committed\n", string(content), "a tracked path is left as git checked it out rather than copied over")
}

func TestBuiltinWorktreeSetupSkipsATrackedSymlinkPath(t *testing.T) {
	repo := initRepo(t)
	require.NoError(t, os.Mkdir(filepath.Join(repo, "vendor"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "vendor", "lib.txt"), []byte("dep\n"), 0o644))
	commitAll(t, repo)

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      "agent-tracked-link",
		repo:         repo,
		worktreesDir: t.TempDir(),
		symlinkPaths: []string{"vendor"},
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	tree := strings.TrimSpace(string(result.Stdout))
	assert.Contains(t, string(result.Stderr), "vendor is tracked, skipping symlink")

	content, err := os.ReadFile(filepath.Join(tree, "vendor", "lib.txt"))
	require.NoError(t, err)
	assert.Equal(t, "dep\n", string(content), "a tracked path is left as git checked it out rather than symlinked over")
}

// TestBuiltinWorktreeSetupRefusesAPathOutsideTheWorktree covers what clearing a destination
// costs: a path that resolves out of the worktree, or names the worktree itself, would have
// setup delete something it was never pointed at.
func TestBuiltinWorktreeSetupRefusesAPathOutsideTheWorktree(t *testing.T) {
	repo := initRepo(t)
	worktreesDir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(filepath.Dir(repo), "sibling"), 0o755))

	outside := filepath.Join(worktreesDir, "sibling")
	require.NoError(t, os.Mkdir(outside, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("keep\n"), 0o644))

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      "agent-escape",
		repo:         repo,
		worktreesDir: worktreesDir,
		copyPaths:    []string{"../sibling"},
		symlinkPaths: []string{"/"},
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	assert.Contains(t, string(result.Stderr), "not a path inside the worktree")
	assert.FileExists(t, filepath.Join(outside, "keep.txt"), "a path that leaves the worktree is refused rather than removed")

	info, err := os.Lstat(strings.TrimSpace(string(result.Stdout)))
	require.NoError(t, err)
	assert.True(t, info.IsDir(), "a path naming the worktree itself is refused rather than replacing it")
}

func TestBuiltinWorktreeSetupRefusesAPathUnderALinkAnEarlierEntryMade(t *testing.T) {
	repo := initRepo(t)
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "local", "bin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "local", "bin", "tool"), []byte("tool\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("local/\n"), 0o644))
	commitAll(t, repo)

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      "agent-nested-link",
		repo:         repo,
		worktreesDir: t.TempDir(),
		symlinkPaths: []string{"local", "local/bin"},
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	assert.Contains(t, string(result.Stderr), "local/bin is not a path inside the worktree")

	content, err := os.ReadFile(filepath.Join(repo, "local", "bin", "tool"))
	require.NoError(t, err, "the repo's own copy of the path survives")
	assert.Equal(t, "tool\n", string(content))

	tree := strings.TrimSpace(string(result.Stdout))
	link, err := os.Readlink(filepath.Join(tree, "local"))
	require.NoError(t, err, "the entry that was inside the worktree is still applied")
	assert.Equal(t, filepath.Join(repo, "local"), link)
}

func TestBuiltinWorktreeSetupRefusesAPathUnderATrackedSymlink(t *testing.T) {
	repo := initRepo(t)
	outside := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(outside, "cache"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "cache", "keep.txt"), []byte("keep\n"), 0o644))
	require.NoError(t, os.Symlink(outside, filepath.Join(repo, "shared")))
	commitAll(t, repo)

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      "agent-tracked-symlink",
		repo:         repo,
		worktreesDir: t.TempDir(),
		copyPaths:    []string{"shared/cache"},
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	assert.Contains(t, string(result.Stderr), "shared/cache is not a path inside the worktree")
	assert.FileExists(t, filepath.Join(outside, "cache", "keep.txt"), "the directory outside the repo survives")
}

func TestBuiltinWorktreeSetupAppliesAPathWrittenWithATrailingSlash(t *testing.T) {
	repo := initRepo(t)
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "local", "bin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "local", "bin", "tool"), []byte("tool\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("local/\n"), 0o644))
	commitAll(t, repo)

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      "agent-trailing-slash",
		repo:         repo,
		worktreesDir: t.TempDir(),
		symlinkPaths: []string{"local", "local/"},
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	content, err := os.ReadFile(filepath.Join(repo, "local", "bin", "tool"))
	require.NoError(t, err, "the repo's own copy of the path survives")
	assert.Equal(t, "tool\n", string(content))

	tree := strings.TrimSpace(string(result.Stdout))
	link, err := os.Readlink(filepath.Join(tree, "local"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(repo, "local"), link)
}

func TestBuiltinWorktreeSetupSkipsATrackedSymlinkWrittenWithATrailingSlash(t *testing.T) {
	repo := initRepo(t)
	outside := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(outside, "cache"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "cache", "keep.txt"), []byte("keep\n"), 0o644))
	require.NoError(t, os.Symlink(outside, filepath.Join(repo, "shared")))
	commitAll(t, repo)

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      "agent-tracked-symlink-slash",
		repo:         repo,
		worktreesDir: t.TempDir(),
		copyPaths:    []string{"shared/"},
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	assert.FileExists(t, filepath.Join(outside, "cache", "keep.txt"), "the directory outside the repo survives")
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

// TestBuiltinWorktreeSetupAsksOriginRatherThanReadingTheCheckedOutBranch covers the repo
// whose origin/HEAD was never written, which is every repo built with git init and a remote
// added afterwards. The branch the human left checked out is not the repo's default, so
// basing an agent on it starts every agent on top of somebody's unfinished work.
func TestBuiltinWorktreeSetupAsksOriginRatherThanReadingTheCheckedOutBranch(t *testing.T) {
	upstream := initBareRepo(t)
	repo := cloneRepo(t, upstream)
	runGit(t, repo, "remote", "set-head", "origin", "--delete")
	runGit(t, repo, "checkout", "--quiet", "-b", "harry/wip")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "wip.txt"), []byte("unfinished\n"), 0o644))
	commitAll(t, repo)

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      "agent-remote-head",
		repo:         repo,
		worktreesDir: t.TempDir(),
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	tree := strings.TrimSpace(string(result.Stdout))
	assert.Contains(t, string(result.Stderr), "fetching main from origin")
	assert.NoFileExists(t, filepath.Join(tree, "wip.txt"), "the agent is not sitting on the checked out branch's work")
	assert.Equal(t, "refs/remotes/origin/main", readSymbolicRef(t, repo, "refs/remotes/origin/HEAD"), "the answer origin gave is recorded for next time")
}

// TestBuiltinWorktreeSetupIgnoresATrackingRefTheRemoteHasMovedOn covers the default branch
// having been renamed on the forge. The remote-tracking ref origin/HEAD names survives that
// rename until somebody prunes, so it still resolves locally while naming a branch origin no
// longer has, and only origin can say so.
func TestBuiltinWorktreeSetupIgnoresATrackingRefTheRemoteHasMovedOn(t *testing.T) {
	upstream := initBareRepo(t)
	repo := cloneRepo(t, upstream)

	runGit(t, upstream, "branch", "--move", "main", "trunk")
	runGit(t, upstream, "symbolic-ref", "HEAD", "refs/heads/trunk")
	pushCommit(t, upstream, "trunk", "after-rename.txt")

	require.Equal(t, "refs/remotes/origin/main", readSymbolicRef(t, repo, "refs/remotes/origin/HEAD"), "origin/HEAD still names the branch that was renamed away")
	require.True(t, hasRef(t, repo, "refs/remotes/origin/main"), "the tracking ref for it is still there, since nothing pruned it")

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      "agent-moved-head",
		repo:         repo,
		worktreesDir: t.TempDir(),
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	tree := strings.TrimSpace(string(result.Stdout))
	assert.Contains(t, string(result.Stderr), "fetching trunk from origin")
	assert.FileExists(t, filepath.Join(tree, "after-rename.txt"), "the worktree is built from origin's branch rather than the stale tracking ref")
}

// TestBuiltinWorktreeSetupFallsBackToTheCachedHeadWhenOriginIsUnreachable covers the same
// repo offline. Asking origin is the only way to be sure, so when the answer cannot be had,
// what git cached last time it could is the best guess left and beats refusing to launch.
func TestBuiltinWorktreeSetupFallsBackToTheCachedHeadWhenOriginIsUnreachable(t *testing.T) {
	upstream := initBareRepo(t)
	repo := cloneRepo(t, upstream)
	runGit(t, repo, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      "agent-offline",
		repo:         repo,
		worktreesDir: t.TempDir(),
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	assert.Contains(t, string(result.Stderr), "fetching main from origin")
	assertIsDir(t, strings.TrimSpace(string(result.Stdout)))
}

// TestBuiltinWorktreeSetupRefusesRatherThanGuessingTheBase covers the repo that has a remote
// nobody can reach right now and no main or master to fall back on. Refusing leaves the
// agent unlaunched, which is the smaller loss: a worktree quietly based on whatever branch
// the repo was on is work built on the wrong commit and nobody notices until review.
func TestBuiltinWorktreeSetupRefusesRatherThanGuessingTheBase(t *testing.T) {
	hermeticGit(t)

	repo := t.TempDir()
	runGit(t, repo, "init", "--quiet", "-b", "trunk")
	runGit(t, repo, "config", "user.email", "test@weave.test")
	runGit(t, repo, "config", "user.name", "weave test")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello\n"), 0o644))
	commitAll(t, repo)
	runGit(t, repo, "remote", "add", "origin", filepath.Join(t.TempDir(), "gone.git"))

	worktreesDir := t.TempDir()
	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      "agent-no-base",
		repo:         repo,
		worktreesDir: worktreesDir,
	})
	require.Error(t, err, "the setup action fails rather than picking a branch of its own")

	assert.Contains(t, string(result.Stderr), "could not work out which branch")
	assert.NoDirExists(t, filepath.Join(worktreesDir, "agent-no-base"), "no worktree was made")
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

// TestBuiltinWorktreeSetupDoesNotTrackTheBase covers the upstream the new branch is given.
// Tracking the base makes git status inside the worktree report the agent as ahead of or
// behind a branch it is not on, and makes a plain git push refuse, since the upstream's name
// does not match the branch's. The branch is left with no upstream and push.autoSetupRemote
// set instead, so the agent's first push creates a remote branch of its own.
func TestBuiltinWorktreeSetupDoesNotTrackTheBase(t *testing.T) {
	upstream := initBareRepo(t)
	repo := cloneRepo(t, upstream)

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      "agent-untracked",
		repo:         repo,
		worktreesDir: t.TempDir(),
		baseBranch:   "main",
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)
	tree := strings.TrimSpace(string(result.Stdout))

	assert.Empty(t, readGitConfig(t, tree, "branch.agent/agent-untracked.remote"), "the branch has no upstream remote")
	assert.Empty(t, readGitConfig(t, tree, "branch.agent/agent-untracked.merge"), "the branch has no upstream ref")
	assert.Equal(t, "true", readGitConfig(t, tree, "push.autoSetupRemote"), "a plain push can create the remote branch")
}

// TestBuiltinWorktreeSetupRunsConcurrentlyInOneRepo covers several agents being created in
// one worktree workflow at once, which is what an orchestrating agent fanning out over the
// API does, and what two schedules matching the same minute do. Anything setup writes to the
// repo's shared config puts every concurrent run on the same lock, and git fails the add
// outright rather than waiting for it.
func TestBuiltinWorktreeSetupRunsConcurrentlyInOneRepo(t *testing.T) {
	upstream := initBareRepo(t)
	repo := cloneRepo(t, upstream)
	worktreesDir := t.TempDir()

	const concurrent = 8

	envs := make([]worktreeEnv, 0, concurrent)
	for i := range concurrent {
		envs = append(envs, worktreeEnv{
			agentID:      fmt.Sprintf("agent-parallel-%d", i),
			repo:         repo,
			worktreesDir: worktreesDir,
			baseBranch:   "main",
		})
	}

	for i, outcome := range runBuiltinConcurrently(t, "_worktree_setup", envs) {
		require.NoErrorf(t, outcome.err, "agent %d stderr: %s", i, outcome.result.Stderr)
		assertIsDir(t, strings.TrimSpace(string(outcome.result.Stdout)))
	}
}

// TestBuiltinWorktreeSetupResumesAfterTheDirectoryIsDeletedOutright covers the directory
// being removed without git being told, which is what an rm -rf does and what weave's own
// worktree_removed column reports. The registration git keeps still claims the branch, so
// rebuilding needs that stale entry pruned first.
func TestBuiltinWorktreeSetupResumesAfterTheDirectoryIsDeletedOutright(t *testing.T) {
	repo := initRepo(t)
	env := worktreeEnv{agentID: "agent-rmrf", repo: repo, worktreesDir: t.TempDir()}

	first, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", first.Stderr)
	tree := strings.TrimSpace(string(first.Stdout))

	require.NoError(t, os.RemoveAll(tree))

	second, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", second.Stderr)
	assertIsDir(t, strings.TrimSpace(string(second.Stdout)))
	assertBranch(t, tree, "agent/agent-rmrf")
}

// TestBuiltinWorktreeCleanupRefusesAWorktreeItDoesNotOwn covers WEAVE_AGENT_CWD pointing at
// a linked worktree that is not this agent's. Being a worktree is not proof of being the one
// weave made: a hand-made worktree passes the same check, and the launch path can reach
// cleanup with the agent's cwd still set to whatever the workflow named.
func TestBuiltinWorktreeCleanupRefusesAWorktreeItDoesNotOwn(t *testing.T) {
	repo := initRepo(t)
	theirs := filepath.Join(t.TempDir(), "theirs")
	runGit(t, repo, "worktree", "add", "--quiet", "-b", "someone-else", theirs)

	result, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{agentID: "agent-thief", agentCwd: theirs})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	assertIsDir(t, theirs)
	assert.NotEmpty(t, readBranchList(t, repo, "someone-else"), "someone else's branch survives")
}

// TestBuiltinWorktreeCleanupKeepsAnUnmergedBranch covers an agent that committed work nobody
// merged. The worktree goes, since a delete that leaves the directory behind leaves it
// behind for good, but the branch is the one recoverable trace of that work and force
// deleting it throws the commits away.
func TestBuiltinWorktreeCleanupKeepsAnUnmergedBranch(t *testing.T) {
	repo := initRepo(t)
	setup, err := runBuiltin(t, "_worktree_setup", worktreeEnv{agentID: "agent-unmerged", repo: repo, worktreesDir: t.TempDir()})
	require.NoError(t, err, "stderr: %s", setup.Stderr)
	tree := strings.TrimSpace(string(setup.Stdout))

	require.NoError(t, os.WriteFile(filepath.Join(tree, "work.txt"), []byte("real work\n"), 0o644))
	runGit(t, tree, "add", "work.txt")
	runGit(t, tree, "commit", "--quiet", "-m", "the agent's work")

	cleanup, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{agentID: "agent-unmerged", agentCwd: tree})
	require.NoError(t, err, "stderr: %s", cleanup.Stderr)

	assert.NoDirExists(t, tree, "the worktree still goes")
	assert.NotEmpty(t, readBranchList(t, repo, "agent/agent-unmerged"), "the branch holding the commit survives")
	assert.Contains(t, string(cleanup.Stderr), "agent/agent-unmerged", "the branch that was kept is named")
}

// TestBuiltinWorktreeCleanupReportsUncommittedWorkItDestroys covers a dirty worktree. Nobody
// is left to ask by the time cleanup runs and the agent's row is already going, so the
// removal is not blocked; what it threw away is written to stderr, which the action logger
// keeps.
func TestBuiltinWorktreeCleanupReportsUncommittedWorkItDestroys(t *testing.T) {
	repo := initRepo(t)
	setup, err := runBuiltin(t, "_worktree_setup", worktreeEnv{agentID: "agent-dirty", repo: repo, worktreesDir: t.TempDir()})
	require.NoError(t, err, "stderr: %s", setup.Stderr)
	tree := strings.TrimSpace(string(setup.Stdout))

	require.NoError(t, os.WriteFile(filepath.Join(tree, "scratch.txt"), []byte("unsaved\n"), 0o644))

	cleanup, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{agentID: "agent-dirty", agentCwd: tree})
	require.NoError(t, err, "stderr: %s", cleanup.Stderr)

	assert.NoDirExists(t, tree, "a delete is not blocked by a dirty worktree")
	assert.Contains(t, string(cleanup.Stderr), "scratch.txt", "what was destroyed is named")
}

// TestBuiltinWorktreeCleanupRefusesAWorktreeHoldingAnother covers a worktree with another
// registered worktree inside it, which an agent working on worktrees makes for itself.
// worktree remove --force reads the inner one as an ordinary untracked directory, deletes
// its files, and leaves git holding a registration for a checkout that is no longer there.
func TestBuiltinWorktreeCleanupRefusesAWorktreeHoldingAnother(t *testing.T) {
	repo := initRepo(t)
	setup, err := runBuiltin(t, "_worktree_setup", worktreeEnv{agentID: "agent-nest", repo: repo, worktreesDir: t.TempDir()})
	require.NoError(t, err, "stderr: %s", setup.Stderr)
	tree := strings.TrimSpace(string(setup.Stdout))

	inner := filepath.Join(tree, "inner")
	runGit(t, repo, "worktree", "add", "--quiet", "-b", "inner-branch", inner)

	cleanup, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{agentID: "agent-nest", agentCwd: tree})
	require.Error(t, err, "the refusal is reported rather than exiting zero")
	assert.Contains(t, string(cleanup.Stderr), "holds another registered worktree")

	assertIsDir(t, inner)
	assert.NotEmpty(t, readBranchList(t, repo, "inner-branch"), "the inner worktree is left registered and intact")
}

// TestBuiltinWorktreeCleanupHandlesADetachedHead covers an agent that left its worktree on
// something other than the branch it was given, which a checkout of a sha, an interrupted
// rebase, or a bisect all do. The path weave chose is the proof of ownership for that reason:
// the branch a tree happens to be on is not one the agent has to leave alone.
func TestBuiltinWorktreeCleanupHandlesADetachedHead(t *testing.T) {
	repo := initRepo(t)
	setup, err := runBuiltin(t, "_worktree_setup", worktreeEnv{agentID: "agent-detached", repo: repo, worktreesDir: t.TempDir()})
	require.NoError(t, err, "stderr: %s", setup.Stderr)
	tree := strings.TrimSpace(string(setup.Stdout))

	runGit(t, tree, "checkout", "--quiet", "--detach", "HEAD")

	cleanup, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{agentID: "agent-detached", agentCwd: tree})
	require.NoError(t, err, "stderr: %s", cleanup.Stderr)

	assert.NoDirExists(t, tree, "a detached head is still this agent's worktree")
	assert.Empty(t, readBranchList(t, repo, "agent/agent-detached"), "the branch goes with it")
}

// TestBuiltinWorktreeCleanupDeletesABranchHoldingNothingNew covers the repo whose local base
// is behind origin, which is most repos most of the time. The worktree's branch has no
// upstream, so measuring it against the repo's own HEAD reads a branch cut from origin with
// no commits of its own as unmerged, and keeping every one of those leaves a branch per agent.
func TestBuiltinWorktreeCleanupDeletesABranchHoldingNothingNew(t *testing.T) {
	upstream := initBareRepo(t)
	repo := cloneRepo(t, upstream)
	worktreesDir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(repo, "second.txt"), []byte("second\n"), 0o644))
	runGit(t, repo, "add", "second.txt")
	runGit(t, repo, "commit", "--quiet", "-m", "a commit only origin has")
	runGit(t, repo, "push", "--quiet", "origin", "main")
	runGit(t, repo, "reset", "--quiet", "--hard", "HEAD~1")

	setup, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      "agent-nothing-new",
		repo:         repo,
		worktreesDir: worktreesDir,
		baseBranch:   "main",
	})
	require.NoError(t, err, "stderr: %s", setup.Stderr)
	tree := strings.TrimSpace(string(setup.Stdout))

	cleanup, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{agentID: "agent-nothing-new", agentCwd: tree})
	require.NoError(t, err, "stderr: %s", cleanup.Stderr)

	assert.NoDirExists(t, tree)
	assert.Empty(t, readBranchList(t, repo, "agent/agent-nothing-new"), "a branch whose commits are all on origin is not worth keeping")
}

// TestBuiltinWorktreeCleanupRefusesWhenTheWorktreeListCannotBeRead covers the listing the
// nested-worktree guard is built on failing. An unreadable list is not proof that nothing is
// nested, and reading it as one deletes the files of whatever was.
func TestBuiltinWorktreeCleanupRefusesWhenTheWorktreeListCannotBeRead(t *testing.T) {
	repo := initRepo(t)
	setup, err := runBuiltin(t, "_worktree_setup", worktreeEnv{agentID: "agent-blind", repo: repo, worktreesDir: t.TempDir()})
	require.NoError(t, err, "stderr: %s", setup.Stderr)
	tree := strings.TrimSpace(string(setup.Stdout))

	cleanup, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{
		agentID:    "agent-blind",
		agentCwd:   tree,
		pathPrefix: failingGitDir(t, "worktree list"),
	})
	require.Error(t, err, "the refusal is reported rather than exiting zero")

	assertIsDir(t, tree)
	assert.Contains(t, string(cleanup.Stderr), "could not list the worktrees")
	assert.NotEmpty(t, readBranchList(t, repo, "agent/agent-blind"), "nothing was removed")
}

func failingGitDir(t *testing.T, failFor string) string {
	t.Helper()

	real, err := exec.LookPath("git")
	require.NoError(t, err)

	dir := t.TempDir()
	shim := fmt.Sprintf(`#!/bin/sh
case " $* " in
*%q*) echo "fatal: shimmed failure for %s" >&2; exit 1 ;;
esac
exec %q "$@"
`, failFor, failFor, real)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "git"), []byte(shim), 0o755))

	return dir
}

type worktreeEnv struct {
	agentID      string
	agentCwd     string
	repo         string
	worktreesDir string
	baseBranch   string
	copyPaths    []string
	symlinkPaths []string

	pathPrefix string
}

func (e worktreeEnv) toEnv() []string {
	env := []string{
		"WEAVE_AGENT_ID=" + e.agentID,
		"WEAVE_AGENT_CWD=" + e.agentCwd,
		"WEAVE_WORKTREE_REPO=" + e.repo,
		"WEAVE_WORKTREES_DIR=" + e.worktreesDir,
		"WEAVE_WORKTREE_BASE_BRANCH=" + e.baseBranch,
		"WEAVE_WORKTREE_COPY_PATHS=" + strings.Join(e.copyPaths, "\n"),
		"WEAVE_WORKTREE_SYMLINK_PATHS=" + strings.Join(e.symlinkPaths, "\n"),
	}

	if e.pathPrefix != "" {
		env = append(env, "PATH="+e.pathPrefix+string(os.PathListSeparator)+os.Getenv("PATH"))
	}

	return env
}

func runBuiltin(t *testing.T, name string, env worktreeEnv) (Result, error) {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, InstallBuiltins(dir))

	action, err := findBuiltin(dir, name)
	require.NoError(t, err)

	dirFile, err := os.CreateTemp(t.TempDir(), "setup-dir-*")
	require.NoError(t, err)
	require.NoError(t, dirFile.Close())

	run := runAction(actionRun{runner: NewRunner(), action: action, env: env, dirPath: dirFile.Name()})

	return run.result, run.err
}

func findBuiltin(dir, name string) (*Action, error) {
	discovered, err := Discover(dir)
	if err != nil {
		return nil, err
	}

	for _, action := range discovered {
		if action.Name == name {
			return &action, nil
		}
	}

	return nil, fmt.Errorf("%s is not a builtin action", name)
}

type actionRun struct {
	runner  Runner
	action  *Action
	env     worktreeEnv
	dirPath string
}

func runAction(run actionRun) outcome {
	result, err := run.runner.RunAndWait(context.Background(), RunParams{
		Action: *run.action,
		Dir:    run.action.GetDir(),
		Env:    append(run.env.toEnv(), "WEAVE_SETUP_DIR_FILE="+run.dirPath),
	})

	if named, readErr := os.ReadFile(run.dirPath); readErr == nil {
		result.Stdout = named
	}

	return outcome{result: result, err: err}
}

type outcome struct {
	result Result
	err    error
}

func runBuiltinConcurrently(t *testing.T, name string, envs []worktreeEnv) []outcome {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, InstallBuiltins(dir))

	action, err := findBuiltin(dir, name)
	require.NoError(t, err)

	runner := NewRunner()
	dirFiles := t.TempDir()
	outcomes := make([]outcome, len(envs))

	var wg sync.WaitGroup
	for i, env := range envs {
		wg.Add(1)

		go func() {
			defer wg.Done()

			outcomes[i] = runAction(actionRun{
				runner:  runner,
				action:  action,
				env:     env,
				dirPath: filepath.Join(dirFiles, fmt.Sprintf("setup-dir-%d", i)),
			})
		}()
	}
	wg.Wait()

	return outcomes
}

func hermeticGit(t *testing.T) {
	t.Helper()

	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
}

func initRepo(t *testing.T) string {
	t.Helper()

	hermeticGit(t)

	dir := t.TempDir()
	runGit(t, dir, "init", "--quiet", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@weave.test")
	runGit(t, dir, "config", "user.name", "weave test")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644))
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "--quiet", "-m", "initial")

	return dir
}

func commitAll(t *testing.T, dir string) {
	t.Helper()

	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "--quiet", "-m", "add fixtures")
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
	runGit(t, dir, "config", "user.email", "test@weave.test")
	runGit(t, dir, "config", "user.name", "weave test")

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

func readGitConfig(t *testing.T, dir, key string) string {
	t.Helper()

	out, err := exec.Command("git", "-C", dir, "config", "--get", key).Output()
	if err != nil {
		var exit *exec.ExitError
		require.ErrorAs(t, err, &exit)
		require.Equal(t, 1, exit.ExitCode(), "git config failed for a reason other than the key being unset: %s", exit.Stderr)

		return ""
	}

	return strings.TrimSpace(string(out))
}

func hasRef(t *testing.T, repo, ref string) bool {
	t.Helper()

	return exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", ref).Run() == nil
}

func pushCommit(t *testing.T, upstream, branch, name string) {
	t.Helper()

	publisher := cloneRepo(t, upstream)
	runGit(t, publisher, "checkout", "--quiet", branch)
	require.NoError(t, os.WriteFile(filepath.Join(publisher, name), []byte("published\n"), 0o644))
	commitAll(t, publisher)
	runGit(t, publisher, "push", "--quiet", "origin", branch)
}

func readSymbolicRef(t *testing.T, repo, ref string) string {
	t.Helper()

	out, err := exec.Command("git", "-C", repo, "symbolic-ref", "--quiet", ref).Output()
	require.NoError(t, err)

	return strings.TrimSpace(string(out))
}

func readBranchList(t *testing.T, repo, name string) string {
	t.Helper()

	out, err := exec.Command("git", "-C", repo, "branch", "--list", name).Output()
	require.NoError(t, err)

	return strings.TrimSpace(string(out))
}

func assertBranch(t *testing.T, tree, want string) {
	t.Helper()

	out, err := exec.Command("git", "-C", tree, "rev-parse", "--abbrev-ref", "HEAD").Output()
	require.NoError(t, err)
	assert.Equal(t, want, strings.TrimSpace(string(out)))
}
