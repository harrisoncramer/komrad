//go:build integration

package scripts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationBuiltinWorktreeSetup exercises the shipped _worktree_setup script for real, the way
// a workflow's provisioner actually runs: installed to disk, found by name, and run
// through the same Runner every other script uses.
func TestIntegrationBuiltinWorktreeSetup(t *testing.T) {
	repo := initRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".env"), []byte("SECRET=1\n"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(repo, "node_modules"), 0o755))

	worktreesDir := t.TempDir()

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-a",
		repo:         repo,
		worktreesDir: worktreesDir,
		copyPaths:    []string{".env"},
		symlinkPaths: []string{"node_modules"},
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	tree := strings.TrimSpace(string(result.Stdout))
	assert.Equal(t, filepath.Join(worktreesDir, "task-a"), tree)
	assertIsDir(t, tree)
	assertBranch(t, tree, "task/task-a")
	assert.Equal(t, "komrad task task-a", readLockReason(t, tree), "the registration is locked for its task")

	envContent, err := os.ReadFile(filepath.Join(tree, ".env"))
	require.NoError(t, err)
	assert.Equal(t, "SECRET=1\n", string(envContent))

	link, err := os.Readlink(filepath.Join(tree, "node_modules"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(repo, "node_modules"), link)
}

// TestIntegrationBuiltinWorktreeSetupIsIdempotentOnResume covers the one thing a provisioner has to
// get right: rerunning it against a worktree that is still there must not fail or make a
// second one, and must print the same path both times.
func TestIntegrationBuiltinWorktreeSetupIsIdempotentOnResume(t *testing.T) {
	repo := initRepo(t)
	worktreesDir := t.TempDir()
	env := worktreeEnv{
		taskID:       "task-b",
		repo:         repo,
		worktreesDir: worktreesDir,
	}

	first, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", first.Stderr)

	env.trigger = "agent.resume"
	env.agentCwd = strings.TrimSpace(string(first.Stdout))
	second, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", second.Stderr)

	assert.Equal(t, first.Stdout, second.Stdout, "the same agent gets the same worktree path both times")
	assert.Contains(t, string(second.Stderr), "already exists", "a resume says it found the worktree rather than trying to make it again")

	tree := strings.TrimSpace(string(first.Stdout))
	listed, err := exec.Command("git", "-C", repo, "worktree", "list").Output()
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(listed), tree), "only one worktree exists after the resume")
	assert.Equal(t, "komrad task task-b", readLockReason(t, tree), "the worktree stays locked across the resume")
}

// TestIntegrationBuiltinWorktreeSetupOnResumeLocksAnUnlockedTree covers a worktree whose lock is
// gone getting it back the next time its agent opens.
func TestIntegrationBuiltinWorktreeSetupOnResumeLocksAnUnlockedTree(t *testing.T) {
	repo := initRepo(t)
	env := worktreeEnv{
		taskID:       "task-unlocked",
		repo:         repo,
		worktreesDir: t.TempDir(),
	}

	first, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", first.Stderr)
	tree := strings.TrimSpace(string(first.Stdout))
	runGit(t, repo, "worktree", "unlock", tree)

	env.trigger = "agent.resume"
	env.agentCwd = tree
	second, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", second.Stderr)

	assert.Equal(t, "komrad task task-unlocked", readLockReason(t, tree))
}

// TestIntegrationBuiltinWorktreeSetupOnResumeStillWorksAgainstTheRepo covers a resume, where the agent's
// cwd is already the worktree and the setup would otherwise relink a symlink to point at itself.
func TestIntegrationBuiltinWorktreeSetupOnResumeStillWorksAgainstTheRepo(t *testing.T) {
	repo := initRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".env"), []byte("SECRET=1\n"), 0o644))

	env := worktreeEnv{
		taskID:       "task-resume-link",
		repo:         repo,
		agentCwd:     repo,
		worktreesDir: t.TempDir(),
		symlinkPaths: []string{".env"},
	}

	first, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", first.Stderr)
	tree := strings.TrimSpace(string(first.Stdout))

	env.trigger = "agent.resume"
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

// TestIntegrationBuiltinWorktreeSetupRefusesAPathWithADotSegment pins the script's own refusal, which has
// to stay safe on the spellings the service now cleans away before it stores them.
func TestIntegrationBuiltinWorktreeSetupRefusesAPathWithADotSegment(t *testing.T) {
	repo := initRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".env"), []byte("SECRET=1\n"), 0o644))

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-dot-segment",
		repo:         repo,
		worktreesDir: t.TempDir(),
		copyPaths:    []string{"./.env"},
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	tree := strings.TrimSpace(string(result.Stdout))
	assert.Contains(t, string(result.Stderr), "is not a path inside the worktree")
	assert.NoFileExists(t, filepath.Join(tree, ".env"))
	assertIsDir(t, tree)
}

