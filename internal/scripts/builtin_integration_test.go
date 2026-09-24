//go:build integration

package scripts

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

// TestIntegrationBuiltinWorktreeSetup exercises the shipped _worktree_setup script for real, the way
// a workflow's provisioner actually runs: installed to disk, found by name, and run
// through the same Runner every other script uses.
func TestIntegrationBuiltinWorktreeSetup(t *testing.T) {
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

// TestIntegrationBuiltinWorktreeSetupIsIdempotentOnResume covers the one thing a provisioner has to
// get right: rerunning it against a worktree that is still there must not fail or make a
// second one, and must print the same path both times.
func TestIntegrationBuiltinWorktreeSetupIsIdempotentOnResume(t *testing.T) {
	repo := initRepo(t)
	worktreesDir := t.TempDir()
	env := worktreeEnv{
		agentID:      "agent-b",
		repo:         repo,
		worktreesDir: worktreesDir,
	}

	first, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", first.Stderr)

	env.trigger = "resume"
	env.agentCwd = strings.TrimSpace(string(first.Stdout))
	second, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", second.Stderr)

	assert.Equal(t, first.Stdout, second.Stdout, "the same agent gets the same worktree path both times")
	assert.Contains(t, string(second.Stderr), "already exists", "a resume says it found the worktree rather than trying to make it again")

	tree := strings.TrimSpace(string(first.Stdout))
	listed, err := exec.Command("git", "-C", repo, "worktree", "list").Output()
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(listed), tree), "only one worktree exists after the resume")
}

// TestIntegrationBuiltinWorktreeSetupOnResumeStillWorksAgainstTheRepo covers a resume, where the agent's
// cwd is already the worktree and a symlink would otherwise be relinked to point at itself.
func TestIntegrationBuiltinWorktreeSetupOnResumeStillWorksAgainstTheRepo(t *testing.T) {
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

	env.trigger = "resume"
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
		agentID:      "agent-dot-segment",
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

	env.trigger = "resume"
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
		agentID:      "agent-g",
		repo:         repo,
		agentCwd:     repo,
		worktreesDir: worktreesDir,
	}

	first, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", first.Stderr)
	tree := strings.TrimSpace(string(first.Stdout))

	runGit(t, repo, "worktree", "remove", "--force", tree)
	env.trigger = "resume"
	env.agentCwd = tree

	branches, err := exec.Command("git", "-C", repo, "branch", "--list", "agent/agent-g").Output()
	require.NoError(t, err)
	require.NotEmpty(t, strings.TrimSpace(string(branches)), "the branch is still there after only the directory was removed")

	second, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", second.Stderr)
	assertIsDir(t, strings.TrimSpace(string(second.Stdout)))
}

// TestIntegrationBuiltinWorktreeSetupOnResumeKeepsTheTreeTheAgentHolds covers the worktrees directory
// being pointed somewhere else after the agent was provisioned, where computing the path
// afresh would send the resume at a directory the branch is not checked out in.
func TestIntegrationBuiltinWorktreeSetupOnResumeKeepsTheTreeTheAgentHolds(t *testing.T) {
	repo := initRepo(t)
	env := worktreeEnv{
		agentID:      "agent-moved-dir",
		repo:         repo,
		agentCwd:     repo,
		worktreesDir: t.TempDir(),
	}

	first, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", first.Stderr)
	tree := strings.TrimSpace(string(first.Stdout))

	moved := t.TempDir()
	env.trigger = "resume"
	env.agentCwd = tree
	env.worktreesDir = moved

	second, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", second.Stderr)

	assert.Equal(t, tree, strings.TrimSpace(string(second.Stdout)), "the resume goes back to the tree the agent already has")
	assert.Contains(t, string(second.Stderr), "already exists", "the resume finds the worktree rather than trying to make it again")
	assert.NoDirExists(t, filepath.Join(moved, "agent-moved-dir"), "nothing is made under the directory worktrees now live in")
}

