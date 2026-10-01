package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"komrad/config"
	"komrad/internal/fail"
	"komrad/internal/git"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	"komrad/lib/xpath"
	"komrad/lib/xptr"
	"komrad/lib/xstr"
)

var ErrAdoptNeedsWorktree = fail.Precondition("adoption needs a workflow whose agents get a worktree of their own")

var ErrAdoptNotABranch = fail.InvalidArgument("komrad adopts a branch, and this is not one")

var ErrAdoptBranchInUse = fail.Precondition("that branch is already checked out")

// Adoption is the agent on an adopted branch, and whether it was there before the adoption.
type Adoption struct {
	Agent    *agentstore.Agent
	Existing bool
}

const worktreeListTimeout = 10 * time.Second

// checkoutReader reports the checkouts of a repository and where its origin points.
type checkoutReader interface {
	ListWorktrees(ctx context.Context, repo string) ([]git.Worktree, error)
	ReadRepositoryURL(ctx context.Context, dir string) string
}

// adoptAgentReadWriter is the slice of the agent store adoption reads.
type adoptAgentReadWriter interface {
	List(ctx context.Context) ([]agentstore.Agent, error)
	ListAll(ctx context.Context) ([]agentstore.Agent, error)
}

type NewAdoptServiceParams struct {
	Agents    adoptAgentReadWriter
	Workflows workflowReader
	Launch    *LaunchService
	Worktrees checkoutReader
}

// AdoptService puts an agent on a branch somebody else made, working out which checkout is to
// hold that branch before handing the launch over.
type AdoptService struct {
	agents    adoptAgentReadWriter
	workflows workflowReader
	launch    *LaunchService
	worktrees checkoutReader
}

// NewAdoptService builds the service from its dependencies.
func NewAdoptService(params NewAdoptServiceParams) *AdoptService {
	return &AdoptService{
		agents:    params.Agents,
		workflows: params.Workflows,
		launch:    params.Launch,
		worktrees: params.Worktrees,
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

// Adopt puts an agent on a branch komrad did not make, running it either in a worktree of its
// own or in the checkout already holding the branch.
func (s *AdoptService) Adopt(ctx context.Context, params AdoptParams) (*Adoption, error) {
	branch := strings.TrimSpace(params.Ref)
	if branch == "" {
		return nil, fmt.Errorf("%w: nothing to adopt", ErrBadDirectory)
	}
	if namesPullRequest(branch) {
		return nil, fmt.Errorf("%w: %s names a pull request, so adopt the branch it is on", ErrAdoptNotABranch, branch)
	}

	wf, err := s.workflows.Get(ctx, params.WorkflowID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrWorkflowNotFound.Saying("no workflow with id %q", params.WorkflowID)
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

	existing, err := s.agentOnBranch(ctx, params.WorkflowID, branch)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if err == nil {
		// A keyed retry of the call that launched it is the one repeat that means "again, safely"
		// rather than "a second agent on this branch".
		if isKeyedReplay(params.IdempotencyKey, *existing) {
			return &Adoption{Agent: existing}, nil
		}

		return &Adoption{Agent: existing, Existing: true}, nil
	}

	create := NewAgentParams{
		Title:          adoptTitle(params.Title, branch),
		Flags:          params.Flags,
		Prompt:         adoptPrompt(branch, params.Prompt),
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
	case xpath.IsSameDir(trees.holder, trees.main):
		return nil, fmt.Errorf("%w: %s is checked out in %s, which is the workflow's own checkout", ErrAdoptBranchInUse, branch, trees.main)
	case !doesDirExist(trees.holder):
		// Git still has the registration but the directory is gone, so a worktree of this agent's
		// own is made instead.
	case owner != nil:
		return &Adoption{Agent: owner, Existing: true}, nil
	default:
		// The checkout is somebody else's work, so the provisioner is turned off to stop the
		// workflow making a second one and, on delete, removing a directory komrad never created.
		create.Dir = trees.holder
		create.ProvisionEnabled = xptr.Ptr(false)
	}

	create.RepositoryURL = s.worktrees.ReadRepositoryURL(ctx, repo)
	created, err := s.launch.NewAgent(ctx, create)
	if errors.Is(err, ErrAlreadyLaunched) {
		return &Adoption{Agent: created}, nil
	}
	if err != nil {
		return nil, err
	}

	return &Adoption{Agent: created}, nil
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

// agentIn returns the top-level agent whose cwd is a directory, archived ones included.
func (s *AdoptService) agentIn(ctx context.Context, dir string) (*agentstore.Agent, error) {
	agents, err := s.agents.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	for _, agent := range agents {
		if agent.ParentAgentID != nil {
			continue
		}

		if agent.Cwd != "" && xpath.IsSameDir(agent.Cwd, dir) {
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

func adoptTitle(title, branch string) string {
	if strings.TrimSpace(title) != "" {
		return title
	}

	return xstr.Slug(branch[strings.LastIndex(branch, "/")+1:], 40)
}

// adoptPrompt puts what the agent is walking into in front of whatever it was asked to do.
func adoptPrompt(branch, prompt string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "You are picking up work that already exists. The branch %s is checked out in your working directory and holds commits you did not write, so read the log and the diff against its base before you change anything.\n", branch)

	b.WriteString("\nThe branch is not yours. Do not force-push it, do not rebase it, and do not delete it.\n")

	if strings.TrimSpace(prompt) == "" {
		b.WriteString("\nNothing has been asked of you yet. Say when you are ready and wait.")

		return b.String()
	}

	b.WriteString("\n")
	b.WriteString(prompt)

	return b.String()
}

// namesPullRequest reports whether a ref is a pull request rather than a branch, meaning a URL,
// a bare number, or a number behind a hash.
func namesPullRequest(ref string) bool {
	if strings.Contains(ref, "/pull/") || strings.Contains(ref, "/merge_requests/") {
		return true
	}

	digits := strings.TrimPrefix(ref, "#")
	if digits == "" {
		return false
	}

	return !strings.ContainsFunc(digits, func(r rune) bool { return r < '0' || r > '9' })
}