// TestIntegrationBuiltinWorktreeSetupOnResumeLeavesAppliedPathsAlone covers copy and symlink lists being
// applied only to a worktree this run created, copying over a directory nesting it deeper.
func TestIntegrationBuiltinWorktreeSetupOnResumeLeavesAppliedPathsAlone(t *testing.T) {
	repo := initRepo(t)
	require.NoError(t, os.Mkdir(filepath.Join(repo, "node_modules"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "node_modules", "mod.txt"), []byte("dep\n"), 0o644))

	env := worktreeEnv{
		taskID:       "task-resume-copy",
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

	env.trigger = "agent.resume"
	env.agentCwd = tree
	second, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", second.Stderr)

	assert.NoDirExists(t, filepath.Join(tree, "node_modules", "node_modules"), "a resume does not copy the directory inside itself")

	content, err := os.ReadFile(edited)
	require.NoError(t, err)
	assert.Equal(t, "patched\n", string(content), "a resume does not overwrite what the agent changed")
}

// TestIntegrationBuiltinWorktreeSetupResumesAfterTheDirectoryIsGoneButTheBranchIsNot covers cleanup
// having removed the worktree directory but left the branch behind, or anything else
// deleting just the directory: setup has to be able to rebuild the worktree rather than
// failing with "branch already exists".
func TestIntegrationBuiltinWorktreeSetupResumesAfterTheDirectoryIsGoneButTheBranchIsNot(t *testing.T) {
	repo := initRepo(t)
	worktreesDir := t.TempDir()
	env := worktreeEnv{
		taskID:       "task-g",
		repo:         repo,
		agentCwd:     repo,
		worktreesDir: worktreesDir,
	}

	first, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", first.Stderr)
	tree := strings.TrimSpace(string(first.Stdout))

	runGit(t, repo, "worktree", "remove", "--force", "--force", tree)
	env.trigger = "agent.resume"
	env.agentCwd = tree

	branches, err := exec.Command("git", "-C", repo, "branch", "--list", "task/task-g").Output()
	require.NoError(t, err)
	require.NotEmpty(t, strings.TrimSpace(string(branches)), "the branch is still there after only the directory was removed")

	second, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", second.Stderr)
	assertIsDir(t, strings.TrimSpace(string(second.Stdout)))
}

// TestIntegrationBuiltinWorktreeSetupOnResumeKeepsTheTreeTheAgentHolds covers the worktrees directory
// pointing somewhere else after provisioning, where computing the path afresh would send the
// resume at a directory the branch is not checked out in.
func TestIntegrationBuiltinWorktreeSetupOnResumeKeepsTheTreeTheAgentHolds(t *testing.T) {
	repo := initRepo(t)
	env := worktreeEnv{
		taskID:       "task-moved-dir",
		repo:         repo,
		agentCwd:     repo,
		worktreesDir: t.TempDir(),
	}

	first, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", first.Stderr)
	tree := strings.TrimSpace(string(first.Stdout))

	moved := t.TempDir()
	env.trigger = "agent.resume"
	env.agentCwd = tree
	env.worktreesDir = moved

	second, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", second.Stderr)

	assert.Equal(t, tree, strings.TrimSpace(string(second.Stdout)), "the resume goes back to the tree the agent already has")
	assert.Contains(t, string(second.Stderr), "already exists", "the resume finds the worktree rather than trying to make it again")
	assert.NoDirExists(t, filepath.Join(moved, "task-moved-dir"), "nothing is made under the directory worktrees now live in")
}

// TestIntegrationBuiltinWorktreeSetupOnResumeRebuildsUnderTheConfiguredDirectory covers the tree an agent
// holds being gone as well as the worktrees directory having moved, where the registration the
// old tree left behind still claims the branch until it is pruned.
func TestIntegrationBuiltinWorktreeSetupOnResumeRebuildsUnderTheConfiguredDirectory(t *testing.T) {
	repo := initRepo(t)
	env := worktreeEnv{
		taskID:       "task-moved-gone",
		repo:         repo,
		agentCwd:     repo,
		worktreesDir: t.TempDir(),
	}

	first, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", first.Stderr)
	tree := strings.TrimSpace(string(first.Stdout))
	require.NoError(t, os.RemoveAll(tree))

	moved := t.TempDir()
	env.trigger = "agent.resume"
	env.agentCwd = tree
	env.worktreesDir = moved

	second, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", second.Stderr)

	rebuilt := strings.TrimSpace(string(second.Stdout))
	assert.Equal(t, filepath.Join(moved, "task-moved-gone"), rebuilt)
	assertIsDir(t, rebuilt)
	assertBranch(t, rebuilt, "task/task-moved-gone")
}

// TestIntegrationBuiltinWorktreeSetupOnProvisionUsesTheConfiguredDirectory covers a create never going back
// to a cwd.
func TestIntegrationBuiltinWorktreeSetupOnProvisionUsesTheConfiguredDirectory(t *testing.T) {
	repo := initRepo(t)
	env := worktreeEnv{
		taskID:       "task-provision-dir",
		repo:         repo,
		agentCwd:     repo,
		worktreesDir: t.TempDir(),
	}

	first, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", first.Stderr)
	tree := strings.TrimSpace(string(first.Stdout))

	moved := t.TempDir()
	env.agentCwd = tree
	env.worktreesDir = moved

	second, _ := runBuiltin(t, "_worktree_setup", env)
	assert.Contains(t, string(second.Stderr), filepath.Join(moved, "task-provision-dir"), "the create aims at the directory worktrees live in now")
	assert.NotContains(t, string(second.Stderr), "already exists", "the tree the cwd names is not taken for a create")
}

// TestIntegrationBuiltinWorktreeSetupOnResumeRefusesACwdThatIsTheRepo covers a create that failed before
// setup replaced the directory, which leaves the agent's cwd naming the workflow's own
// checkout: resuming into that would run the agent inside the repository.
func TestIntegrationBuiltinWorktreeSetupOnResumeRefusesACwdThatIsTheRepo(t *testing.T) {
	repo := initRepo(t)
	taskID := filepath.Base(repo)
	worktreesDir := t.TempDir()

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       taskID,
		repo:         repo,
		agentCwd:     repo,
		trigger:      "agent.resume",
		worktreesDir: worktreesDir,
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	tree := strings.TrimSpace(string(result.Stdout))
	assert.Equal(t, filepath.Join(worktreesDir, taskID), tree, "the repository is not taken for the agent's worktree")
	assertIsDir(t, tree)
	assertBranch(t, tree, "task/"+taskID)
}

// TestIntegrationBuiltinWorktreeSetupCopiesADirectory covers a copy path naming a directory rather
// than a file: cp without -r would fail and silently drop the rest of the list.
func TestIntegrationBuiltinWorktreeSetupCopiesADirectory(t *testing.T) {
	repo := initRepo(t)
	require.NoError(t, os.Mkdir(filepath.Join(repo, "assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "assets", "logo.png"), []byte("fake-png"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "after.env"), []byte("AFTER=1\n"), 0o644))

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-h",
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

// TestIntegrationBuiltinWorktreeSetupSkipsATrackedCopyPath pins that a path git already tracks is left
// to the checkout rather than copied over with the version in the working tree.
func TestIntegrationBuiltinWorktreeSetupSkipsATrackedCopyPath(t *testing.T) {
	repo := initRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "settings.json"), []byte("committed\n"), 0o644))
	commitAll(t, repo)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "settings.json"), []byte("local\n"), 0o644))

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-tracked-skip",
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

// TestIntegrationBuiltinWorktreeSetupSkipsATrackedSymlinkPath pins that a tracked directory is left as
// git checked it out rather than replaced with a link.
func TestIntegrationBuiltinWorktreeSetupSkipsATrackedSymlinkPath(t *testing.T) {
	repo := initRepo(t)
	require.NoError(t, os.Mkdir(filepath.Join(repo, "vendor"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "vendor", "lib.txt"), []byte("dep\n"), 0o644))
	commitAll(t, repo)

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-tracked-link",
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

// TestIntegrationBuiltinWorktreeSetupRefusesAPathOutsideTheWorktree covers what clearing a destination
// costs: a path that resolves out of the worktree, or names the worktree itself, would have
// setup delete something it was never pointed at.
func TestIntegrationBuiltinWorktreeSetupRefusesAPathOutsideTheWorktree(t *testing.T) {
	repo := initRepo(t)
	worktreesDir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(filepath.Dir(repo), "sibling"), 0o755))

	outside := filepath.Join(worktreesDir, "sibling")
	require.NoError(t, os.Mkdir(outside, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("keep\n"), 0o644))

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-escape",
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

// TestIntegrationBuiltinWorktreeSetupRefusesAPathUnderALinkAnEarlierEntryMade pins that the setup refuses a second entry
// reaching through a link the first one made rather than writing into the repo.
func TestIntegrationBuiltinWorktreeSetupRefusesAPathUnderALinkAnEarlierEntryMade(t *testing.T) {
	repo := initRepo(t)
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "local", "bin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "local", "bin", "tool"), []byte("tool\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("local/\n"), 0o644))
	commitAll(t, repo)

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-nested-link",
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

// TestIntegrationBuiltinWorktreeSetupRefusesAPathUnderATrackedSymlink pins that the setup refuses a
// path resolving through a tracked symlink out of the repo.
func TestIntegrationBuiltinWorktreeSetupRefusesAPathUnderATrackedSymlink(t *testing.T) {
	repo := initRepo(t)
	outside := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(outside, "cache"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "cache", "keep.txt"), []byte("keep\n"), 0o644))
	require.NoError(t, os.Symlink(outside, filepath.Join(repo, "shared")))
	commitAll(t, repo)

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-tracked-symlink",
		repo:         repo,
		worktreesDir: t.TempDir(),
		copyPaths:    []string{"shared/cache"},
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	assert.Contains(t, string(result.Stderr), "shared/cache is not a path inside the worktree")
	assert.FileExists(t, filepath.Join(outside, "cache", "keep.txt"), "the directory outside the repo survives")
}

