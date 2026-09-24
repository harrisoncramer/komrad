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
	"time"

	"weave/config"
	"weave/internal/harness"
	worktreestore "weave/internal/store/worktree"
	"weave/internal/weavetest"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"
	worktreev1 "weave/proto/gen/worktree/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestIntegrationWorktrees covers the worktrees a workflow cuts, from create to delete.
func TestIntegrationWorktrees(t *testing.T) {
	isolateGit(t)

	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()
	repo := initGitRepo(t)

	shipped, err := config.Config{}.GetWorktreesDir()
	require.NoError(t, err)
	require.NotEqual(t, shipped, instance.WorktreesDir, "an instance names a worktrees directory of its own")

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name:     "backend",
		Cwd:      repo,
		Worktree: &workflowv1.WorktreeConfig{Enabled: true, BaseBranch: "main"},
	}))
	require.NoError(t, err)

	const siblings = 4
	agents := make([]*agentv1.Agent, siblings)

	require.True(t, t.Run("agents created at once each get a worktree of their own", func(t *testing.T) {
		errs := make([]error, siblings)

		var wg sync.WaitGroup
		for i := range siblings {
			wg.Go(func() {
				created, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
					Prompt:     "do the work",
					Title:      fmt.Sprintf("sibling-%d", i),
					WorkflowId: workflow.Msg.GetWorkflow().GetId(),
				}))
				if err != nil {
					errs[i] = err

					return
				}

				agents[i] = created.Msg.GetAgent()
			})
		}
		wg.Wait()

		for i := range siblings {
			require.NoErrorf(t, errs[i], "agent %d", i)
			assert.Equal(t, instance.WorktreesDir, filepath.Dir(agents[i].GetCwd()))
			assert.DirExists(t, agents[i].GetCwd())
		}
	}))

	publisher, lister, leaver, synced := agents[0], agents[1], agents[2], agents[3]

	t.Run("an agent can publish its branch", func(t *testing.T) {
		tree := publisher.GetCwd()
		branch := readGitOutput(t, tree, "rev-parse", "--abbrev-ref", "HEAD")
		assert.Empty(t, readGitConfig(t, tree, "branch."+branch+".merge"), "the agent's branch has no upstream to be measured against")

		commitIn(t, tree, "work.txt", "the published work")
		runGitIn(t, tree, "push")

		assert.Equal(t, readGitOutput(t, tree, "rev-parse", "HEAD"), readGitOutput(t, tree, "rev-parse", "refs/remotes/origin/"+branch))
	})

	orphan := filepath.Join(instance.WorktreesDir, "01a0ffff-0000-7000-8000-000000000000")
	require.NoError(t, os.MkdirAll(orphan, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(orphan, "blob"), make([]byte, 1<<20), 0o644))

	t.Run("the listing names each worktree under the agent it was cut for", func(t *testing.T) {
		found := listWorktrees(t, instance)

		require.Contains(t, found, lister.GetCwd())
		tree := found[lister.GetCwd()]
		assert.True(t, tree.GetReadable())
		assert.Equal(t, "agent/"+lister.GetId(), tree.GetBranch())
		assert.Equal(t, lister.GetId(), tree.GetAgent().GetId())
		assert.Equal(t, "sibling-1", tree.GetAgent().GetName())
		assert.NotEmpty(t, tree.GetLastActiveAt())
		assert.Contains(t, found, orphan)
		for dir, worktree := range found {
			assert.Nil(t, worktree.SizeBytes, "%s carries no size before the loop has been round", dir)
		}
	})

	t.Run("the size loop measures every directory", func(t *testing.T) {
		instance.MeasureWorktrees()

		found := listWorktrees(t, instance)
		assert.Positive(t, found[lister.GetCwd()].GetSizeBytes())
		assert.NotEmpty(t, found[lister.GetCwd()].GetMeasuredAt())
		assert.GreaterOrEqual(t, found[orphan].GetSizeBytes(), int64(1<<20))
	})

	t.Run("reading a worktree measures it against its base and writes its size", func(t *testing.T) {
		tree := lister.GetCwd()
		before := listWorktrees(t, instance)[tree].GetSizeBytes()
		commitIn(t, tree, "work.txt", "the agent's work")
		require.NoError(t, os.WriteFile(filepath.Join(tree, "blob"), make([]byte, 2<<20), 0o644))

		read, err := instance.Worktrees.GetWorktree(ctx, connect.NewRequest(&worktreev1.GetWorktreeRequest{Dir: tree}))
		require.NoError(t, err)

		assert.Equal(t, "origin/main", read.Msg.GetBaseRef())
		assert.Equal(t, int32(1), read.Msg.GetAhead())
		assert.Equal(t, int32(0), read.Msg.GetBehind())
		assert.True(t, read.Msg.GetKeepsBranch(), "the agent's commit is on no other ref")
		require.Len(t, read.Msg.GetCommits(), 1)
		assert.Equal(t, "the agent's work", read.Msg.GetCommits()[0].GetSubject())
		assert.NotEmpty(t, read.Msg.GetEntries())
		assert.Greater(t, read.Msg.GetSizeBytes(), before)
		assert.Equal(t, read.Msg.GetSizeBytes(), listWorktrees(t, instance)[tree].GetSizeBytes())
	})

	t.Run("the remover refuses a worktree an agent answers for and takes an orphan", func(t *testing.T) {
		removal, err := instance.Worktrees.RemoveUnownedWorktrees(ctx, connect.NewRequest(&worktreev1.RemoveUnownedWorktreesRequest{
			Dirs: []string{lister.GetCwd(), orphan},
		}))
		require.NoError(t, err)

		assert.Equal(t, []string{orphan}, removal.Msg.GetRemoved())
		require.Len(t, removal.Msg.GetRefused(), 1)
		assert.Contains(t, removal.Msg.GetRefused()[0].GetReason(), "sibling-1")
		assert.DirExists(t, lister.GetCwd())
		assert.NoDirExists(t, orphan)

		instance.MeasureWorktrees()

		stored, err := worktreestore.NewWorktreeStore(instance.Stores.Pool.GetDB()).ListSizes(ctx)
		require.NoError(t, err)
		assert.NotContains(t, xslice.Map(stored, func(size worktreestore.Size) string {
			return size.Dir
		}), orphan,
			"the row of a directory that has gone is forgotten")
	})

	t.Run("deleting an agent takes its worktree out of the listing", func(t *testing.T) {
		_, err := instance.Agents.DeleteAgents(ctx, connect.NewRequest(&agentv1.DeleteAgentsRequest{Ids: []string{leaver.GetId()}}))
		require.NoError(t, err)

		assert.NotContains(t, listWorktrees(t, instance), leaver.GetCwd(), "the directory leaves the listing as soon as the delete claims it")
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.NoDirExists(c, leaver.GetCwd())
		}, 10*time.Second, 100*time.Millisecond)
	})

	t.Run("the cwd loop records the repository a checkout pushes to", func(t *testing.T) {
		runGitIn(t, repo, "remote", "set-url", "origin", "git@github.com:harrisoncramer/weave.git")
		instance.SyncCwds()

		fetched, err := instance.Agents.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: synced.GetId()}))
		require.NoError(t, err)
		assert.NotEmpty(t, fetched.Msg.GetAgent().GetAgent().GetBranch())
		assert.Equal(t, "https://github.com/harrisoncramer/weave", fetched.Msg.GetAgent().GetAgent().GetRepositoryUrl())
	})

	t.Run("a worktree removed by hand reaches the caller", func(t *testing.T) {
		require.NoError(t, os.RemoveAll(publisher.GetCwd()))
		instance.SyncCwds()

		fetched, err := instance.Agents.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: publisher.GetId()}))
		require.NoError(t, err)
		assert.True(t, fetched.Msg.GetAgent().GetAgent().GetWorktreeRemoved())

		listed, err := instance.Agents.ListAgents(ctx, connect.NewRequest(&agentv1.ListAgentsRequest{}))
		require.NoError(t, err)
		removed := map[string]bool{}
		for _, row := range listed.Msg.GetAgents() {
			removed[row.GetAgent().GetId()] = row.GetAgent().GetWorktreeRemoved()
		}
		assert.Equal(t, map[string]bool{
			publisher.GetId(): true,
			lister.GetId():    false,
			synced.GetId():    false,
		}, removed)
	})
}

