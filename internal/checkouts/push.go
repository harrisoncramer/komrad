package checkouts

import (
	"context"
	"fmt"
	"log/slog"

	"komrad/internal/events"
	"komrad/internal/fail"
	"komrad/internal/git"
	"komrad/internal/logging"
	agentstore "komrad/internal/store/agent"
	pullrequeststore "komrad/internal/store/pullrequest"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

var (
	// ErrPushDetached says the checkout is on no branch, so there is nothing to name on origin.
	ErrPushDetached = fail.Precondition("the worktree is on no branch, so there is nothing to push")

	// ErrPushInProgress says a sequence owns the index, so the branch is not where it will end up.
	ErrPushInProgress = fail.Precondition("the worktree is part-way through a rebase, merge or pick, so finish or abort that first")

	// ErrPushBase says the checkout stands on the branch everything else is measured against.
	ErrPushBase = fail.Precondition("the worktree is on the workflow's base branch, which komrad never pushes")

	// ErrPushFinished says the branch's pull request has merged or closed.
	ErrPushFinished = fail.Precondition("the pull request from this branch has merged or closed, so there is nothing left to push it for")

	// ErrPushNothing says the branch holds nothing the base does not.
	ErrPushNothing = fail.Precondition("the branch holds nothing the base does not, so pushing it would leave its pull request empty")

	// ErrPushAdoptedForce says a branch komrad did not cut would have origin's copy overwritten.
	ErrPushAdoptedForce = fail.Precondition("origin's copy of this branch has moved apart from it, and komrad never overwrites a branch it did not cut")
)

// PushPlan is what a push would do.
type PushPlan struct {
	Branch string
	// Commits is how many commits go up, counted against origin's copy, or against the base when
	// origin has none.
	Commits int
	// Forced says origin's copy holds commits the branch does not, so the push overwrites it.
	Forced bool
	// Uncommitted is how many files stay behind because nothing has committed them.
	Uncommitted int
}

// PushResult is what a push did.
type PushResult struct {
	PushPlan
	// SHA is the commit origin's copy of the branch now names.
	SHA string
}

// PlanPush reads what a push would do without sending anything.
func (s *Service) PlanPush(ctx context.Context, agentID string) (*PushPlan, error) {
	agent, err := s.eligible(ctx, agentID)
	if err != nil {
		return nil, err
	}

	return s.planPush(ctx, *agent)
}

// Push sends the agent's branch to origin, overwriting origin's copy when a rebase moved the
// branch apart from it.
func (s *Service) Push(ctx context.Context, agentID string) (*PushResult, error) {
	agent, err := s.eligible(ctx, agentID)
	if err != nil {
		return nil, err
	}

	unlock, taken := s.locks.TryLock(agent.Cwd)
	if !taken {
		return nil, fmt.Errorf("%w: %s", ErrBusy, agent.Cwd)
	}
	defer unlock()

	plan, err := s.planPush(ctx, *agent)
	if err != nil {
		return nil, err
	}

	// A push cut off part-way leaves nobody knowing whether origin moved, so it runs to the end
	// whether or not whoever asked is still waiting.
	ctx = context.WithoutCancel(ctx)
	defer s.checkoutSync.Wake()
	defer s.pullRequestSync.Wake()

	sha, err := s.pusher.Push(ctx, git.PushParams{
		Dir:    agent.Cwd,
		Branch: plan.Branch,
		Force:  plan.Forced,
	})
	if err != nil {
		return nil, err
	}

	result := &PushResult{PushPlan: *plan, SHA: sha}
	if result.Forced {
		s.tellPushed(ctx, *agent, *result)
	}

	return result, nil
}

// planPush reads the checkout and says what a push of it would do, or why it may not have one.
func (s *Service) planPush(ctx context.Context, agent agentstore.Agent) (*PushPlan, error) {
	state, err := s.pusher.ReadTreeState(ctx, agent.Cwd)
	if err != nil {
		return nil, err
	}

	switch {
	case state.InProgress:
		return nil, ErrPushInProgress
	case state.Branch == "":
		return nil, ErrPushDetached
	}

	flow, err := s.workflows.Get(ctx, agent.WorkflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to read the workflow of agent %q: %w", agent.Name, err)
	}
	if state.Branch == flow.Worktree.BaseBranch {
		return nil, fmt.Errorf("%w: %s", ErrPushBase, state.Branch)
	}

	if err := s.refuseFinished(ctx, agent, state.Branch); err != nil {
		return nil, err
	}

	plan, err := s.countPush(ctx, agent.Cwd, state.Branch, flow.Worktree.BaseBranch)
	if err != nil {
		return nil, err
	}
	if plan.Forced && agent.AdoptedBranch != "" {
		return nil, fmt.Errorf("%w: %s", ErrPushAdoptedForce, agent.AdoptedBranch)
	}

	changes, err := s.pusher.PeekChanges(ctx, agent.Cwd)
	if err != nil {
		return nil, err
	}
	plan.Uncommitted = len(changes.Modified) + len(changes.Untracked)

	return plan, nil
}

// countPush counts what goes up against origin's copy of the branch, or against the base when
// origin has none, refusing a branch that holds nothing of its own.
func (s *Service) countPush(ctx context.Context, dir, branch, base string) (*PushPlan, error) {
	ref, err := s.pusher.ResolveBase(ctx, dir, base)
	if err != nil {
		return nil, err
	}

	own, _, err := s.pusher.CountAheadBehind(ctx, dir, ref)
	if err != nil {
		return nil, err
	}
	if own == 0 {
		return nil, fmt.Errorf("%w: %s", ErrPushNothing, branch)
	}

	remote, err := s.pusher.CountAgainstRemote(ctx, dir, branch)
	if err != nil {
		return nil, err
	}
	if !remote.Pushed {
		return &PushPlan{Branch: branch, Commits: own}, nil
	}

	return &PushPlan{
		Branch:  branch,
		Commits: remote.Unpushed,
		Forced:  remote.Unpulled > 0,
	}, nil
}

// refuseFinished turns down a branch whose pull request has merged or closed, which the forge
// may already have deleted.
func (s *Service) refuseFinished(ctx context.Context, agent agentstore.Agent, branch string) error {
	if agent.RepositoryURL == nil || *agent.RepositoryURL == "" {
		return nil
	}

	listed, err := s.pullRequests.ListPage(ctx, pullrequeststore.ListPageParams{
		RepositoryURL: *agent.RepositoryURL,
		HeadBranch:    branch,
		Limit:         1,
	})
	if err != nil {
		return err
	}
	if len(listed) == 0 || !listed[0].IsFinished() {
		return nil
	}

	return fmt.Errorf("%w: %s", ErrPushFinished, listed[0].URL)
}

// tellPushed says in the agent's mailbox that its branch on origin was overwritten.
func (s *Service) tellPushed(ctx context.Context, agent agentstore.Agent, result PushResult) {
	event, err := events.NewEvent(Pushed{
		AgentID: agent.ID,
		Branch:  result.Branch,
		SHA:     result.SHA,
	})
	if err == nil {
		err = s.events.InsertManyTx(ctx, func(pgx.Tx) ([]river.JobArgs, error) {
			return []river.JobArgs{*event}, nil
		})
	}
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to tell an agent its branch was force-pushed",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
	}
}

// pushedMail is what an agent is told once its branch on origin was overwritten from the panel.
func pushedMail(pushed Pushed) string {
	return fmt.Sprintf("Your branch %s was force-pushed to origin from the panel, after a rebase rewrote it. "+
		"Origin's copy now matches your checkout at %s.\n\n"+
		"Carry on from here and push as you normally would. Do not pull or reset to the copy origin held before.",
		pushed.Branch, git.ShortSHA(pushed.SHA))
}