// TestIntegrationBuiltinWorktreeSetupOnResumeRebuildsUnderTheConfiguredDirectory covers the tree an agent
// holds being gone as well as the worktrees directory having moved, where the registration the
// old tree left behind still claims the branch until it is pruned.
func TestIntegrationBuiltinWorktreeSetupOnResumeRebuildsUnderTheConfiguredDirectory(t *testing.T) {
	repo := initRepo(t)
	env := worktreeEnv{
		agentID:      "agent-moved-gone",
		repo:         repo,
		agentCwd:     repo,
		worktreesDir: t.TempDir(),
	}

	first, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", first.Stderr)
	tree := strings.TrimSpace(string(first.Stdout))
	require.NoError(t, os.RemoveAll(tree))

	moved := t.TempDir()
	env.trigger = "resume"
	env.agentCwd = tree
	env.worktreesDir = moved

	second, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", second.Stderr)

	rebuilt := strings.TrimSpace(string(second.Stdout))
	assert.Equal(t, filepath.Join(moved, "agent-moved-gone"), rebuilt)
	assertIsDir(t, rebuilt)
	assertBranch(t, rebuilt, "agent/agent-moved-gone")
}

// TestIntegrationBuiltinWorktreeSetupOnProvisionUsesTheConfiguredDirectory covers a create never going back
// to a cwd.
func TestIntegrationBuiltinWorktreeSetupOnProvisionUsesTheConfiguredDirectory(t *testing.T) {
	repo := initRepo(t)
	env := worktreeEnv{
		agentID:      "agent-provision-dir",
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
	assert.Contains(t, string(second.Stderr), filepath.Join(moved, "agent-provision-dir"), "the create aims at the directory worktrees live in now")
	assert.NotContains(t, string(second.Stderr), "already exists", "the tree the cwd names is not taken for a create")
}

// TestIntegrationBuiltinWorktreeSetupOnResumeRefusesACwdThatIsTheRepo covers a create that failed before
// setup replaced the directory, which leaves the agent's cwd naming the workflow's own
// checkout: resuming into that would run the agent inside the repository.
func TestIntegrationBuiltinWorktreeSetupOnResumeRefusesACwdThatIsTheRepo(t *testing.T) {
	repo := initRepo(t)
	agentID := filepath.Base(repo)
	worktreesDir := t.TempDir()

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      agentID,
		repo:         repo,
		agentCwd:     repo,
		trigger:      "resume",
		worktreesDir: worktreesDir,
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	tree := strings.TrimSpace(string(result.Stdout))
	assert.Equal(t, filepath.Join(worktreesDir, agentID), tree, "the repository is not taken for the agent's worktree")
	assertIsDir(t, tree)
	assertBranch(t, tree, "agent/"+agentID)
}

// TestIntegrationBuiltinWorktreeSetupCopiesADirectory covers a copy path naming a directory rather
// than a file: cp without -r would fail and silently drop the rest of the list.
func TestIntegrationBuiltinWorktreeSetupCopiesADirectory(t *testing.T) {
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

// TestIntegrationBuiltinWorktreeSetupSkipsATrackedCopyPath pins that a path git already tracks is left
// to the checkout rather than copied over with the version in the working tree.
func TestIntegrationBuiltinWorktreeSetupSkipsATrackedCopyPath(t *testing.T) {
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

// TestIntegrationBuiltinWorktreeSetupSkipsATrackedSymlinkPath pins that a tracked directory is left as
// git checked it out rather than replaced with a link.
func TestIntegrationBuiltinWorktreeSetupSkipsATrackedSymlinkPath(t *testing.T) {
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

// TestIntegrationBuiltinWorktreeSetupRefusesAPathUnderALinkAnEarlierEntryMade pins that a second entry
// reaching through a link the first one made is refused rather than writing into the repo.
func TestIntegrationBuiltinWorktreeSetupRefusesAPathUnderALinkAnEarlierEntryMade(t *testing.T) {
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

// TestIntegrationBuiltinWorktreeSetupRefusesAPathUnderATrackedSymlink pins that a path resolving
// through a tracked symlink out of the repo is refused.
func TestIntegrationBuiltinWorktreeSetupRefusesAPathUnderATrackedSymlink(t *testing.T) {
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

// TestIntegrationBuiltinWorktreeSetupAppliesAPathWrittenWithATrailingSlash pins that a trailing slash
// names the same path, so the entry is not applied twice.
func TestIntegrationBuiltinWorktreeSetupAppliesAPathWrittenWithATrailingSlash(t *testing.T) {
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
		agentID:      "agent-tracked-symlink-slash",
		repo:         repo,
		worktreesDir: t.TempDir(),
		copyPaths:    []string{"shared/"},
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	assert.FileExists(t, filepath.Join(outside, "cache", "keep.txt"), "the directory outside the repo survives")
}

// TestIntegrationBuiltinWorktreeCleanupRefusesAPlainCheckout covers KOMRADE_AGENT_CWD pointing at a
// checkout that is not a linked worktree at all, such as the repo komrade itself lives in:
// cleanup must not run worktree remove or branch -D against it.
func TestIntegrationBuiltinWorktreeCleanupRefusesAPlainCheckout(t *testing.T) {
	repo := initRepo(t)
	runGit(t, repo, "branch", "agent/agent-i")

	result, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{agentID: "agent-i", agentCwd: repo})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	assertIsDir(t, repo)
	branches, err := exec.Command("git", "-C", repo, "branch", "--list", "agent/agent-i").Output()
	require.NoError(t, err)
	assert.NotEmpty(t, strings.TrimSpace(string(branches)), "the branch survives since this was never a linked worktree")
}

// TestIntegrationBuiltinWorktreeSetupCopyMissingSourceIsNonFatal covers a copy or symlink entry naming a
// file the repo does not have, where setup warns on stderr and keeps going.
func TestIntegrationBuiltinWorktreeSetupCopyMissingSourceIsNonFatal(t *testing.T) {
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

// TestIntegrationBuiltinWorktreeSetupRefusesABlankBase covers a workflow naming no base, which setup
// refuses rather than guessing a branch of its own.
func TestIntegrationBuiltinWorktreeSetupRefusesABlankBase(t *testing.T) {
	worktreesDir := t.TempDir()
	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      "agent-d",
		repo:         initRepo(t),
		worktreesDir: worktreesDir,
		noBase:       true,
	})
	require.Error(t, err, "the provisioner fails rather than picking a branch of its own")

	assert.Contains(t, string(result.Stderr), "names no base branch")
	assert.NoDirExists(t, filepath.Join(worktreesDir, "agent-d"), "no worktree was made")
}

// TestIntegrationBuiltinWorktreeSetupFetchesFromOrigin covers the repo that has a remote: setup has to
// fetch the base branch and build the worktree off origin's copy of it rather than
// whatever the local repo happens to already have.
func TestIntegrationBuiltinWorktreeSetupFetchesFromOrigin(t *testing.T) {
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

// TestIntegrationBuiltinWorktreeSetupBranchesFromABaseTheRefspecDoesNotCover covers a single-branch clone,
// where fetching the default branch exits zero and still writes no ref to branch from.
func TestIntegrationBuiltinWorktreeSetupBranchesFromABaseTheRefspecDoesNotCover(t *testing.T) {
	upstream := initBareRepo(t)
	pushBranch(t, upstream, "feature", "feature-only.txt")
	repo := singleBranchClone(t, upstream, "feature")

	require.False(t, hasRef(t, repo, "refs/remotes/origin/main"), "the clone's refspec covers only the branch it asked for")

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      "agent-single-branch",
		repo:         repo,
		worktreesDir: t.TempDir(),
	})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	tree := strings.TrimSpace(string(result.Stdout))
	assert.Contains(t, string(result.Stderr), "fetching main from origin")
	assert.FileExists(t, filepath.Join(tree, "README.md"))
	assert.NoFileExists(t, filepath.Join(tree, "feature-only.txt"), "the worktree is cut from origin's default branch rather than the one the clone happens to hold")
	assert.False(t, hasRef(t, repo, "refs/remotes/origin/main"), "the human's remote refs are left as their refspec asked for")
	assert.False(t, hasRef(t, repo, "refs/komrade/base/agent-single-branch"), "the ref that named the branch point for one command is not left behind")
}

// TestIntegrationBuiltinWorktreeSetupBranchesFromABaseWithNoRefspecAtAll covers the repo wired to a
// remote by hand rather than cloned, which has a URL to fetch from and no refspec to map
// anything it fetches, so no fetch by branch name ever writes a tracking ref.
func TestIntegrationBuiltinWorktreeSetupBranchesFromABaseWithNoRefspecAtAll(t *testing.T) {
	upstream := initBareRepo(t)

	repo := initRepo(t)
	runGit(t, repo, "config", "remote.origin.url", upstream)

	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      "agent-no-refspec",
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
			agentID:      "agent-on-main",
			repo:         repo,
			worktreesDir: worktreesDir,
			baseBranch:   "main",
		},
		{
			agentID:      "agent-on-other",
			repo:         repo,
			worktreesDir: worktreesDir,
			baseBranch:   "other",
		},
	})

	wants := []struct {
		agentID string
		file    string
		absent  string
	}{
		{
			agentID: "agent-on-main",
			file:    "main-only.txt",
			absent:  "other-only.txt",
		},
		{
			agentID: "agent-on-other",
			file:    "other-only.txt",
			absent:  "main-only.txt",
		},
	}

	for i, want := range wants {
		require.NoErrorf(t, outcomes[i].err, "%s: stderr: %s", want.agentID, outcomes[i].result.Stderr)

		tree := filepath.Join(worktreesDir, want.agentID)
		assert.FileExists(t, filepath.Join(tree, want.file), "%s is on the base its own workflow named", want.agentID)
		assert.NoFileExists(t, filepath.Join(tree, want.absent), "%s did not read the other setup's branch point", want.agentID)
	}
}