// TestIntegrationRebase covers rebasing a branch whose work landed on the base as a squash.
func TestIntegrationRebase(t *testing.T) {
	isolateGit(t)

	instance := weavetest.Start(t, weavetest.Params{WorktreesDir: t.TempDir()})
	repo := initGitRepo(t)
	agentID, tree := createRebaseAgent(t, instance, repo)
	landOnTheBase(t, repo)
	writeIn(t, tree, "uncommitted.txt", "never committed\n")
	before := readGitOutput(t, tree, "rev-parse", "HEAD")

	require.NoError(t, instance.Harness.SetActivity(agentID, harness.ActivityWorking))
	instance.WaitForActivity(agentID, harness.ActivityWorking)

	const rest = 2 * time.Second
	settled := make(chan error, 1)
	go func() {
		time.Sleep(rest)
		settled <- instance.Harness.SetActivity(agentID, harness.ActivityIdle)
	}()
	t.Cleanup(func() { assert.NoError(t, <-settled) })

	started := time.Now()
	rebased, err := instance.Agents.RebaseWorktree(t.Context(), connect.NewRequest(&agentv1.RebaseWorktreeRequest{
		AgentId: agentID,
	}))
	require.NoError(t, err)

	t.Run("the branch waits for the agent to stop", func(t *testing.T) {
		assert.GreaterOrEqual(t, time.Since(started), rest)
	})

	t.Run("a branch whose work landed reaches the base without replaying anything", func(t *testing.T) {
		assert.True(t, rebased.Msg.GetMoved())
		assert.False(t, rebased.Msg.GetConflicted(), "which is the collision the cutoff exists to avoid")
		assert.Zero(t, rebased.Msg.GetReplayCommits())
		assert.Positive(t, rebased.Msg.GetLandedCommits())
		assert.Equal(t, readGitOutput(t, repo, "rev-parse", "origin/main"), readGitOutput(t, tree, "rev-parse", "HEAD"))
	})

	t.Run("the checkout is parked before it moves", func(t *testing.T) {
		parked := readGitOutput(t, tree, "for-each-ref", "--format=%(refname)", "refs/weave/prerefresh/"+agentID+"/")
		require.NotEmpty(t, parked)

		assert.Equal(t, before, readGitOutput(t, tree, "rev-parse", parked+"^"), "the park stands on the commit the branch was at")
		assert.Equal(t, "never committed", readGitOutput(t, tree, "show", parked+":uncommitted.txt"),
			"and holds what was never committed, which the reflog could not")
	})
}