// TestIntegrationBuiltinWorktreeSetupAppliesAPathWrittenWithATrailingSlash pins that a trailing slash
// names the same path, so the entry is not applied twice.
func TestIntegrationBuiltinWorktreeSetupAppliesAPathWrittenWithATrailingSlash(t *testing.T) {
	repo := initRepo(t)
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "local", "bin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "local", "bin", "tool"), []byte("tool\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("local/\n"), 0o644))
	commitAll(t, repo)

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-trailing-slash",
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

// TestIntegrationBuiltinWorktreeSetupSkipsATrackedSymlinkWrittenWithATrailingSlash pins that the
// tracked check still recognizes the path when it is written with a trailing slash.
func TestIntegrationBuiltinWorktreeSetupSkipsATrackedSymlinkWrittenWithATrailingSlash(t *testing.T) {
	repo := initRepo(t)
	outside := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(outside, "cache"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "cache", "keep.txt"), []byte("keep\n"), 0o644))
	require.NoError(t, os.Symlink(outside, filepath.Join(repo, "shared")))
	commitAll(t, repo)

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-tracked-symlink-slash",
		repo:         repo,
		worktreesDir: t.TempDir(),
		copyPaths:    []string{"shared/"},
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	assert.FileExists(t, filepath.Join(outside, "cache", "keep.txt"), "the directory outside the repo survives")
}

// TestIntegrationBuiltinWorktreeCleanupRefusesAPlainCheckout covers KOMRAD_AGENT_CWD pointing at a
// checkout that is not a linked worktree at all, such as the repo komrad itself lives in:
// cleanup must not run worktree remove or branch -D against it.
func TestIntegrationBuiltinWorktreeCleanupRefusesAPlainCheckout(t *testing.T) {
	repo := initRepo(t)
	runGit(t, repo, "branch", "task/task-i")

	result, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{taskID: "task-i", agentCwd: repo})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	assertIsDir(t, repo)
	branches, err := exec.Command("git", "-C", repo, "branch", "--list", "task/task-i").Output()
	require.NoError(t, err)
	assert.NotEmpty(t, strings.TrimSpace(string(branches)), "the branch survives since this was never a linked worktree")
}

// TestIntegrationBuiltinWorktreeSetupCopyMissingSourceIsNonFatal covers a copy or symlink entry naming a
// file the repo does not have, where setup warns on stderr and keeps going.
func TestIntegrationBuiltinWorktreeSetupCopyMissingSourceIsNonFatal(t *testing.T) {
	repo := initRepo(t)

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-c",
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

// TestIntegrationBuiltinWorktreeSetupRefusesABlankBase covers a workflow naming no base, which setup
// refuses rather than guessing a branch of its own.
func TestIntegrationBuiltinWorktreeSetupRefusesABlankBase(t *testing.T) {
	worktreesDir := t.TempDir()
	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-d",
		repo:         initRepo(t),
		worktreesDir: worktreesDir,
		noBase:       true,
	})
	require.Error(t, err, "the provisioner fails rather than picking a branch of its own")

	assert.Contains(t, string(result.Stderr), "names no base branch")
	assert.NoDirExists(t, filepath.Join(worktreesDir, "task-d"), "no worktree was made")
}

// TestIntegrationBuiltinWorktreeSetupFetchesFromOrigin covers the repo that has a remote: setup has to
// fetch the base branch and build the worktree off origin's copy of it rather than
// whatever the local repo happens to already have.
func TestIntegrationBuiltinWorktreeSetupFetchesFromOrigin(t *testing.T) {
	upstream := initBareRepo(t)
	repo := cloneRepo(t, upstream)

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-e",
		repo:         repo,
		worktreesDir: t.TempDir(),
		baseBranch:   "main",
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)
	assert.Contains(t, string(result.Stderr), "fetching main from origin")
	assertIsDir(t, strings.TrimSpace(string(result.Stdout)))
}

// TestIntegrationBuiltinWorktreeSetupBranchesFromABaseTheRefspecDoesNotCover covers a single-branch clone,
// where fetching the default branch exits zero and still writes no ref to branch from.
func TestIntegrationBuiltinWorktreeSetupBranchesFromABaseTheRefspecDoesNotCover(t *testing.T) {
	upstream := initBareRepo(t)
	pushBranch(t, upstream, "feature", "feature-only.txt")
	repo := singleBranchClone(t, upstream, "feature")

	require.False(t, hasRef(t, repo, "refs/remotes/origin/main"), "the clone's refspec covers only the branch it asked for")

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-single-branch",
		repo:         repo,
		worktreesDir: t.TempDir(),
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	tree := strings.TrimSpace(string(result.Stdout))
	assert.Contains(t, string(result.Stderr), "fetching main from origin")
	assert.FileExists(t, filepath.Join(tree, "README.md"))
	assert.NoFileExists(t, filepath.Join(tree, "feature-only.txt"), "the worktree is cut from origin's default branch rather than the one the clone happens to hold")
	assert.False(t, hasRef(t, repo, "refs/remotes/origin/main"), "the human's remote refs are left as their refspec asked for")
	assert.False(t, hasRef(t, repo, "refs/komrad/base/agent-of-task-single-branch"), "the ref that named the branch point for one command is not left behind")
}

// TestIntegrationBuiltinWorktreeSetupBranchesFromABaseWithNoRefspecAtAll covers the repo wired to a
// remote by hand rather than cloned, which has a URL to fetch from and no refspec to map
// anything it fetches, so no fetch by branch name ever writes a tracking ref.
func TestIntegrationBuiltinWorktreeSetupBranchesFromABaseWithNoRefspecAtAll(t *testing.T) {
	upstream := initBareRepo(t)

	repo := initRepo(t)
	runGit(t, repo, "config", "remote.origin.url", upstream)

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-no-refspec",
		repo:         repo,
		worktreesDir: t.TempDir(),
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	assert.Contains(t, string(result.Stderr), "fetching main from origin")
	assertIsDir(t, strings.TrimSpace(string(result.Stdout)))
}

// TestIntegrationBuiltinWorktreeSetupStagesAnUncoveredBasePerAgent covers two setups fetching different
// bases at once, where the shared FETCH_HEAD would leave each reading the other's.
func TestIntegrationBuiltinWorktreeSetupStagesAnUncoveredBasePerAgent(t *testing.T) {
	upstream := initBareRepo(t)
	pushBranch(t, upstream, "other", "other-only.txt")
	pushBranch(t, upstream, "feature", "feature-only.txt")
	pushCommit(t, upstream, "main", "main-only.txt")
	repo := singleBranchClone(t, upstream, "feature")

	worktreesDir := t.TempDir()
	outcomes := runBuiltinConcurrently(t, "_worktree_setup", []worktreeEnv{
		{
			taskID:       "task-on-main",
			repo:         repo,
			worktreesDir: worktreesDir,
			baseBranch:   "main",
		},
		{
			taskID:       "task-on-other",
			repo:         repo,
			worktreesDir: worktreesDir,
			baseBranch:   "other",
		},
	})

	wants := []struct {
		taskID string
		file   string
		absent string
	}{
		{
			taskID: "task-on-main",
			file:   "main-only.txt",
			absent: "other-only.txt",
		},
		{
			taskID: "task-on-other",
			file:   "other-only.txt",
			absent: "main-only.txt",
		},
	}

	for i, want := range wants {
		require.NoErrorf(t, outcomes[i].err, "%s: stderr: %s", want.taskID, outcomes[i].result.Stderr)

		tree := filepath.Join(worktreesDir, want.taskID)
		assert.FileExists(t, filepath.Join(tree, want.file), "%s is on the base its own workflow named", want.taskID)
		assert.NoFileExists(t, filepath.Join(tree, want.absent), "%s did not read the other setup's branch point", want.taskID)
	}
}