// TestIntegrationBuiltinWorktreeSetupRefusesABaseTheRepoCannotReach covers a workflow naming a base nothing
// has, where letting git fail with invalid reference reads as a broken repo instead.
func TestIntegrationBuiltinWorktreeSetupRefusesABaseTheRepoCannotReach(t *testing.T) {
	upstream := initBareRepo(t)
	repo := cloneRepo(t, upstream)

	worktreesDir := t.TempDir()
	result, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      "agent-bad-base",
		repo:         repo,
		worktreesDir: worktreesDir,
		baseBranch:   "no-such-branch",
	})
	require.Error(t, err, "the provisioner fails rather than handing the add a reference nothing resolved")

	assert.Contains(t, string(result.Stderr), "no-such-branch is not a branch")
	assert.NoDirExists(t, filepath.Join(worktreesDir, "agent-bad-base"), "no worktree was made")
}

// TestIntegrationBuiltinWorktreeCleanup covers the ordinary case: the worktree and its branch both
// exist, and cleanup removes both.
func TestIntegrationBuiltinWorktreeCleanup(t *testing.T) {
	repo := initRepo(t)
	setup, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      "agent-f",
		repo:         repo,
		worktreesDir: t.TempDir(),
	})
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

// TestIntegrationBuiltinWorktreeCleanupIsBestEffortWhenAlreadyGone covers what docs/scripts.md requires
// of every deprovisioner: a workflow's cleanup must not block a delete whatever state its
// target is in, so running cleanup against a directory that was never a worktree, or was
// already removed, must still exit zero.
func TestIntegrationBuiltinWorktreeCleanupIsBestEffortWhenAlreadyGone(t *testing.T) {
	result, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{agentID: "agent-never-existed", agentCwd: t.TempDir()})
	require.NoError(t, err, "stderr: %s", result.Stderr)
}

