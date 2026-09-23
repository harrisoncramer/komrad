//go:build integration

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"weave/config"
	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationWorktreeAgentsAreCreatedConcurrently covers a worktree workflow fanned out over the
// API, where anything the provisioner writes to shared git config puts every create on one
// lock.
func TestIntegrationWorktreeAgentsAreCreatedConcurrently(t *testing.T) {
	hermeticGit(t)

	worktreesDir := t.TempDir()
	instance := weavetest.Start(t, weavetest.Params{WorktreesDir: worktreesDir})
	ctx := t.Context()

	repo := initGitRepo(t)

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name:     "backend",
		Cwd:      repo,
		Worktree: &workflowv1.WorktreeConfig{Enabled: true},
	}))
	require.NoError(t, err)

	workflowID := workflow.Msg.GetWorkflow().GetId()

	const concurrent = 4

	type created struct {
		cwd string
		err error
	}

	results := make([]created, concurrent)

	var wg sync.WaitGroup
	for i := range concurrent {
		wg.Add(1)

		go func() {
			defer wg.Done()

			agent, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
				Prompt:     "do the work",
				Title:      fmt.Sprintf("sibling-%d", i),
				WorkflowId: workflowID,
			}))
			if err != nil {
				results[i] = created{err: err}

				return
			}

			results[i] = created{cwd: agent.Msg.GetAgent().GetCwd()}
		}()
	}
	wg.Wait()

	for i, result := range results {
		require.NoErrorf(t, result.err, "agent %d", i)
		assert.Equal(t, worktreesDir, filepath.Dir(result.cwd), "the agent runs in its own generated worktree")
		assert.DirExists(t, result.cwd)
	}
}

// TestIntegrationWorktreeAgentsStayOutOfTheRealStateDirectory covers an instance given no worktrees
// directory making its own rather than reaching for the running panel's.
func TestIntegrationWorktreeAgentsStayOutOfTheRealStateDirectory(t *testing.T) {
	hermeticGit(t)

	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	shipped, err := config.Config{}.GetWorktreesDir()
	require.NoError(t, err)
	require.NotEqual(t, shipped, instance.WorktreesDir, "an instance names a worktrees directory of its own")

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(worktreeWorkflow(t)))
	require.NoError(t, err)

	agent, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "do the work",
		Title:      "login",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)

	assert.Equal(t, instance.WorktreesDir, filepath.Dir(agent.Msg.GetAgent().GetCwd()))
}

// TestIntegrationWorktreeAgentCanPublishItsBranch covers the agent reaching for git push, which a branch
// tracking the base it was cut from refuses outright.
func TestIntegrationWorktreeAgentCanPublishItsBranch(t *testing.T) {
	hermeticGit(t)

	worktreesDir := t.TempDir()
	instance := weavetest.Start(t, weavetest.Params{WorktreesDir: worktreesDir})
	ctx := t.Context()

	repo := initGitRepo(t)

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name:     "backend",
		Cwd:      repo,
		Worktree: &workflowv1.WorktreeConfig{Enabled: true},
	}))
	require.NoError(t, err)

	agent, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "do the work",
		Title:      "publisher",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)

	tree := agent.Msg.GetAgent().GetCwd()
	require.DirExists(t, tree)

	branch := readGitOutput(t, tree, "rev-parse", "--abbrev-ref", "HEAD")
	assert.Empty(t, readGitConfig(t, tree, "branch."+branch+".merge"), "the agent's branch has no upstream to be measured against")

	require.NoError(t, os.WriteFile(filepath.Join(tree, "work.txt"), []byte("done\n"), 0o644))
	runGitIn(t, tree, "add", "work.txt")
	runGitIn(t, tree, "commit", "--quiet", "-m", "the agent's work")

	runGitIn(t, tree, "push")

	assert.Equal(t, readGitOutput(t, tree, "rev-parse", "HEAD"), readGitOutput(t, tree, "rev-parse", "refs/remotes/origin/"+branch),
		"the push created the branch's own remote")
}

func hermeticGit(t *testing.T) {
	t.Helper()

	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
}

func initGitRepo(t *testing.T) string {
	t.Helper()

	seed := t.TempDir()
	runGitIn(t, seed, "init", "--quiet", "-b", "main")
	runGitIn(t, seed, "config", "user.email", "test@weave.test")
	runGitIn(t, seed, "config", "user.name", "weave test")
	require.NoError(t, os.WriteFile(filepath.Join(seed, "README.md"), []byte("hello\n"), 0o644))
	runGitIn(t, seed, "add", "README.md")
	runGitIn(t, seed, "commit", "--quiet", "-m", "initial")

	upstream := t.TempDir()
	runGitIn(t, seed, "clone", "--quiet", "--bare", seed, upstream)

	clone := t.TempDir()
	output, err := exec.Command("git", "clone", "--quiet", upstream, clone).CombinedOutput()
	require.NoErrorf(t, err, "git clone: %s", output)
	runGitIn(t, clone, "config", "user.email", "test@weave.test")
	runGitIn(t, clone, "config", "user.name", "weave test")

	return clone
}

func runGitIn(t *testing.T, dir string, args ...string) {
	t.Helper()

	output, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	require.NoErrorf(t, err, "git %v: %s", args, output)
}

func readGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()

	output, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	require.NoErrorf(t, err, "git %v", args)

	return strings.TrimSpace(string(output))
}

func readGitConfig(t *testing.T, dir, key string) string {
	t.Helper()

	output, err := exec.Command("git", "-C", dir, "config", "--get", key).Output()
	if err != nil {
		var exit *exec.ExitError
		require.ErrorAs(t, err, &exit)
		require.Equal(t, 1, exit.ExitCode(), "git config failed for a reason other than the key being unset: %s", exit.Stderr)

		return ""
	}

	return strings.TrimSpace(string(output))
}

// replaceWorktreeTeardown writes over the built-in that gives an agent's worktree back, which
// is the only deprovision there is now that a workflow cannot name a script of its own.
func replaceWorktreeTeardown(t *testing.T, instance *weavetest.Instance, script string) {
	t.Helper()

	require.NoError(t, os.WriteFile(filepath.Join(instance.BuiltinDir, "_worktree_cleanup"), []byte(script), 0o755))
}

// worktreeWorkflow is the request for a workflow whose agents each get a worktree of their own.
func worktreeWorkflow(t *testing.T) *workflowv1.CreateWorkflowRequest {
	t.Helper()

	return &workflowv1.CreateWorkflowRequest{
		Name:     "backend",
		Cwd:      initGitRepo(t),
		Worktree: &workflowv1.WorktreeConfig{Enabled: true},
	}
}