// TestIntegrationBuiltinWorktreeSetupRefusesABaseTheRepoCannotReach covers a workflow naming a base nothing
// has, where letting git fail with invalid reference reads as a broken repo instead.
func TestIntegrationBuiltinWorktreeSetupRefusesABaseTheRepoCannotReach(t *testing.T) {
	upstream := initBareRepo(t)
	repo := cloneRepo(t, upstream)

	worktreesDir := t.TempDir()
	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-bad-base",
		repo:         repo,
		worktreesDir: worktreesDir,
		baseBranch:   "no-such-branch",
	})
	require.Error(t, err, "the provisioner fails rather than handing the add a reference nothing resolved")

	assert.Contains(t, string(result.Stderr), "no-such-branch is not a branch")
	assert.NoDirExists(t, filepath.Join(worktreesDir, "task-bad-base"), "no worktree was made")
}

// TestIntegrationBuiltinWorktreeCleanup covers the ordinary case: the worktree and its branch both
// exist, and cleanup removes both.
func TestIntegrationBuiltinWorktreeCleanup(t *testing.T) {
	repo := initRepo(t)
	setup, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-f",
		repo:         repo,
		worktreesDir: t.TempDir(),
	})
	require.NoError(t, err, "stderr: %s", setup.Stderr)
	tree := strings.TrimSpace(string(setup.Stdout))

	cleanup, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{taskID: "task-f", agentCwd: tree})
	require.NoError(t, err, "stderr: %s", cleanup.Stderr)

	_, statErr := os.Stat(tree)
	assert.True(t, os.IsNotExist(statErr), "the worktree directory is gone")

	branches, err := exec.Command("git", "-C", repo, "branch", "--list", "task/task-f").Output()
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(string(branches)), "the branch is gone")
}

// TestIntegrationBuiltinWorktreeCleanupRemovesATreeWhoseLockIsGone covers a person having unlocked
// the worktree before komrad gives it back.
func TestIntegrationBuiltinWorktreeCleanupRemovesATreeWhoseLockIsGone(t *testing.T) {
	repo := initRepo(t)
	setup, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-lock-gone",
		repo:         repo,
		worktreesDir: t.TempDir(),
	})
	require.NoError(t, err, "stderr: %s", setup.Stderr)
	tree := strings.TrimSpace(string(setup.Stdout))
	runGit(t, repo, "worktree", "unlock", tree)

	cleanup, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{taskID: "task-lock-gone", repo: repo, agentCwd: tree})
	require.NoError(t, err, "stderr: %s", cleanup.Stderr)

	assert.NoDirExists(t, tree)
	assert.Empty(t, readBranchList(t, repo, "task/task-lock-gone"))
}

// TestIntegrationBuiltinWorktreeCleanupDropsTheRegistrationOfATreeRemovedByHand covers the locked
// registration a deleted directory leaves, which no prune takes away.
func TestIntegrationBuiltinWorktreeCleanupDropsTheRegistrationOfATreeRemovedByHand(t *testing.T) {
	repo := initRepo(t)
	setup, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-by-hand",
		repo:         repo,
		worktreesDir: t.TempDir(),
	})
	require.NoError(t, err, "stderr: %s", setup.Stderr)
	tree := strings.TrimSpace(string(setup.Stdout))
	require.NoError(t, os.RemoveAll(tree))

	cleanup, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{taskID: "task-by-hand", repo: repo, agentCwd: tree})
	require.NoError(t, err, "stderr: %s", cleanup.Stderr)

	listed, err := exec.Command("git", "-C", repo, "worktree", "list", "--porcelain").Output()
	require.NoError(t, err)
	assert.NotContains(t, string(listed), "task-by-hand", "the registration is gone")
}

// TestIntegrationBuiltinWorktreeCleanupIsBestEffortWhenAlreadyGone covers what docs/scripts.md requires
// of every deprovisioner: a workflow's cleanup must not block a delete whatever state its
// target is in, so running cleanup against a directory that was never a worktree, or was
// already removed, must still exit zero.
func TestIntegrationBuiltinWorktreeCleanupIsBestEffortWhenAlreadyGone(t *testing.T) {
	result, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{taskID: "task-never-existed", agentCwd: t.TempDir()})
	require.NoError(t, err, "stderr: %s", result.Stderr)
}

// TestIntegrationBuiltinWorktreeSetupDoesNotTrackTheBase covers the new branch being left with no upstream,
// which tracking the base would have git status report it against and a plain push refuse.
func TestIntegrationBuiltinWorktreeSetupDoesNotTrackTheBase(t *testing.T) {
	upstream := initBareRepo(t)
	repo := cloneRepo(t, upstream)

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-untracked",
		repo:         repo,
		worktreesDir: t.TempDir(),
		baseBranch:   "main",
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)
	tree := strings.TrimSpace(string(result.Stdout))

	assert.Empty(t, readGitConfig(t, tree, "branch.task/task-untracked.remote"), "the branch has no upstream remote")
	assert.Empty(t, readGitConfig(t, tree, "branch.task/task-untracked.merge"), "the branch has no upstream ref")
	assert.Equal(t, "true", readGitConfig(t, tree, "push.autoSetupRemote"), "a plain push can create the remote branch")
}

// TestIntegrationBuiltinWorktreeSetupRunsConcurrentlyInOneRepo covers several agents created at once, where
// anything setup writes to the repo's shared config makes git fail the add rather than wait.
func TestIntegrationBuiltinWorktreeSetupRunsConcurrentlyInOneRepo(t *testing.T) {
	upstream := initBareRepo(t)
	repo := cloneRepo(t, upstream)
	worktreesDir := t.TempDir()

	const concurrent = 8

	envs := make([]worktreeEnv, 0, concurrent)
	for i := range concurrent {
		envs = append(envs, worktreeEnv{
			taskID:       fmt.Sprintf("task-parallel-%d", i),
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

// TestIntegrationBuiltinWorktreeSetupResumesAfterTheDirectoryIsDeletedOutright covers an rm -rf of the
// worktree, whose stale registration still claims the branch until it is pruned.
func TestIntegrationBuiltinWorktreeSetupResumesAfterTheDirectoryIsDeletedOutright(t *testing.T) {
	repo := initRepo(t)
	env := worktreeEnv{
		taskID:       "task-rmrf",
		repo:         repo,
		worktreesDir: t.TempDir(),
	}

	first, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", first.Stderr)
	tree := strings.TrimSpace(string(first.Stdout))

	require.NoError(t, os.RemoveAll(tree))

	env.trigger = "agent.resume"
	env.agentCwd = tree
	second, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", second.Stderr)
	assertIsDir(t, strings.TrimSpace(string(second.Stdout)))
	assertBranch(t, tree, "task/task-rmrf")
}

// TestIntegrationBuiltinWorktreeCleanupRefusesAWorktreeItDoesNotOwn covers KOMRAD_AGENT_CWD pointing at a
// linked worktree komrad did not make, which passes the same check a hand-made one does.
func TestIntegrationBuiltinWorktreeCleanupRefusesAWorktreeItDoesNotOwn(t *testing.T) {
	repo := initRepo(t)
	theirs := filepath.Join(t.TempDir(), "theirs")
	runGit(t, repo, "worktree", "add", "--quiet", "-b", "someone-else", theirs)

	result, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{taskID: "task-thief", agentCwd: theirs})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	assertIsDir(t, theirs)
	assert.NotEmpty(t, readBranchList(t, repo, "someone-else"), "someone else's branch survives")
}

// TestIntegrationBuiltinWorktreeCleanupKeepsAnUnmergedBranch covers an agent that committed work nobody
// merged, where the branch is the one recoverable trace of it.
func TestIntegrationBuiltinWorktreeCleanupKeepsAnUnmergedBranch(t *testing.T) {
	repo := initRepo(t)
	setup, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-unmerged",
		repo:         repo,
		worktreesDir: t.TempDir(),
	})
	require.NoError(t, err, "stderr: %s", setup.Stderr)
	tree := strings.TrimSpace(string(setup.Stdout))

	require.NoError(t, os.WriteFile(filepath.Join(tree, "work.txt"), []byte("real work\n"), 0o644))
	runGit(t, tree, "add", "work.txt")
	runGit(t, tree, "commit", "--quiet", "-m", "the agent's work")

	cleanup, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{taskID: "task-unmerged", agentCwd: tree})
	require.NoError(t, err, "stderr: %s", cleanup.Stderr)

	assert.NoDirExists(t, tree, "the worktree still goes")
	assert.NotEmpty(t, readBranchList(t, repo, "task/task-unmerged"), "the branch holding the commit survives")
	assert.Contains(t, string(cleanup.Stderr), "task/task-unmerged", "the branch that was kept is named")
}