// TestIntegrationBuiltinWorktreeSetupDoesNotTrackTheBase covers the new branch being left with no upstream,
// which tracking the base would have git status report it against and a plain push refuse.
func TestIntegrationBuiltinWorktreeSetupDoesNotTrackTheBase(t *testing.T) {
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

// TestIntegrationBuiltinWorktreeSetupResumesAfterTheDirectoryIsDeletedOutright covers an rm -rf of the
// worktree, whose stale registration still claims the branch until it is pruned.
func TestIntegrationBuiltinWorktreeSetupResumesAfterTheDirectoryIsDeletedOutright(t *testing.T) {
	repo := initRepo(t)
	env := worktreeEnv{
		agentID:      "agent-rmrf",
		repo:         repo,
		worktreesDir: t.TempDir(),
	}

	first, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", first.Stderr)
	tree := strings.TrimSpace(string(first.Stdout))

	require.NoError(t, os.RemoveAll(tree))

	env.trigger = "resume"
	env.agentCwd = tree
	second, err := runBuiltin(t, "_worktree_setup", env)
	require.NoError(t, err, "stderr: %s", second.Stderr)
	assertIsDir(t, strings.TrimSpace(string(second.Stdout)))
	assertBranch(t, tree, "agent/agent-rmrf")
}

// TestIntegrationBuiltinWorktreeCleanupRefusesAWorktreeItDoesNotOwn covers KOMRADE_AGENT_CWD pointing at a
// linked worktree komrade did not make, which passes the same check a hand-made one does.
func TestIntegrationBuiltinWorktreeCleanupRefusesAWorktreeItDoesNotOwn(t *testing.T) {
	repo := initRepo(t)
	theirs := filepath.Join(t.TempDir(), "theirs")
	runGit(t, repo, "worktree", "add", "--quiet", "-b", "someone-else", theirs)

	result, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{agentID: "agent-thief", agentCwd: theirs})
	require.NoError(t, err, "stderr: %s", result.Stderr)

	assertIsDir(t, theirs)
	assert.NotEmpty(t, readBranchList(t, repo, "someone-else"), "someone else's branch survives")
}