func listWorktrees(t *testing.T, instance *weavetest.Instance) map[string]*worktreev1.Worktree {
	t.Helper()

	listed, err := instance.Worktrees.ListWorktrees(t.Context(), connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)

	return xslice.KeyBy(listed.Msg.GetWorktrees(), func(worktree *worktreev1.Worktree) string {
		return worktree.GetDir()
	})
}

func commitIn(t *testing.T, dir, name, subject string) {
	t.Helper()

	writeIn(t, dir, name, subject+"\n")
	runGitIn(t, dir, "add", name)
	runGitIn(t, dir, "commit", "--quiet", "-m", subject)
}

// createRebaseAgent is an agent on a worktree of its own with two commits on its branch.
func createRebaseAgent(t *testing.T, instance *weavetest.Instance, repo string) (agentID, tree string) {
	t.Helper()

	ctx := t.Context()

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name:     "backend",
		Cwd:      repo,
		Worktree: &workflowv1.WorktreeConfig{Enabled: true, BaseBranch: "main"},
	}))
	require.NoError(t, err)

	agent, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "do the work",
		Title:      "rebaser",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)

	tree = agent.Msg.GetAgent().GetCwd()
	require.DirExists(t, tree)
	instance.WaitForHarness(agent.Msg.GetAgent().GetId())

	writeIn(t, tree, "own.txt", "a first draft\n")
	runGitIn(t, tree, "add", "own.txt")
	runGitIn(t, tree, "commit", "--quiet", "-m", "a first draft")

	writeIn(t, tree, "own.txt", "the agent's work\n")
	runGitIn(t, tree, "add", "own.txt")
	runGitIn(t, tree, "commit", "--quiet", "-m", "the agent's work")

	return agent.Msg.GetAgent().GetId(), tree
}

// landOnTheBase puts the finished content of the agent's file on main as a commit of its own,
// the way a squash merge of its branch would.
func landOnTheBase(t *testing.T, repo string) {
	t.Helper()

	branch := readGitOutput(t, repo, "rev-parse", "--abbrev-ref", "HEAD")
	runGitIn(t, repo, "checkout", "--quiet", "main")
	writeIn(t, repo, "own.txt", "the agent's work\n")
	runGitIn(t, repo, "add", "own.txt")
	runGitIn(t, repo, "commit", "--quiet", "-m", "the agent's work, squashed")
	writeIn(t, repo, "elsewhere.txt", "another pull request\n")
	runGitIn(t, repo, "add", "elsewhere.txt")
	runGitIn(t, repo, "commit", "--quiet", "-m", "another pull request")
	runGitIn(t, repo, "push", "--quiet")
	runGitIn(t, repo, "checkout", "--quiet", branch)
}

// writeIn writes a file into a checkout.
func writeIn(t *testing.T, dir, name, body string) {
	t.Helper()

	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
}

func isolateGit(t *testing.T) {
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

// buildWorktreeWorkflow is the request for a workflow whose agents each get a worktree of their own.
func buildWorktreeWorkflow(t *testing.T) *workflowv1.CreateWorkflowRequest {
	t.Helper()

	return &workflowv1.CreateWorkflowRequest{
		Name:     "backend",
		Cwd:      initGitRepo(t),
		Worktree: &workflowv1.WorktreeConfig{Enabled: true},
	}
}
