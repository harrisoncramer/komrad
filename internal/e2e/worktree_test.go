package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWorktreeAgentsAreCreatedConcurrently covers a worktree workflow being fanned out over
// the API, which is what an orchestrating agent spawning siblings does and what two schedules
// matching the same minute do. The generated setup action runs for real here, so anything it
// writes to the repo's shared git config puts every concurrent create on the same lock and
// costs all but one of them their agent.
func TestWorktreeAgentsAreCreatedConcurrently(t *testing.T) {
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

// TestWorktreeAgentCanPublishItsBranch covers the state the agent's own git is left in. An
// agent that has finished its work reaches for `git push`, and a branch tracking the base it
// was cut from refuses that outright, since the upstream's name is not the branch's.
func TestWorktreeAgentCanPublishItsBranch(t *testing.T) {
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

	// The assertion the rest of this is for: a plain push, the command an agent that has
	// finished reaches for, with no -u and no upstream to inherit.
	runGitIn(t, tree, "push")

	assert.Equal(t, readGitOutput(t, tree, "rev-parse", "HEAD"), readGitOutput(t, tree, "rev-parse", "refs/remotes/origin/"+branch),
		"the push created the branch's own remote")
}

// hermeticGit points git at empty global and system config for the rest of the test, so what
// the setup action does is decided by the repo in front of it rather than by whatever the
// developer running the suite happens to have set. It is process-wide, so it is called by the
// test itself rather than buried in a helper.
func hermeticGit(t *testing.T) {
	t.Helper()

	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
}

// initGitRepo builds a checkout with an origin behind it, since a repo with no remote never
// reaches the half of the setup action that builds a worktree off origin's copy of the base.
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
	// The excludes file git reads when core.excludesFile is unset is a path rather than a
	// config value, so the hermetic environment does not reach it and the developer's own
	// ignore list would decide which copy paths a worktree gets.
	runGitIn(t, clone, "config", "core.excludesFile", os.DevNull)

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

// readGitConfig reads one config key, blank when the key is unset. Exit 1 is the only status
// that means unset, so any other failure ends the test rather than reading as an absent value
// the caller would then assert on.
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