// TestIntegrationBuiltinWorktreeCleanupKeepsAnUnmergedBranch covers an agent that committed work nobody
// merged, where the branch is the one recoverable trace of it.
func TestIntegrationBuiltinWorktreeCleanupKeepsAnUnmergedBranch(t *testing.T) {
	repo := initRepo(t)
	setup, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      "agent-unmerged",
		repo:         repo,
		worktreesDir: t.TempDir(),
	})
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

// TestIntegrationBuiltinWorktreeCleanupReportsUncommittedWorkItDestroys covers a dirty worktree, where
// nobody is left to ask so what was thrown away goes to stderr instead.
func TestIntegrationBuiltinWorktreeCleanupReportsUncommittedWorkItDestroys(t *testing.T) {
	repo := initRepo(t)
	setup, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      "agent-dirty",
		repo:         repo,
		worktreesDir: t.TempDir(),
	})
	require.NoError(t, err, "stderr: %s", setup.Stderr)
	tree := strings.TrimSpace(string(setup.Stdout))

	require.NoError(t, os.WriteFile(filepath.Join(tree, "scratch.txt"), []byte("unsaved\n"), 0o644))

	cleanup, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{agentID: "agent-dirty", agentCwd: tree})
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
		agentID:      "agent-nest",
		repo:         repo,
		worktreesDir: t.TempDir(),
	})
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

