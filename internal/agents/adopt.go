package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"weave/config"
	"weave/internal/git"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	"weave/lib/xptr"
	"weave/lib/xstr"
)

var ErrAdoptNeedsWorktree = errors.New("adoption needs a workflow whose agents get a worktree of their own")

var ErrAdoptNoResolver = errors.New("this panel cannot read pull requests, so name a branch instead")

var ErrAdoptRepoMismatch = errors.New("that pull request belongs to another repository than the workflow's")

var ErrAdoptForkBranch = errors.New("that pull request is from a fork, whose branch is not on this repository's origin")

var ErrAdoptBranchInUse = errors.New("that branch is already checked out")

var ErrAdoptAlreadyAdopted = errors.New("that branch already has an agent on it")

const adoptLookupTimeout = 30 * time.Second

const worktreeListTimeout = 10 * time.Second

// pullRequestResolver reads a pull request and reports which repository a directory belongs to.
type pullRequestResolver interface {
	Resolve(ctx context.Context, dir, ref string) (*PullRequest, error)
	Repository(ctx context.Context, dir string) (string, error)
}

// worktreeLister reports the checkouts of a repository.
type worktreeLister interface {
	ListWorktrees(ctx context.Context, repo string) ([]git.Worktree, error)
}

// GitWorktrees lists a repository's checkouts with git.
type GitWorktrees struct{}

// ListWorktrees reports every checkout of a repository, the main one first.
func (GitWorktrees) ListWorktrees(ctx context.Context, repo string) ([]git.Worktree, error) {
	return git.ListWorktrees(ctx, repo)
}

// adoptAgentReadWriter is the slice of the agent store adoption reads.
type adoptAgentReadWriter interface {
	List(ctx context.Context) ([]agentstore.Agent, error)
	ListAll(ctx context.Context) ([]agentstore.Agent, error)
	SetMetadata(ctx context.Context, params agentstore.SetMetadataParams) (bool, error)
}

// AdoptService puts an agent on a branch somebody else made, working out which checkout is to
// hold that branch before handing the launch over.
type AdoptService struct {
	agents       adoptAgentReadWriter
	workflows    workflowReader
	launch       *LaunchService
	pullRequests pullRequestResolver
	worktrees    worktreeLister
}

type NewAdoptServiceParams struct {
	Agents    adoptAgentReadWriter
	Workflows workflowReader
	Launch    *LaunchService
	// PullRequests lets adoption take a pull request rather than a branch, a branch name still
	// adopting without it.
	PullRequests pullRequestResolver
	Worktrees    worktreeLister
}

// NewAdoptService builds the service from its dependencies.
func NewAdoptService(params NewAdoptServiceParams) *AdoptService {
	return &AdoptService{
		agents:       params.Agents,
		workflows:    params.Workflows,
		launch:       params.Launch,
		pullRequests: params.PullRequests,
		worktrees:    params.Worktrees,
	}
}

type AdoptParams struct {
	Ref            string
	Title          string
	Flags          string
	Prompt         string
	Harness        string
	WorkflowID     string
	Hidden         bool
	IdempotencyKey string
}