// TestIntegrationBuiltinWorktreeCleanupReportsUncommittedWorkItDestroys covers a dirty worktree, where
// nobody is left to ask so what was thrown away goes to stderr instead.
func TestIntegrationBuiltinWorktreeCleanupReportsUncommittedWorkItDestroys(t *testing.T) {
	repo := initRepo(t)
	setup, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-dirty",
		repo:         repo,
		worktreesDir: t.TempDir(),
	})
	require.NoError(t, err, "stderr: %s", setup.Stderr)
	tree := strings.TrimSpace(string(setup.Stdout))

	require.NoError(t, os.WriteFile(filepath.Join(tree, "scratch.txt"), []byte("unsaved\n"), 0o644))

	cleanup, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{taskID: "task-dirty", agentCwd: tree})
	require.NoError(t, err, "stderr: %s", cleanup.Stderr)

	assert.NoDirExists(t, tree, "a delete is not blocked by a dirty worktree")
	assert.Contains(t, string(cleanup.Stderr), "scratch.txt", "what was destroyed is named")
}

// TestIntegrationBuiltinWorktreeCleanupRefusesAWorktreeHoldingAnother covers a worktree with another
// registered inside it, which worktree remove --force would delete while leaving git holding
// its registration.
func TestIntegrationBuiltinWorktreeCleanupRefusesAWorktreeHoldingAnother(t *testing.T) {
	repo := initRepo(t)
	setup, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-nest",
		repo:         repo,
		worktreesDir: t.TempDir(),
	})
	require.NoError(t, err, "stderr: %s", setup.Stderr)
	tree := strings.TrimSpace(string(setup.Stdout))

	inner := filepath.Join(tree, "inner")
	runGit(t, repo, "worktree", "add", "--quiet", "-b", "inner-branch", inner)

	cleanup, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{taskID: "task-nest", agentCwd: tree})
	require.Error(t, err, "the refusal is reported rather than exiting zero")
	assert.Contains(t, string(cleanup.Stderr), "holds another registered worktree")

	assertIsDir(t, inner)
	assert.NotEmpty(t, readBranchList(t, repo, "inner-branch"), "the inner worktree is left registered and intact")
}

// TestIntegrationBuiltinWorktreeCleanupHandlesADetachedHead covers a worktree left off its branch by a
// checkout, an interrupted rebase, or a bisect, where the path komrad chose proves ownership.
func TestIntegrationBuiltinWorktreeCleanupHandlesADetachedHead(t *testing.T) {
	repo := initRepo(t)
	setup, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-detached",
		repo:         repo,
		worktreesDir: t.TempDir(),
	})
	require.NoError(t, err, "stderr: %s", setup.Stderr)
	tree := strings.TrimSpace(string(setup.Stdout))

	runGit(t, tree, "checkout", "--quiet", "--detach", "HEAD")

	cleanup, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{taskID: "task-detached", agentCwd: tree})
	require.NoError(t, err, "stderr: %s", cleanup.Stderr)

	assert.NoDirExists(t, tree, "a detached head is still this agent's worktree")
	assert.Empty(t, readBranchList(t, repo, "task/task-detached"), "the branch goes with it")
}

// TestIntegrationBuiltinWorktreeCleanupDeletesABranchHoldingNothingNew covers a local base behind origin,
// where measuring against the repo's own HEAD would read every agent's branch as unmerged.
func TestIntegrationBuiltinWorktreeCleanupDeletesABranchHoldingNothingNew(t *testing.T) {
	upstream := initBareRepo(t)
	repo := cloneRepo(t, upstream)
	worktreesDir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(repo, "second.txt"), []byte("second\n"), 0o644))
	runGit(t, repo, "add", "second.txt")
	runGit(t, repo, "commit", "--quiet", "-m", "a commit only origin has")
	runGit(t, repo, "push", "--quiet", "origin", "main")
	runGit(t, repo, "reset", "--quiet", "--hard", "HEAD~1")

	setup, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-nothing-new",
		repo:         repo,
		worktreesDir: worktreesDir,
		baseBranch:   "main",
	})
	require.NoError(t, err, "stderr: %s", setup.Stderr)
	tree := strings.TrimSpace(string(setup.Stdout))

	cleanup, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{taskID: "task-nothing-new", agentCwd: tree})
	require.NoError(t, err, "stderr: %s", cleanup.Stderr)

	assert.NoDirExists(t, tree)
	assert.Empty(t, readBranchList(t, repo, "task/task-nothing-new"), "a branch whose commits are all on origin is not worth keeping")
}

// TestIntegrationBuiltinWorktreeCleanupRefusesWhenTheWorktreeListCannotBeRead covers the nested-worktree
// guard failing, where an unreadable list does not prove that nothing is nested.
func TestIntegrationBuiltinWorktreeCleanupRefusesWhenTheWorktreeListCannotBeRead(t *testing.T) {
	repo := initRepo(t)
	setup, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-blind",
		repo:         repo,
		worktreesDir: t.TempDir(),
	})
	require.NoError(t, err, "stderr: %s", setup.Stderr)
	tree := strings.TrimSpace(string(setup.Stdout))

	cleanup, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{
		taskID:     "task-blind",
		agentCwd:   tree,
		pathPrefix: failingGitDir(t, "worktree list"),
	})
	require.Error(t, err, "the refusal is reported rather than exiting zero")

	assertIsDir(t, tree)
	assert.Contains(t, string(cleanup.Stderr), "could not list the worktrees")
	assert.NotEmpty(t, readBranchList(t, repo, "task/task-blind"), "nothing was removed")
}

