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

const adoptLookupTimeout = 30 * time.Second

const worktreeListTimeout = 10 * time.Second

// pullRequestResolver reads a pull request and reports which repository a directory belongs
// to, so adoption and the archive sweep can check the two against each other.
type pullRequestResolver interface {
	Resolve(ctx context.Context, dir, ref string) (*PullRequest, error)
	Repository(ctx context.Context, dir string) (string, error)
}

// adoptAgentReadWriter is the slice of the agent store adoption reads, which is every agent the
// panel holds.
type adoptAgentReadWriter interface {
	List(ctx context.Context) ([]agentstore.Agent, error)
	ListAll(ctx context.Context) ([]agentstore.Agent, error)
	SetMetadata(ctx context.Context, params agentstore.SetMetadataParams) (bool, error)
}

// AdoptService puts an agent on a branch somebody else made, working out which checkout is to
// hold that branch before handing the launch itself over.
type AdoptService struct {
	agents       adoptAgentReadWriter
	workflows    workflowReader
	launch       *LaunchService
	pullRequests pullRequestResolver
}

type NewAdoptServiceParams struct {
	Agents    adoptAgentReadWriter
	Workflows workflowReader
	Launch    *LaunchService
	// PullRequests arms adoption's ability to take a pull request rather than a branch, and a
	// branch name still adopts without it.
	PullRequests pullRequestResolver
}

// NewAdoptService builds the service from its dependencies.
func NewAdoptService(params NewAdoptServiceParams) *AdoptService {
	return &AdoptService{
		agents:       params.Agents,
		workflows:    params.Workflows,
		launch:       params.Launch,
		pullRequests: params.PullRequests,
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

// Adoption is what one adopt call did: the agent on the branch, and whether it was already
// there rather than put there by this call.
type Adoption struct {
	Agent    *agentstore.Agent
	Existing bool
}

// Adopt puts an agent on a branch weave did not make, named directly or through a pull request,
// running it either in a worktree of its own or in the checkout already holding the branch.
func (s *AdoptService) Adopt(ctx context.Context, params AdoptParams) (Adoption, error) {
	ref := strings.TrimSpace(params.Ref)
	if ref == "" {
		return Adoption{}, fmt.Errorf("%w: nothing to adopt", ErrBadDirectory)
	}

	wf, err := s.workflows.Get(ctx, params.WorkflowID)
	if errors.Is(err, store.ErrNotFound) {
		return Adoption{}, fmt.Errorf("%w: %q", ErrWorkflowNotFound, params.WorkflowID)
	}
	if err != nil {
		return Adoption{}, fmt.Errorf("failed to look up workflow %q: %w", params.WorkflowID, err)
	}
	if !wf.Worktree.Enabled {
		return Adoption{}, fmt.Errorf("%w: %q does not", ErrAdoptNeedsWorktree, wf.Name)
	}

	repo := config.ExpandHome(strings.TrimSpace(wf.Cwd))
	if repo == "" {
		return Adoption{}, ErrWorktreeNeedsCwd
	}

	pr, branch, err := s.resolveAdoption(ctx, repo, ref)
	if err != nil {
		return Adoption{}, err
	}

	existing, err := s.agentOnBranch(ctx, params.WorkflowID, branch)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return Adoption{}, err
	}
	if err == nil {
		return Adoption{Agent: existing, Existing: true}, nil
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

	trees, err := readWorktrees(ctx, repo, branch)
	if err != nil {
		return Adoption{}, err
	}

	var owner *agentstore.Agent
	if trees.holder != "" {
		found, err := s.agentIn(ctx, trees.holder)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return Adoption{}, err
		}
		owner = found
	}

	switch {
	case trees.holder == "":
	case isSameDir(trees.holder, trees.main):
		return Adoption{}, fmt.Errorf("%w: %s is checked out in %s, which is the workflow's own checkout", ErrAdoptBranchInUse, branch, trees.main)
	case !doesDirExist(trees.holder):
		// Git still has the registration but the directory is gone, so a worktree of this agent's
		// own is made instead.
	case owner != nil && isAdopted(*owner, branch):
		return Adoption{Agent: owner, Existing: true}, nil
	case owner != nil:
		return Adoption{}, fmt.Errorf("%w: %s is the worktree of agent %s, so adopting into it would hand one directory to two agents", ErrAdoptBranchInUse, trees.holder, owner.Name)
	default:
		// The checkout is somebody else's work, so the provisioner is turned off to stop the
		// workflow making a second one and, on delete, removing a directory weave never created.
		create.Dir = trees.holder
		create.ProvisionEnabled = xptr.Ptr(false)
	}

	created, err := s.launch.NewAgent(ctx, create)
	if err != nil && !errors.Is(err, ErrAlreadyLaunched) {
		return Adoption{}, err
	}

	// An idempotency key answered with the agent it already launched is the same answer as a
	// branch that already had one.
	adoption := Adoption{Agent: created, Existing: errors.Is(err, ErrAlreadyLaunched)}

	if pr != nil {
		if _, err := s.agents.SetMetadata(ctx, agentstore.SetMetadataParams{
			ID:    created.ID,
			Key:   prMetadataKey,
			Value: &pr.URL,
		}); err != nil {
			return Adoption{}, fmt.Errorf("failed to record the pull request %s was adopted from: %w", created.Name, err)
		}
	}

	return adoption, nil
}

// resolveAdoption turns what was typed into the branch to adopt, reading the pull request
// behind it when that is what it names.
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

// agentOnBranch returns the live agent already adopted onto a branch in this workflow, so
// asking twice hands back the agent doing the work rather than fighting git over a branch
// that can only be checked out once.
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
// stamped with the branch it was going to work on. Counting that corpse as the agent on the
// branch answers every later adopt with it, so the branch can never be adopted again and the
// caller is told it succeeded. releaseFailedLaunch guards the idempotency key the same way.

// isAdopted reports whether an agent is the one working on a branch right now, which rules out a
// create that never launched, an agent whose worktree is gone, and an archived one whose worktree
// is still its own to remove.
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
// worktree, which is the checkout a human works in, and whichever checkout holds the branch.
type worktrees struct {
	main   string
	holder string
}

// readWorktrees reports the checkout holding a branch, blank when none does.
func readWorktrees(ctx context.Context, repo, branch string) (worktrees, error) {
	listCtx, cancel := context.WithTimeout(ctx, worktreeListTimeout)
	defer cancel()

	out, err := git.RunBytes(listCtx, repo, "worktree", "list", "--porcelain")
	if err != nil {
		return worktrees{}, err
	}

	want := "branch refs/heads/" + branch

	var found worktrees
	var dir string
	for line := range strings.SplitSeq(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			dir = strings.TrimPrefix(line, "worktree ")
			// git answers from anywhere inside a checkout and reports roots, so the main worktree is
			// the first one listed rather than the directory git was asked from.
			if found.main == "" {
				found.main = dir
			}
		case line == want:
			found.holder = dir
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