// Adopt puts an agent on a branch weave did not make, named directly or through a pull request,
// running it either in a worktree of its own or in the checkout already holding the branch.
func (s *AdoptService) Adopt(ctx context.Context, params AdoptParams) (*agentstore.Agent, error) {
	ref := strings.TrimSpace(params.Ref)
	if ref == "" {
		return nil, fmt.Errorf("%w: nothing to adopt", ErrBadDirectory)
	}

	wf, err := s.workflows.Get(ctx, params.WorkflowID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %q", ErrWorkflowNotFound, params.WorkflowID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to look up workflow %q: %w", params.WorkflowID, err)
	}
	if !wf.Worktree.Enabled {
		return nil, fmt.Errorf("%w: %q does not", ErrAdoptNeedsWorktree, wf.Name)
	}

	repo := config.ExpandHome(strings.TrimSpace(wf.Cwd))
	if repo == "" {
		return nil, ErrWorktreeNeedsCwd
	}

	pr, branch, err := s.resolveAdoption(ctx, repo, ref)
	if err != nil {
		return nil, err
	}

	existing, err := s.agentOnBranch(ctx, params.WorkflowID, branch)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if err == nil {
		// A keyed retry of the call that launched it is the one repeat that means "again, safely"
		// rather than "a second agent on this branch".
		if isKeyedReplay(params.IdempotencyKey, *existing) {
			return existing, nil
		}

		return nil, fmt.Errorf("%w: %s is on %s already", ErrAdoptAlreadyAdopted, existing.Name, branch)
	}

	create := NewAgentParams{
		Title:          adoptTitle(params.Title, pr, branch),
		Flags:          params.Flags,
		Prompt:         adoptPrompt(pr, branch, params.Prompt),
		Harness:        params.Harness,
		WorkflowID:     params.WorkflowID,
		Hidden:         params.Hidden,
		IdempotencyKey: params.IdempotencyKey,
		AdoptedBranch:  branch,
	}

	trees, err := s.readWorktrees(ctx, repo, branch)
	if err != nil {
		return nil, err
	}

	var owner *agentstore.Agent
	if trees.holder != "" {
		found, err := s.agentIn(ctx, trees.holder)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		owner = found
	}

	switch {
	case trees.holder == "":
	case isSameDir(trees.holder, trees.main):
		return nil, fmt.Errorf("%w: %s is checked out in %s, which is the workflow's own checkout", ErrAdoptBranchInUse, branch, trees.main)
	case !doesDirExist(trees.holder):
		// Git still has the registration but the directory is gone, so a worktree of this agent's
		// own is made instead.
	case owner != nil && isAdopted(*owner, branch):
		return nil, fmt.Errorf("%w: %s is on %s already, in %s", ErrAdoptAlreadyAdopted, owner.Name, branch, trees.holder)
	case owner != nil:
		return nil, fmt.Errorf("%w: %s is the worktree of agent %s, so adopting into it would hand one directory to two agents", ErrAdoptBranchInUse, trees.holder, owner.Name)
	default:
		// The checkout is somebody else's work, so the provisioner is turned off to stop the
		// workflow making a second one and, on delete, removing a directory weave never created.
		create.Dir = trees.holder
		create.ProvisionEnabled = xptr.Ptr(false)
	}

	created, err := s.launch.NewAgent(ctx, create)
	if err != nil && !errors.Is(err, ErrAlreadyLaunched) {
		return nil, err
	}

	if pr != nil {
		if _, err := s.agents.SetMetadata(ctx, agentstore.SetMetadataParams{
			ID:    created.ID,
			Key:   prMetadataKey,
			Value: &pr.URL,
		}); err != nil {
			return nil, fmt.Errorf("failed to record the pull request %s was adopted from: %w", created.Name, err)
		}
	}

	return created, nil
}

// resolveAdoption turns what was typed into the branch to adopt, reading the pull request
// behind it when that is what was named.
func (s *AdoptService) resolveAdoption(ctx context.Context, repo, ref string) (*PullRequest, string, error) {
	if !isPullRequestRef(ref) {
		return nil, ref, nil
	}
	if s.pullRequests == nil {
		return nil, "", ErrAdoptNoResolver
	}

	lookupCtx, cancel := context.WithTimeout(ctx, adoptLookupTimeout)
	defer cancel()

	pr, err := s.pullRequests.Resolve(lookupCtx, repo, ref)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read the pull request %s: %w", ref, err)
	}
	if pr.CrossRepository {
		return nil, "", fmt.Errorf("%w: %s", ErrAdoptForkBranch, pr.URL)
	}

	origin, err := s.pullRequests.Repository(lookupCtx, repo)
	if err != nil {
		return nil, "", fmt.Errorf("failed to work out which repository %s belongs to: %w", repo, err)
	}
	if pr.Repository != "" && origin != "" && !strings.EqualFold(pr.Repository, origin) {
		return nil, "", fmt.Errorf("%w: %s is on %s where %s is on %s", ErrAdoptRepoMismatch, pr.URL, pr.Repository, repo, origin)
	}

	return pr, pr.Branch, nil
}

// agentOnBranch returns the live agent already adopted onto a branch in this workflow.
func (s *AdoptService) agentOnBranch(ctx context.Context, workflowID, branch string) (*agentstore.Agent, error) {
	agents, err := s.agents.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	for _, agent := range agents {
		if agent.WorkflowID == workflowID && isAdopted(agent, branch) {
			return &agent, nil
		}
	}

	return nil, fmt.Errorf("%w: no agent in workflow %q is adopted onto %q", store.ErrNotFound, workflowID, branch)
}