// TestIntegrationBuiltinWorktreeCleanupRefusesATreeWhoseRepoHasMoved covers a worktree whose .git file
// points into a repo that is no longer at that path: git cannot answer at all, so cleanup has
// to fail rather than record a clean deprovision for a directory that is still on disk.
func TestIntegrationBuiltinWorktreeCleanupRefusesATreeWhoseRepoHasMoved(t *testing.T) {
	repo := initRepo(t)
	setup, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-adrift",
		repo:         repo,
		worktreesDir: t.TempDir(),
	})
	require.NoError(t, err, "stderr: %s", setup.Stderr)
	tree := strings.TrimSpace(string(setup.Stdout))

	resolvedTree, err := filepath.EvalSymlinks(tree)
	require.NoError(t, err)
	resolvedRepo, err := filepath.EvalSymlinks(repo)
	require.NoError(t, err)

	moved := filepath.Join(t.TempDir(), "moved")
	require.NoError(t, os.Rename(repo, moved)) //nolint:forbidigo // moves a directory, which WriteAtomic does not

	cleanup, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{taskID: "task-adrift", agentCwd: tree})
	require.Error(t, err, "the refusal is reported rather than exiting zero")

	assertIsDir(t, tree)
	assert.Contains(t, string(cleanup.Stderr), "git cannot read "+resolvedTree)
	assert.Contains(t, string(cleanup.Stderr), filepath.Join(resolvedRepo, ".git", "worktrees", "task-adrift"), "the message names the gitdir the .git file points at")
	assert.NotEmpty(t, readBranchList(t, moved, "task/task-adrift"), "nothing was removed")
}

// TestIntegrationBuiltinWorktreeCleanupRefusesATreeWhoseRegistrationIsGone covers the retry of a delete
// that failed partway: worktree remove already dropped the registration before it hit the
// error, so the second run finds a directory git no longer knows anything about.
func TestIntegrationBuiltinWorktreeCleanupRefusesATreeWhoseRegistrationIsGone(t *testing.T) {
	repo := initRepo(t)
	setup, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-unregistered",
		repo:         repo,
		worktreesDir: t.TempDir(),
	})
	require.NoError(t, err, "stderr: %s", setup.Stderr)
	tree := strings.TrimSpace(string(setup.Stdout))

	require.NoError(t, os.RemoveAll(filepath.Join(repo, ".git", "worktrees", "task-unregistered")))

	cleanup, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{taskID: "task-unregistered", agentCwd: tree})
	require.Error(t, err, "the refusal is reported rather than exiting zero")

	assertIsDir(t, tree)
	assert.Contains(t, string(cleanup.Stderr), "the registration is gone")
	assert.NotEmpty(t, readBranchList(t, repo, "task/task-unregistered"), "the branch is left for a person to deal with")
}

// TestIntegrationBuiltinWorktreeCleanupLeavesAWorktreeItDoesNotOwnAloneWhenGitCannotAnswer covers the two
// refusals meeting: the ownership claim is a fact about the path and the agent id alone, so a
// tree komrad never made still exits zero when there is no repo left to ask.
func TestIntegrationBuiltinWorktreeCleanupLeavesAWorktreeItDoesNotOwnAloneWhenGitCannotAnswer(t *testing.T) {
	repo := initRepo(t)
	theirs := filepath.Join(t.TempDir(), "theirs")
	runGit(t, repo, "worktree", "add", "--quiet", "-b", "someone-else", theirs)

	moved := filepath.Join(t.TempDir(), "moved")
	require.NoError(t, os.Rename(repo, moved)) //nolint:forbidigo // moves a directory, which WriteAtomic does not

	cleanup, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{taskID: "task-thief", agentCwd: theirs})
	require.NoError(t, err, "stderr: %s", cleanup.Stderr)

	assertIsDir(t, theirs)
	assert.Contains(t, string(cleanup.Stderr), "is not named task-thief")
	assert.NotEmpty(t, readBranchList(t, moved, "someone-else"), "someone else's branch survives")
}

func failingGitDir(t *testing.T, failFor string) string {
	t.Helper()

	gitPath, err := exec.LookPath("git")
	require.NoError(t, err)

	dir := t.TempDir()
	shim := fmt.Sprintf(`#!/bin/sh
case " $* " in
*%q*) echo "fatal: shimmed failure for %s" >&2; exit 1 ;;
esac
exec %q "$@"
`, failFor, failFor, gitPath)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "git"), []byte(shim), 0o755))

	return dir
}

type worktreeEnv struct {
	taskID       string
	agentCwd     string
	trigger      string
	repo         string
	worktreesDir string
	baseBranch   string
	// noBase leaves the base blank, which every other test fills with main.
	noBase       bool
	adoptBranch  string
	copyPaths    []string
	symlinkPaths []string
	setupCommand string
	pathPrefix   string
	// noWorktree leaves out the worktree's variables, as for a task that has none.
	noWorktree bool
}

