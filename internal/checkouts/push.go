package checkouts

import (
	"context"
	"fmt"

	"komrad/internal/fail"
	"komrad/internal/git"
	pullrequeststore "komrad/internal/store/pullrequest"
	taskstore "komrad/internal/store/task"
	"komrad/lib/xptr"
	"komrad/lib/xsha"
)

var (
	// errPushDetached says the checkout is on no branch, so there is nothing to name on origin.
	errPushDetached = fail.Precondition("the worktree is on no branch, so there is nothing to push")

	// errPushInProgress says a sequence owns the index, so the branch is not where it will end up.
	errPushInProgress = fail.Precondition("the worktree is part-way through a rebase, merge or pick, so finish or abort that first")

	// errPushBase says the checkout stands on the branch everything else is measured against.
	errPushBase = fail.Precondition("the worktree is on the workflow's base branch, which komrad never pushes")

	// errPushFinished says the branch's pull request has merged or closed and the checkout still stands where it ended.
	errPushFinished = fail.Precondition("the pull request from this branch has merged or closed and the checkout holds nothing newer, so there is nothing left to push")

	// errPushNothing says the branch holds nothing the base does not.
	errPushNothing = fail.Precondition("the branch holds nothing the base does not, so pushing it would leave its pull request empty")

	// errPushInSync says origin's copy of the branch already holds every commit on it.
	errPushInSync = fail.Precondition("origin already holds every commit on this branch, so there is nothing to push")

	// errPushAdoptedForce says a branch komrad did not cut would have origin's copy overwritten.
	errPushAdoptedForce = fail.Precondition("origin's copy of this branch has moved apart from it, and komrad never overwrites a branch it did not cut")
)

