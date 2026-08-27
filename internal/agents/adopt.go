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

// pullRequestResolver reads a pull request somebody else opened, and reports which
// repository a directory belongs to so the two can be checked against each other.
type pullRequestResolver interface {
	Resolve(ctx context.Context, dir, ref string) (*PullRequest, error)
	Repository(ctx context.Context, dir string) (string, error)
}

// WithPullRequests arms adoption's ability to take a pull request rather than a branch.
// Without it a branch name still adopts, since nothing outside the repository is read.
func (s *AgentService) WithPullRequests(r pullRequestResolver) *AgentService {
	s.pullRequests = r

	return s
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

// AdoptAgent puts an agent on a branch weave did not make, named directly or through a pull
// request. The workflow decides the repository, and weave decides where the agent runs: a
// worktree of its own on that branch, or the checkout already holding it.
func (s *AgentService) AdoptAgent(ctx context.Context, p AdoptParams) (*agentstore.Agent, error) {
	ref := strings.TrimSpace(p.Ref)
	if ref == "" {
		return nil, fmt.Errorf("%w: nothing to adopt", ErrBadDirectory)
	}

	wf, err := s.workflows.Get(ctx, p.WorkflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to look up workflow %q: %w", p.WorkflowID, err)
	}
	if wf == nil {
		return nil, fmt.Errorf("%w: %q", ErrWorkflowNotFound, p.WorkflowID)
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

	if existing, err := s.agentOnBranch(ctx, p.WorkflowID, branch); err != nil {
		return nil, err
	} else if existing != nil {
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

	owner, err := s.agentIn(ctx, trees.holder)
	if err != nil {
		return nil, err
	}

	switch {
	case trees.holder == "":
	case sameDir(trees.holder, trees.main):
		return nil, fmt.Errorf("%w: %s is checked out in %s, which is the workflow's own checkout", ErrAdoptBranchInUse, branch, trees.main)
	case owner != nil && owner.WorktreeRemoved:
		// Git still has the registration but the directory is gone, so there is nothing to
		// launch into. A worktree of this agent's own is made instead, which the add's
		// prune-and-retry is what gets past the registration left behind.
	case owner != nil && adopted(*owner, branch):
		return owner, nil
	case owner != nil:
		return nil, fmt.Errorf("%w: %s is the worktree of agent %s, so adopting into it would hand one directory to two agents", ErrAdoptBranchInUse, trees.holder, owner.Name)
	default:
		// The checkout is somebody else's work, made by hand or by another tool, so the agent
		// is launched into it as it stands rather than provisioned one. Blanking both actions
		// is what stops the workflow's worktree pair from making a second checkout and, on
		// delete, from removing a directory weave never created.
		params.Dir = trees.holder
		params.SetupAction = xptr.Ptr("")
		params.CleanupAction = xptr.Ptr("")
	}

	created, err := s.NewAgent(ctx, params)
	if err != nil {
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
func (s *AgentService) resolveAdoption(ctx context.Context, repo, ref string) (*PullRequest, string, error) {
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
func (s *AgentService) agentOnBranch(ctx context.Context, workflowID, branch string) (*agentstore.Agent, error) {
	agents, err := s.agents.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	for _, agent := range agents {
		if agent.WorkflowID == workflowID && adopted(agent, branch) {
			return &agent, nil
		}
	}

	return nil, nil
}

// adopted reports whether an agent is the one working on a branch right now. An agent whose
// worktree has since been removed is not: it has no directory to be working in, and holding
// the branch against it would leave nothing able to take the work on but a delete. Nor is an
// archived one, which has been stopped: its worktree is still its own to be torn down with,
// so it is something to be refused over rather than handed back as the agent on the job.
func adopted(agent agentstore.Agent, branch string) bool {
	return agent.AdoptedBranch == branch && !agent.WorktreeRemoved && agent.ArchivedAt == nil
}

// agentIn returns the agent whose cwd is a directory, archived ones included, since an
// archived agent's worktree is still on disk and still that agent's to be torn down with.
// Launching a second agent into it would have the first one's cleanup remove the directory
// out from under it.
func (s *AgentService) agentIn(ctx context.Context, dir string) (*agentstore.Agent, error) {
	if dir == "" {
		return nil, nil
	}

	agents, err := s.agents.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	for _, agent := range agents {
		if agent.Cwd != "" && sameDir(agent.Cwd, dir) {
			return &agent, nil
		}
	}

	return nil, nil
}

// worktrees is what one listing of a repository's checkouts says about a branch: the main
// worktree, which is the checkout a human works in, and whichever checkout holds the branch.
type worktrees struct {
	main   string
	holder string
}

// readWorktrees reports the checkout holding a branch, blank when none does. Git refuses to
// check a branch out twice, so this is what tells an adoption that has a worktree to make
// from one that has a directory to be launched into. The main worktree is read from the same
// listing rather than taken to be the directory git was asked from, since git answers from
// anywhere inside a checkout and reports roots.
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

// adoptPrompt puts what the agent is walking into in front of whatever it was asked to do.
// An agent that does not know the branch is somebody else's treats the commits on it as its
// own and rewrites them.
func adoptPrompt(pr *PullRequest, branch, prompt string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "You are picking up work that already exists. The branch %s is checked out in your working directory and holds commits you did not write, so read the log and the diff against its base before you change anything.\n", branch)

	if pr != nil {
		fmt.Fprintf(&b, "\nIt is pull request %s, %q, open against %s. Read what people have already said on it with gh before you decide anything.\n", pr.URL, pr.Title, pr.BaseBranch)
	}

	b.WriteString("\nThe branch is not yours. Do not force-push it, do not rebase it, and do not delete it.\n")

	if strings.TrimSpace(prompt) == "" {
		b.WriteString("\nSay what state the work is in and what you would do next, then wait.")

		return b.String()
	}

	b.WriteString("\n")
	b.WriteString(prompt)

	return b.String()
}