func (e worktreeEnv) toEnv() []string {
	trigger := e.trigger
	if trigger == "" {
		trigger = "agent.provision"
	}

	baseBranch := e.baseBranch
	if baseBranch == "" && !e.noBase {
		baseBranch = "main"
	}

	env := []string{
		"KOMRAD_AGENT_ID=agent-of-" + e.taskID,
		"KOMRAD_AGENT_CWD=" + e.agentCwd,
		"KOMRAD_TRIGGER=" + trigger,
	}
	if !e.noWorktree {
		env = append(env,
			"KOMRAD_WORKTREE_REPO="+e.repo,
			"KOMRAD_WORKTREES_DIR="+e.worktreesDir,
			"KOMRAD_WORKTREE_BASE_BRANCH="+baseBranch,
			"KOMRAD_WORKTREE_ADOPT_BRANCH="+e.adoptBranch,
			"KOMRAD_WORKTREE_COPY_PATHS="+strings.Join(e.copyPaths, "\n"),
			"KOMRAD_WORKTREE_SYMLINK_PATHS="+strings.Join(e.symlinkPaths, "\n"),
			"KOMRAD_WORKTREE_SETUP_COMMAND="+e.setupCommand,
			"KOMRAD_WORKTREE_NAME="+e.taskID,
			"KOMRAD_WORKTREE_BRANCH=task/"+e.taskID,
		)
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

	script, err := findBuiltin(dir, name)
	require.NoError(t, err)

	dirFile, err := os.CreateTemp(t.TempDir(), "setup-dir-*")
	require.NoError(t, err)
	require.NoError(t, dirFile.Close())

	run := runScript(t.Context(), scriptRun{
		runner:  NewRunner(),
		script:  script,
		env:     env,
		dirPath: dirFile.Name(),
	})

	return run.result, run.err
}

func findBuiltin(dir, name string) (*Script, error) {
	discovered, err := discover(dir)
	if err != nil {
		return nil, err
	}

	for _, script := range discovered {
		if script.Name == name {
			return &script, nil
		}
	}

	return nil, fmt.Errorf("%s is not a builtin script", name)
}

type scriptRun struct {
	runner  Runner
	script  *Script
	env     worktreeEnv
	dirPath string
}

func runScript(ctx context.Context, run scriptRun) outcome {
	result, err := run.runner.RunAndWait(ctx, RunParams{
		Script: *run.script,
		Dir:    run.script.GetDir(),
		Env:    append(run.env.toEnv(), "KOMRAD_SETUP_DIR_FILE="+run.dirPath),
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

	script, err := findBuiltin(dir, name)
	require.NoError(t, err)

	runner := NewRunner()
	dirFiles := t.TempDir()
	outcomes := make([]outcome, len(envs))

	var wg sync.WaitGroup
	for i, env := range envs {
		wg.Add(1)

		go func() {
			defer wg.Done()

			outcomes[i] = runScript(t.Context(), scriptRun{
				runner:  runner,
				script:  script,
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
	runGit(t, dir, "config", "user.email", "test@komrad.test")
	runGit(t, dir, "config", "user.name", "komrad test")
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
	runGit(t, dir, "config", "user.email", "test@komrad.test")
	runGit(t, dir, "config", "user.name", "komrad test")

	return dir
}

func singleBranchClone(t *testing.T, upstream, branch string) string {
	t.Helper()

	dir := t.TempDir()
	runGitClone(t, "--quiet", "--single-branch", "--branch", branch, upstream, dir)
	runGit(t, dir, "config", "user.email", "test@komrad.test")
	runGit(t, dir, "config", "user.name", "komrad test")

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

func pushBranch(t *testing.T, upstream, branch, name string) {
	t.Helper()

	publisher := cloneRepo(t, upstream)
	runGit(t, publisher, "checkout", "--quiet", "-b", branch)
	require.NoError(t, os.WriteFile(filepath.Join(publisher, name), []byte("published\n"), 0o644))
	commitAll(t, publisher)
	runGit(t, publisher, "push", "--quiet", "origin", branch)
}

func readBranchList(t *testing.T, repo, name string) string {
	t.Helper()

	out, err := exec.Command("git", "-C", repo, "branch", "--list", name).Output()
	require.NoError(t, err)

	return strings.TrimSpace(string(out))
}

func readLockReason(t *testing.T, tree string) string {
	t.Helper()

	gitDir, err := exec.Command("git", "-C", tree, "rev-parse", "--absolute-git-dir").Output()
	require.NoError(t, err)

	reason, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(gitDir)), "locked"))
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	require.NoError(t, err)

	return strings.TrimSpace(string(reason))
}

func assertBranch(t *testing.T, tree, want string) {
	t.Helper()

	out, err := exec.Command("git", "-C", tree, "rev-parse", "--abbrev-ref", "HEAD").Output()
	require.NoError(t, err)
	assert.Equal(t, want, strings.TrimSpace(string(out)))
}

// TestIntegrationBuiltinWorktreeSetupAdoptsALocalBranch covers adoption with everything already on disk,
// where somebody else's branch is checked out as it stands rather than cut or reset.
func TestIntegrationBuiltinWorktreeSetupAdoptsALocalBranch(t *testing.T) {
	repo := initRepo(t)
	runGit(t, repo, "checkout", "--quiet", "-b", "someones/work")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "theirs.txt"), []byte("theirs\n"), 0o644))
	commitAll(t, repo)
	runGit(t, repo, "checkout", "--quiet", "main")

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-adopt-local",
		repo:         repo,
		worktreesDir: t.TempDir(),
		adoptBranch:  "someones/work",
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	tree := strings.TrimSpace(string(result.Stdout))
	assertIsDir(t, tree)
	assertBranch(t, tree, "someones/work")
	assert.FileExists(t, filepath.Join(tree, "theirs.txt"), "the commits already on the branch are what the agent is looking at")
}

// TestIntegrationBuiltinWorktreeSetupAdoptsABranchOnlyOnOrigin covers adopting a pull request nobody here has
// pulled yet.
func TestIntegrationBuiltinWorktreeSetupAdoptsABranchOnlyOnOrigin(t *testing.T) {
	upstream := initBareRepo(t)
	author := cloneRepo(t, upstream)
	runGit(t, author, "checkout", "--quiet", "-b", "feature/theirs")
	require.NoError(t, os.WriteFile(filepath.Join(author, "theirs.txt"), []byte("theirs\n"), 0o644))
	commitAll(t, author)
	runGit(t, author, "push", "--quiet", "origin", "feature/theirs")

	repo := cloneRepo(t, upstream)
	require.False(t, hasRef(t, repo, "refs/heads/feature/theirs"), "the branch is not in this checkout before adoption")

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-adopt-remote",
		repo:         repo,
		worktreesDir: t.TempDir(),
		adoptBranch:  "feature/theirs",
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)
	assert.Contains(t, string(result.Stderr), "fetching feature/theirs from origin")

	tree := strings.TrimSpace(string(result.Stdout))
	assertBranch(t, tree, "feature/theirs")
	assert.FileExists(t, filepath.Join(tree, "theirs.txt"))

	out, err := exec.Command("git", "-C", tree, "rev-parse", "--abbrev-ref", "HEAD@{upstream}").Output()
	require.NoError(t, err, "an adopted branch tracks the branch it was adopted from")
	assert.Equal(t, "origin/feature/theirs", strings.TrimSpace(string(out)))
}

// TestIntegrationBuiltinWorktreeSetupRefusesAnAdoptionItCannotReach covers a branch name that is not a
// branch anywhere, where making one up would put the agent on an empty branch wearing its name.
func TestIntegrationBuiltinWorktreeSetupRefusesAnAdoptionItCannotReach(t *testing.T) {
	repo := initRepo(t)

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-adopt-missing",
		repo:         repo,
		worktreesDir: t.TempDir(),
		adoptBranch:  "no/such/branch",
	})
	require.Error(t, err)
	assert.Contains(t, string(result.Stderr), "no/such/branch is not a branch")
}

// TestIntegrationBuiltinWorktreeCleanupKeepsAnAdoptedBranch covers adoption's cleanup, where the branch was
// somebody else's so removing the worktree is the whole job.
func TestIntegrationBuiltinWorktreeCleanupKeepsAnAdoptedBranch(t *testing.T) {
	repo := initRepo(t)
	runGit(t, repo, "branch", "someones/work")

	setup, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		taskID:       "task-adopt-cleanup",
		repo:         repo,
		worktreesDir: t.TempDir(),
		adoptBranch:  "someones/work",
	})
	require.NoError(t, err, "stderr: %s", setup.Stderr)
	tree := strings.TrimSpace(string(setup.Stdout))

	cleanup, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{
		taskID:      "task-adopt-cleanup",
		agentCwd:    tree,
		adoptBranch: "someones/work",
	})
	require.NoError(t, err, "stderr: %s", cleanup.Stderr)

	_, statErr := os.Stat(tree)
	assert.True(t, os.IsNotExist(statErr), "the worktree directory is gone")
	assert.True(t, hasRef(t, repo, "refs/heads/someones/work"), "the branch outlives the agent that adopted it")
}