// TestIntegrationBuiltinWorktreeCleanupHandlesADetachedHead covers a worktree left off its branch by a
// checkout, an interrupted rebase, or a bisect, where the path komrade chose proves ownership.
func TestIntegrationBuiltinWorktreeCleanupHandlesADetachedHead(t *testing.T) {
	repo := initRepo(t)
	setup, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      "agent-detached",
		repo:         repo,
		worktreesDir: t.TempDir(),
	})
	require.NoError(t, err, "stderr: %s", setup.Stderr)
	tree := strings.TrimSpace(string(setup.Stdout))

	runGit(t, tree, "checkout", "--quiet", "--detach", "HEAD")

	cleanup, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{agentID: "agent-detached", agentCwd: tree})
	require.NoError(t, err, "stderr: %s", cleanup.Stderr)

	assert.NoDirExists(t, tree, "a detached head is still this agent's worktree")
	assert.Empty(t, readBranchList(t, repo, "agent/agent-detached"), "the branch goes with it")
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

// TestIntegrationBuiltinWorktreeCleanupRefusesWhenTheWorktreeListCannotBeRead covers the nested-worktree
// guard failing, where an unreadable list is not proof that nothing is nested.
func TestIntegrationBuiltinWorktreeCleanupRefusesWhenTheWorktreeListCannotBeRead(t *testing.T) {
	repo := initRepo(t)
	setup, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      "agent-blind",
		repo:         repo,
		worktreesDir: t.TempDir(),
	})
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

// TestIntegrationBuiltinWorktreeCleanupRefusesATreeWhoseRepoHasMoved covers a worktree whose .git file
// points into a repo that is no longer at that path: git cannot answer at all, so cleanup has
// to fail rather than record a clean deprovision for a directory that is still on disk.
func TestIntegrationBuiltinWorktreeCleanupRefusesATreeWhoseRepoHasMoved(t *testing.T) {
	repo := initRepo(t)
	setup, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      "agent-adrift",
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
	require.NoError(t, os.Rename(repo, moved))

	cleanup, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{agentID: "agent-adrift", agentCwd: tree})
	require.Error(t, err, "the refusal is reported rather than exiting zero")

	assertIsDir(t, tree)
	assert.Contains(t, string(cleanup.Stderr), "git cannot read "+resolvedTree)
	assert.Contains(t, string(cleanup.Stderr), filepath.Join(resolvedRepo, ".git", "worktrees", "agent-adrift"), "the message names the gitdir the .git file points at")
	assert.NotEmpty(t, readBranchList(t, moved, "agent/agent-adrift"), "nothing was removed")
}

// TestIntegrationBuiltinWorktreeCleanupRefusesATreeWhoseRegistrationIsGone covers the retry of a delete
// that failed partway: worktree remove already dropped the registration before it hit the
// error, so the second run finds a directory git no longer knows anything about.
func TestIntegrationBuiltinWorktreeCleanupRefusesATreeWhoseRegistrationIsGone(t *testing.T) {
	repo := initRepo(t)
	setup, err := runBuiltin(t, "_worktree_setup", worktreeEnv{
		agentID:      "agent-unregistered",
		repo:         repo,
		worktreesDir: t.TempDir(),
	})
	require.NoError(t, err, "stderr: %s", setup.Stderr)
	tree := strings.TrimSpace(string(setup.Stdout))

	require.NoError(t, os.RemoveAll(filepath.Join(repo, ".git", "worktrees", "agent-unregistered")))

	cleanup, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{agentID: "agent-unregistered", agentCwd: tree})
	require.Error(t, err, "the refusal is reported rather than exiting zero")

	assertIsDir(t, tree)
	assert.Contains(t, string(cleanup.Stderr), "the registration is gone")
	assert.NotEmpty(t, readBranchList(t, repo, "agent/agent-unregistered"), "the branch is left for a person to deal with")
}