// PushPlan is what a push would do.
type PushPlan struct {
	Branch string
	// Commits is how many commits go up, counted against origin's copy, or against the base when
	// origin has none.
	Commits int
	// Forced says origin's copy holds commits the branch does not, so the push overwrites it.
	Forced bool
	// InSync says origin already holds every commit on the branch, so there is nothing to push.
	InSync bool
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
func (s *Service) PlanPush(ctx context.Context, ref string) (*PushPlan, error) {
	target, err := s.eligible(ctx, ref)
	if err != nil {
		return nil, err
	}

	return s.planPush(ctx, *target, false)
}

// Push sends the task's branch to origin, overwriting origin's copy when a rebase moved the
// branch apart from it.
func (s *Service) Push(ctx context.Context, ref string) (*PushResult, error) {
	target, err := s.eligible(ctx, ref)
	if err != nil {
		return nil, err
	}

	unlock, err := s.lockTree(target.dir())
	if err != nil {
		return nil, err
	}
	defer unlock()

	plan, err := s.planPush(ctx, *target, true)
	if err != nil {
		return nil, err
	}
	if plan.InSync {
		return nil, fmt.Errorf("%w: %s", errPushInSync, plan.Branch)
	}

	// A push cut off part-way leaves nobody knowing whether origin moved, so it runs to the end
	// whether or not whoever asked is still waiting.
	ctx = context.WithoutCancel(ctx)
	defer s.checkoutSync.Wake()
	defer s.heat(*target.task)

	sha, err := s.git.Push(ctx, git.PushParams{
		Dir:    target.dir(),
		Branch: plan.Branch,
		Force:  plan.Forced,
	})
	if err != nil {
		return nil, err
	}

	result := &PushResult{PushPlan: *plan, SHA: sha}
	if result.Forced {
		s.queueEvent(ctx, target.task.ID, pushed{
			TaskID: target.task.ID,
			Branch: result.Branch,
			SHA:    result.SHA,
		})
	}

	return result, nil
}

// planPush reads the checkout and says what a push of it would do, or why it may not have one,
// reading origin's copy of a finished branch afresh first when refresh is set.
func (s *Service) planPush(ctx context.Context, target checkoutTarget, refresh bool) (*PushPlan, error) {
	dir, checkout, flow := target.dir(), target.task.Checkout, target.workflow
	state, err := s.git.ReadTreeState(ctx, dir)
	if err != nil {
		return nil, err
	}

	switch {
	case state.InProgress:
		return nil, errPushInProgress
	case state.Branch == "":
		return nil, errPushDetached
	}

	if state.Branch == flow.Worktree.BaseBranch {
		return nil, fmt.Errorf("%w: %s", errPushBase, state.Branch)
	}

	finished, err := s.refuseFinished(ctx, target, state)
	if err != nil {
		return nil, err
	}
	if finished && refresh {
		if err := s.git.RefreshRemoteBranch(ctx, dir, state.Branch); err != nil {
			return nil, err
		}
	}

	plan, err := s.countPush(ctx, dir, state.Branch, flow.Worktree.BaseBranch)
	if err != nil {
		return nil, err
	}
	if plan.Forced && checkout.AdoptedBranch != "" {
		return nil, fmt.Errorf("%w: %s", errPushAdoptedForce, checkout.AdoptedBranch)
	}

	changes, err := s.git.PeekChanges(ctx, dir)
	if err != nil {
		return nil, err
	}
	plan.Uncommitted = len(changes.Modified) + len(changes.Untracked)

	return plan, nil
}

// countPush counts what goes up against origin's copy of the branch, or against the base when
// origin has none, refusing a branch that holds nothing of its own or nothing origin lacks.
func (s *Service) countPush(ctx context.Context, dir, branch, base string) (*PushPlan, error) {
	ref, err := s.git.ResolveBase(ctx, dir, base)
	if err != nil {
		return nil, err
	}

	own, _, err := s.git.CountAheadBehind(ctx, dir, ref)
	if err != nil {
		return nil, err
	}
	if own == 0 {
		return nil, fmt.Errorf("%w: %s", errPushNothing, branch)
	}

	remote, err := s.git.CountAgainstRemote(ctx, dir, branch)
	if err != nil {
		return nil, err
	}
	if !remote.Pushed {
		return &PushPlan{Branch: branch, Commits: own}, nil
	}
	if remote.Unpushed == 0 && remote.Unpulled > 0 {
		return nil, fmt.Errorf("%w: %s", git.ErrPushRejected, branch)
	}
	if remote.Unpushed == 0 {
		return &PushPlan{Branch: branch, InSync: true}, nil
	}

	return &PushPlan{
		Branch:  branch,
		Commits: remote.Unpushed,
		Forced:  remote.Unpulled > 0,
	}, nil
}

// refuseFinished turns down a branch whose pull request has merged or closed until the checkout
// moves past where it ended, and says whether there was such a pull request.
func (s *Service) refuseFinished(ctx context.Context, target checkoutTarget, state git.TreeState) (bool, error) {
	repository := xptr.Deref(target.task.Checkout.RepositoryURL)
	if repository == "" {
		return false, nil
	}

	listed, err := s.pullRequests.ListPage(ctx, pullrequeststore.ListPageParams{
		RepositoryURL: repository,
		HeadBranch:    state.Branch,
		Limit:         1,
	})
	if err != nil {
		return false, err
	}
	if len(listed) == 0 || !listed[0].IsFinished() {
		return false, nil
	}
	pr := listed[0]
	if state.HeadSHA == "" || state.HeadSHA == pr.HeadSHA {
		return false, fmt.Errorf("%w: %s", errPushFinished, pr.URL)
	}
	if pr.MergedAt != nil && pr.HeadSHA != "" {
		held, err := s.git.HoldsCommit(ctx, target.dir(), pr.HeadSHA)
		if err != nil {
			return false, err
		}
		if held {
			return false, fmt.Errorf("%w: %s", errPushFinished, pr.URL)
		}
	}

	return true, nil
}

// pushedMail tells a writer that the panel overwrote its branch on origin.
func pushedMail(pushed pushed) string {
	return fmt.Sprintf("Your branch %s was force-pushed to origin from the panel, replacing the copy origin held. "+
		"Origin's copy now matches your checkout at %s.\n\n"+
		"Carry on from here and push as you normally would. Do not pull or reset to the copy origin held before.",
		pushed.Branch, xsha.Short(pushed.SHA))
}

// heat keeps the pull request sync reading the host the task's checkout pushes to often for a
// while.
func (s *Service) heat(task taskstore.Task) {
	remote, err := git.ParseRemoteURL(xptr.Deref(task.Checkout.RepositoryURL))
	if err != nil || remote.Host == "" {
		return
	}

	s.pullRequestSync.Heat(remote.Host)
}