// Explainer: a create that fails during setup leaves its row behind in failed_to_create, still
// stamped with the branch it was going to work on. Counting that corpse as the agent on the branch
// would refuse the branch for good, since the retry that is the whole point of a refusal is the
// call being refused. releaseFailedLaunch guards the idempotency key the same way.

// isKeyedReplay reports whether an agent is the one a given idempotency key already launched.
func isKeyedReplay(key string, agent agentstore.Agent) bool {
	return key != "" && agent.IdempotencyKey != nil && *agent.IdempotencyKey == key
}

// isAdopted reports whether an agent is the one working on a branch right now, which rules out
// a create that never launched, an agent whose worktree is gone, and an archived one.
func isAdopted(agent agentstore.Agent, branch string) bool {
	return agent.AdoptedBranch == branch &&
		agent.State != agentstore.StateFailedToCreate &&
		!agent.WorktreeRemoved &&
		agent.ArchivedAt == nil
}

// agentIn returns the agent whose cwd is a directory, archived ones included.
func (s *AdoptService) agentIn(ctx context.Context, dir string) (*agentstore.Agent, error) {
	agents, err := s.agents.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	for _, agent := range agents {
		if agent.Cwd != "" && isSameDir(agent.Cwd, dir) {
			return &agent, nil
		}
	}

	return nil, fmt.Errorf("%w: no agent is working in %q", store.ErrNotFound, dir)
}

// worktrees is what one listing of a repository's checkouts says about a branch: the main
// worktree a human works in, and whichever checkout holds the branch.
type worktrees struct {
	main   string
	holder string
}

// readWorktrees reports the checkout holding a branch, blank when none does.
func (s *AdoptService) readWorktrees(ctx context.Context, repo, branch string) (worktrees, error) {
	listCtx, cancel := context.WithTimeout(ctx, worktreeListTimeout)
	defer cancel()

	listed, err := s.worktrees.ListWorktrees(listCtx, repo)
	if err != nil {
		return worktrees{}, err
	}

	var found worktrees
	for _, tree := range listed {
		// git answers from anywhere inside a checkout and reports roots, so the main worktree is
		// the first one listed rather than the directory git was asked from.
		if found.main == "" {
			found.main = tree.Dir
		}

		if tree.Branch == branch {
			found.holder = tree.Dir
		}
	}

	return found, nil
}

// doesDirExist reports whether a path is a directory right now.
func doesDirExist(dir string) bool {
	info, err := os.Stat(dir)

	return err == nil && info.IsDir()
}

// isSameDir compares two paths as the filesystem sees them.
func isSameDir(a, b string) bool {
	resolvedA, err := filepath.EvalSymlinks(a)
	if err != nil {
		resolvedA = a
	}

	resolvedB, err := filepath.EvalSymlinks(b)
	if err != nil {
		resolvedB = b
	}

	return filepath.Clean(resolvedA) == filepath.Clean(resolvedB)
}

func adoptTitle(title string, pr *PullRequest, branch string) string {
	if strings.TrimSpace(title) != "" {
		return title
	}
	if pr != nil {
		return fmt.Sprintf("pr-%d", pr.Number)
	}

	return xstr.Slug(branch[strings.LastIndex(branch, "/")+1:], 40)
}

// adoptPrompt puts what the agent is walking into in front of whatever it was asked to do.
func adoptPrompt(pr *PullRequest, branch, prompt string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "You are picking up work that already exists. The branch %s is checked out in your working directory and holds commits you did not write, so read the log and the diff against its base before you change anything.\n", branch)

	if pr != nil {
		fmt.Fprintf(&b, "\nIt is pull request %s, %q, open against %s. Read what people have already said on it with gh before you decide anything.\n", pr.URL, pr.Title, pr.BaseBranch)
	}

	b.WriteString("\nThe branch is not yours. Do not force-push it, do not rebase it, and do not delete it.\n")

	if strings.TrimSpace(prompt) == "" {
		b.WriteString("\nNothing has been asked of you yet. Say when you are ready and wait.")

		return b.String()
	}

	b.WriteString("\n")
	b.WriteString(prompt)

	return b.String()
}