// TestIntegrationBuiltinKomradCheckoutTeardown covers giving back what a komrad checkout claimed.
func TestIntegrationBuiltinKomradCheckoutTeardown(t *testing.T) {
	tests := []struct {
		name        string
		checkout    bool
		dirName     string
		dockerDown  bool
		noWorktree  bool
		wantFreed   bool
		wantCompose bool
	}{
		{
			name:       "a komrad checkout run for a task with no worktree is left alone",
			checkout:   true,
			dirName:    "task-a",
			noWorktree: true,
		},
		{
			name:        "a komrad checkout gives back its instance and stack",
			checkout:    true,
			dirName:     "task-a",
			wantFreed:   true,
			wantCompose: true,
		},
		{
			name:       "a komrad checkout with docker down still gives back its instance",
			checkout:   true,
			dirName:    "task-a",
			dockerDown: true,
			wantFreed:  true,
		},
		{
			name:     "a komrad checkout not named for its worktree is left alone",
			checkout: true,
			dirName:  "elsewhere",
		},
		{name: "any other checkout is left alone", dirName: "task-a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := filepath.Join(t.TempDir(), tt.dirName)
			require.NoError(t, os.MkdirAll(tree, 0o755))
			calls := t.TempDir()
			freed := filepath.Join(calls, "freed")
			dockered := filepath.Join(calls, "docker")

			if tt.checkout {
				tasks := filepath.Join(tree, "mise", "tasks")
				require.NoError(t, os.MkdirAll(tasks, 0o755))
				stub := fmt.Sprintf("#!/bin/sh\nprintf '%%s' \"$*\" > %q\n", freed)
				require.NoError(t, os.WriteFile(filepath.Join(tasks, "_test_instance"), []byte(stub), 0o755))
			}

			bin := t.TempDir()
			docker := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %q\n", dockered)
			if tt.dockerDown {
				docker = "#!/bin/sh\nexit 1\n"
			}
			require.NoError(t, os.WriteFile(filepath.Join(bin, "docker"), []byte(docker), 0o755))

			result, err := runBuiltin(t, "komrad-checkout-teardown", worktreeEnv{
				taskID:     "task-a",
				agentCwd:   tree,
				trigger:    "agent.pre-delete",
				pathPrefix: bin,
				noWorktree: tt.noWorktree,
			})
			require.NoError(t, err, "stderr: %s", result.Stderr)
			if tt.noWorktree {
				assert.Empty(t, result.Stderr, "a task with no worktree is not even looked at")
			}

			said, freedErr := os.ReadFile(freed)
			ran, dockerErr := os.ReadFile(dockered)
			if tt.wantFreed {
				require.NoError(t, freedErr)
				assert.Equal(t, "down --root "+tree, string(said))
			} else {
				assert.True(t, os.IsNotExist(freedErr), "no instance was given back")
			}

			if tt.wantCompose {
				require.NoError(t, dockerErr)
				assert.Contains(t, string(ran), "compose -p task-a --profile * down --volumes --remove-orphans --rmi local\n")
			} else {
				assert.NotContains(t, string(ran), "compose", "no stack was taken down")
			}
		})
	}
}

// TestIntegrationBuiltinKomradCheckoutArchive covers shrinking an archived komrad checkout.
func TestIntegrationBuiltinKomradCheckoutArchive(t *testing.T) {
	rebuilt := []string{"docs/node_modules", "local/custom-gcl", "local/custom-gcl.sum", "local/komradslot", "local/buf-plugin-komrad"}
	kept := []string{"docs/package.json", "local/recordings/tour.gif"}

	tests := []struct {
		name       string
		checkout   bool
		dirName    string
		noWorktree bool
		wantShrunk bool
	}{
		{
			name:       "a komrad checkout run for a task with no worktree is left alone",
			checkout:   true,
			dirName:    "task-a",
			noWorktree: true,
		},
		{
			name:       "a komrad checkout gives back its instance and drops what it rebuilds",
			checkout:   true,
			dirName:    "task-a",
			wantShrunk: true,
		},
		{
			name:     "a komrad checkout not named for its worktree is left alone",
			checkout: true,
			dirName:  "elsewhere",
		},
		{name: "any other checkout is left alone", dirName: "task-a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := filepath.Join(t.TempDir(), tt.dirName)
			freed := filepath.Join(t.TempDir(), "freed")

			for _, p := range append(slices.Clone(rebuilt), kept...) {
				require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(tree, p)), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(tree, p), nil, 0o644))
			}

			if tt.checkout {
				tasks := filepath.Join(tree, "mise", "tasks")
				require.NoError(t, os.MkdirAll(tasks, 0o755))
				stub := fmt.Sprintf("#!/bin/sh\nprintf '%%s' \"$*\" > %q\n", freed)
				require.NoError(t, os.WriteFile(filepath.Join(tasks, "_test_instance"), []byte(stub), 0o755))
			}

			result, err := runBuiltin(t, "komrad-checkout-archive", worktreeEnv{
				taskID:     "task-a",
				agentCwd:   tree,
				trigger:    "agent.post-archive",
				noWorktree: tt.noWorktree,
			})
			require.NoError(t, err, "stderr: %s", result.Stderr)
			if tt.noWorktree {
				assert.Empty(t, result.Stderr, "a task with no worktree is not even looked at")
			}

			said, freedErr := os.ReadFile(freed)
			if tt.wantShrunk {
				require.NoError(t, freedErr)
				assert.Equal(t, "down --root "+tree, string(said))
			} else {
				assert.True(t, os.IsNotExist(freedErr), "no instance was given back")
			}

			for _, p := range rebuilt {
				_, statErr := os.Stat(filepath.Join(tree, p))
				assert.Equal(t, tt.wantShrunk, os.IsNotExist(statErr), p)
			}

			for _, p := range kept {
				assert.FileExists(t, filepath.Join(tree, p))
			}
		})
	}
}

// TestIntegrationBuiltinWorktreeSetupCommand covers running the workflow's setup command in a
// worktree before its agent starts.
func TestIntegrationBuiltinWorktreeSetupCommand(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		resume   bool
		wantErr  bool
		wantRan  bool
		wantTree bool
	}{
		{
			name:    "a failed setup command removes the new worktree and its branch",
			command: `pwd -P > "$RAN"; exit 3`,
			wantErr: true,
			wantRan: true,
		},
		{
			name:     "a failed setup command keeps a resumed worktree",
			command:  `pwd -P > "$RAN"; exit 3`,
			resume:   true,
			wantErr:  true,
			wantRan:  true,
			wantTree: true,
		},
		{
			name:     "a workflow with no setup command runs nothing",
			wantTree: true,
		},
		{
			name:     "a new worktree runs the setup command inside it",
			command:  `pwd -P > "$RAN"`,
			wantRan:  true,
			wantTree: true,
		},
		{
			name:     "a resumed worktree runs the setup command again",
			command:  `pwd -P > "$RAN"`,
			resume:   true,
			wantRan:  true,
			wantTree: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ran := filepath.Join(t.TempDir(), "ran")
			t.Setenv("RAN", ran)

			repo := initRepo(t)
			env := worktreeEnv{
				taskID:       "task-setup",
				repo:         repo,
				worktreesDir: t.TempDir(),
			}
			tree := filepath.Join(env.worktreesDir, env.taskID)

			if tt.resume {
				first, err := runBuiltin(t, "_worktree_setup", env)
				require.NoError(t, err, "stderr: %s", first.Stderr)

				env.trigger = "agent.resume"
				env.agentCwd = tree
			}

			env.setupCommand = tt.command
			result, err := runBuiltin(t, "_worktree_setup", env)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrFailed)
			} else {
				require.NoError(t, err, "stderr: %s", result.Stderr)
			}

			listed, err := exec.Command("git", "-C", repo, "worktree", "list", "--porcelain").Output()
			require.NoError(t, err)
			if tt.wantTree {
				assert.DirExists(t, tree)
				assert.Contains(t, string(listed), tree)
			} else {
				assert.NoDirExists(t, tree)
				assert.NotContains(t, string(listed), tree)
			}
			assert.Equal(t, tt.wantTree, hasRef(t, repo, "refs/heads/task/"+env.taskID))

			said, err := os.ReadFile(ran)
			if !tt.wantRan {
				require.ErrorIs(t, err, os.ErrNotExist)

				return
			}
			require.NoError(t, err)

			dir, err := filepath.EvalSymlinks(env.worktreesDir)
			require.NoError(t, err)
			assert.Equal(t, filepath.Join(dir, env.taskID), strings.TrimSpace(string(said)))
		})
	}
}