// TestIntegrationBuiltinWorktreeCleanupLeavesAWorktreeItDoesNotOwnAloneWhenGitCannotAnswer covers the two
// refusals meeting: the ownership claim is a fact about the path and the agent id alone, so a
// tree komrade never made still exits zero when there is no repo left to ask.
func TestIntegrationBuiltinWorktreeCleanupLeavesAWorktreeItDoesNotOwnAloneWhenGitCannotAnswer(t *testing.T) {
	repo := initRepo(t)
	theirs := filepath.Join(t.TempDir(), "theirs")
	runGit(t, repo, "worktree", "add", "--quiet", "-b", "someone-else", theirs)

	moved := filepath.Join(t.TempDir(), "moved")
	require.NoError(t, os.Rename(repo, moved))

	cleanup, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{agentID: "agent-thief", agentCwd: theirs})
	require.NoError(t, err, "stderr: %s", cleanup.Stderr)

	assertIsDir(t, theirs)
	assert.Contains(t, string(cleanup.Stderr), "is not named after agent-thief")
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
	agentID      string
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
	pathPrefix   string
}

func (e worktreeEnv) toEnv() []string {
	trigger := e.trigger
	if trigger == "" {
		trigger = "provision"
	}

	baseBranch := e.baseBranch
	if baseBranch == "" && !e.noBase {
		baseBranch = "main"
	}

	env := []string{
		"KOMRADE_AGENT_ID=" + e.agentID,
		"KOMRADE_AGENT_CWD=" + e.agentCwd,
		"KOMRADE_TRIGGER=" + trigger,
		"KOMRADE_WORKTREE_REPO=" + e.repo,
		"KOMRADE_WORKTREES_DIR=" + e.worktreesDir,
		"KOMRADE_WORKTREE_BASE_BRANCH=" + baseBranch,
		"KOMRADE_WORKTREE_ADOPT_BRANCH=" + e.adoptBranch,
		"KOMRADE_WORKTREE_COPY_PATHS=" + strings.Join(e.copyPaths, "\n"),
		"KOMRADE_WORKTREE_SYMLINK_PATHS=" + strings.Join(e.symlinkPaths, "\n"),
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

	run := runScript(scriptRun{
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

func runScript(run scriptRun) outcome {
	result, err := run.runner.RunAndWait(context.Background(), RunParams{
		Script: *run.script,
		Dir:    run.script.GetDir(),
		Env:    append(run.env.toEnv(), "KOMRADE_SETUP_DIR_FILE="+run.dirPath),
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

			outcomes[i] = runScript(scriptRun{
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
	runGit(t, dir, "config", "user.email", "test@komrade.test")
	runGit(t, dir, "config", "user.name", "komrade test")
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
	runGit(t, dir, "config", "user.email", "test@komrade.test")
	runGit(t, dir, "config", "user.name", "komrade test")

	return dir
}

func singleBranchClone(t *testing.T, upstream, branch string) string {
	t.Helper()

	dir := t.TempDir()
	runGitClone(t, "--quiet", "--single-branch", "--branch", branch, upstream, dir)
	runGit(t, dir, "config", "user.email", "test@komrade.test")
	runGit(t, dir, "config", "user.name", "komrade test")

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
		agentID:      "agent-adopt-local",
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
		agentID:      "agent-adopt-remote",
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
		agentID:      "agent-adopt-missing",
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
		agentID:      "agent-adopt-cleanup",
		repo:         repo,
		worktreesDir: t.TempDir(),
		adoptBranch:  "someones/work",
	})
	require.NoError(t, err, "stderr: %s", setup.Stderr)
	tree := strings.TrimSpace(string(setup.Stdout))

	cleanup, err := runBuiltin(t, "_worktree_cleanup", worktreeEnv{
		agentID:     "agent-adopt-cleanup",
		agentCwd:    tree,
		adoptBranch: "someones/work",
	})
	require.NoError(t, err, "stderr: %s", cleanup.Stderr)

	_, statErr := os.Stat(tree)
	assert.True(t, os.IsNotExist(statErr), "the worktree directory is gone")
	assert.True(t, hasRef(t, repo, "refs/heads/someones/work"), "the branch outlives the agent that adopted it")
}
