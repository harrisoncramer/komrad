package agents

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"weave/config"
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

// adoptAgentStore is the slice of the agent store adoption reads, which is every agent the
// panel holds, since the question is which of them already has the branch or the directory.
type adoptAgentStore interface {
	List(ctx context.Context) ([]agentstore.Agent, error)
	ListAll(ctx context.Context) ([]agentstore.Agent, error)
	SetMetadata(ctx context.Context, params agentstore.SetMetadataParams) (bool, error)
}

// AdoptService puts an agent on a branch somebody else made, working out which checkout is to
// hold that branch before handing the launch itself over.
type AdoptService struct {
	agents       adoptAgentStore
	workflows    workflowReader
	launch       *LaunchService
	pullRequests pullRequestResolver
}

type AdoptServiceParams struct {
	Agents    adoptAgentStore
	Workflows workflowReader
	Launch    *LaunchService
	// PullRequests arms adoption's ability to take a pull request rather than a branch, and a
	// branch name still adopts without it.
	PullRequests pullRequestResolver
}

// NewAdoptService builds the service from its dependencies.
func NewAdoptService(p AdoptServiceParams) *AdoptService {
	return &AdoptService{
		agents:       p.Agents,
		workflows:    p.Workflows,
		launch:       p.Launch,
		pullRequests: p.PullRequests,
	}
}

type AdoptParams struct {
	Client         string
	Ref            string
	Title          string
	Flags          string
	Prompt         string
	Harness        string
	WorkflowID     string
	Hidden         bool
	IdempotencyKey string
	SetupPane      string
}

// Adopt puts an agent on a branch weave did not make, named directly or through a pull request,
// running it either in a worktree of its own or in the checkout already holding the branch.
func (s *AdoptService) Adopt(ctx context.Context, p AdoptParams) (*agentstore.Agent, error) {
	ref := strings.TrimSpace(p.Ref)
	if ref == "" {
		return nil, fmt.Errorf("%w: nothing to adopt", ErrBadDirectory)
	}

	wf, err := s.workflows.Get(ctx, p.WorkflowID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %q", ErrWorkflowNotFound, p.WorkflowID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to look up workflow %q: %w", p.WorkflowID, err)
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

	existing, err := s.agentOnBranch(ctx, p.WorkflowID, branch)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if err == nil {
		return existing, nil
	}

	params := NewAgentParams{
		Client:         p.Client,
		Title:          adoptTitle(p.Title, pr, branch),
		Flags:          p.Flags,
		Prompt:         adoptPrompt(pr, branch, p.Prompt),
		Harness:        p.Harness,
		WorkflowID:     p.WorkflowID,
		Hidden:         p.Hidden,
		IdempotencyKey: p.IdempotencyKey,
		SetupPane:      p.SetupPane,
		AdoptedBranch:  branch,
	}

	trees, err := readWorktrees(ctx, repo, branch)
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
	case sameDir(trees.holder, trees.main):
		return nil, fmt.Errorf("%w: %s is checked out in %s, which is the workflow's own checkout", ErrAdoptBranchInUse, branch, trees.main)
	case owner != nil && owner.WorktreeRemoved:
		// Git still has the registration but the directory is gone, so a worktree of this agent's
		// own is made instead.
	case owner != nil && adopted(*owner, branch):
		return owner, nil
	case owner != nil:
		return nil, fmt.Errorf("%w: %s is the worktree of agent %s, so adopting into it would hand one directory to two agents", ErrAdoptBranchInUse, trees.holder, owner.Name)
	default:
		// The checkout is somebody else's work, so both actions are blanked to stop the workflow
		// making a second one and, on delete, removing a directory weave never created.
		params.Dir = trees.holder
		params.SetupAction = xptr.Ptr("")
		params.CleanupAction = xptr.Ptr("")
	}

	created, err := s.launch.NewAgent(ctx, params)
	if err != nil && !errors.Is(err, ErrAlreadyLaunched) {
		return nil, err
	}

	if pr != nil {
		if _, err := s.agents.SetMetadata(ctx, agentstore.SetMetadataParams{ID: created.ID, Key: prMetadataKey, Value: &pr.URL}); err != nil {
			return nil, fmt.Errorf("failed to record the pull request %s was adopted from: %w", created.Name, err)
		}
	}

	return created, nil
}

// resolveAdoption turns what was typed into the branch to adopt, reading the pull request
// behind it when that is what it names.
func (s *AdoptService) resolveAdoption(ctx context.Context, repo, ref string) (*PullRequest, string, error) {
	if !looksLikePullRequest(ref) {
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
		if agent.WorkflowID == workflowID && adopted(agent, branch) {
			return &agent, nil
		}
	}

	return nil, fmt.Errorf("%w: no agent in workflow %q is adopted onto %q", store.ErrNotFound, workflowID, branch)
}

// adopted reports whether an agent is the one working on a branch right now, which rules out an
// agent whose worktree is gone and an archived one whose worktree is still its own to remove.
func adopted(agent agentstore.Agent, branch string) bool {
	return agent.AdoptedBranch == branch && !agent.WorktreeRemoved && agent.ArchivedAt == nil
}

// agentIn returns the agent whose cwd is a directory, archived ones included, since the first
// agent's cleanup would otherwise remove the directory out from under a second one.
func (s *AdoptService) agentIn(ctx context.Context, dir string) (*agentstore.Agent, error) {
	agents, err := s.agents.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	for _, agent := range agents {
		if agent.Cwd != "" && sameDir(agent.Cwd, dir) {
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

// readWorktrees reports the checkout holding a branch, blank when none does, since git refuses
// to check the same branch out twice.
func readWorktrees(ctx context.Context, repo, branch string) (worktrees, error) {
	listCtx, cancel := context.WithTimeout(ctx, worktreeListTimeout)
	defer cancel()

	out, err := exec.CommandContext(listCtx, "git", "-C", repo, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return worktrees{}, fmt.Errorf("git -C %s worktree list: %w", repo, err)
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

// sameDir compares two paths as the filesystem sees them, since a worktree listing reports
// resolved paths and a configured cwd is whatever somebody typed.
func sameDir(a, b string) bool {
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

// adoptPrompt puts what the agent is walking into in front of whatever it was asked to do, since
// an agent that does not know the branch is somebody else's will rewrite the commits on it.
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
