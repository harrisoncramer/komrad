package checkouts

import (
	"context"
	"errors"
	"fmt"

	"komrad/internal/eventprompts"
	"komrad/internal/fail"
	"komrad/internal/git"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	pullrequeststore "komrad/internal/store/pullrequest"
	taskstore "komrad/internal/store/task"
	"komrad/lib/xptr"
	"komrad/lib/xsha"
)

var (
	// errStandsOnNone is a stand placed on a task that stands on no other task.
	errStandsOnNone = fail.Precondition("this task stands on no other task")
	// errStandNotHeld is a stand placed on a commit the task's branch does not hold.
	errStandNotHeld = fail.Precondition("the task's branch does not hold this commit, so its own commits cannot sit on it")
)

// restackOnto is where a standing task's own commits are bound.
type restackOnto struct {
	// Branch is the parent's branch, or the workflow's base branch once the parent merged.
	Branch string
	SHA    string
	Merged bool
}

// Restack is one pass over every standing task, moving each whose parent has moved onto the parent's new tip.
func (s *Service) Restack(ctx context.Context) error {
	standing, err := s.tasks.ListStanding(ctx)
	if err != nil {
		return err
	}

	var errs []error
	for _, task := range standing {
		if err := s.restackTask(ctx, task); err != nil {
			errs = append(errs, fmt.Errorf("failed to restack task %q: %w", task.Slug, err))
		}
	}

	return errors.Join(errs...)
}

// restackTask brings one standing task up to date with the task it stands on.
func (s *Service) restackTask(ctx context.Context, task taskstore.Task) error {
	if !task.Checkout.IsUsable() || task.Stand.Blocked == taskstore.RestackLost {
		return nil
	}

	parent, err := s.tasks.Get(ctx, task.Stand.ParentID)
	if err != nil {
		return err
	}

	folded, err := s.foldIntoParent(ctx, task, *parent)
	if err != nil || folded {
		return err
	}

	pr, err := s.readStandPullRequest(ctx, task, *parent)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}

	onto, due, err := s.readOnto(ctx, task, *parent, pr)
	if err != nil || !due {
		return err
	}

	return s.moveOnto(ctx, task, *parent, onto)
}

// foldIntoParent ends the stand of a task whose own pull request merged into the parent's
// branch, moving the tasks standing on it onto the parent, and reports whether it did.
func (s *Service) foldIntoParent(ctx context.Context, task, parent taskstore.Task) (bool, error) {
	own, err := s.readPullRequest(ctx, task, task.GetBranch())
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if own.MergedAt == nil || own.OpenedAt.Before(task.CreatedAt) || own.BaseBranch != parent.GetBranch() {
		return false, nil
	}

	riders, err := s.tasks.ListStandingOn(ctx, []string{task.ID})
	if err != nil {
		return false, err
	}

	for _, rider := range riders {
		if err := s.tasks.SetStand(ctx, rider.ID, &taskstore.Stand{
			ParentID:      parent.ID,
			Tip:           rider.Stand.Tip,
			PullRequestID: task.Stand.PullRequestID,
		}); err != nil {
			return false, err
		}
	}

	return true, s.tasks.SetStand(ctx, task.ID, nil)
}

// readStandPullRequest is the parent's pull request the task follows, recording the first one
// opened from the parent's branch since the task began when it follows none yet, and
// store.ErrNotFound while there is none.
func (s *Service) readStandPullRequest(ctx context.Context, task, parent taskstore.Task) (*pullrequeststore.PullRequest, error) {
	if id := task.Stand.PullRequestID; id != "" {
		return s.pullRequests.GetByID(ctx, id)
	}

	repository := xptr.Deref(task.Checkout.RepositoryURL)
	if repository == "" {
		return nil, fmt.Errorf("%w: task %q has no repository", store.ErrNotFound, task.Slug)
	}

	found, err := s.pullRequests.GetStandPullRequest(ctx, pullrequeststore.GetStandPullRequestParams{
		RepositoryURL: repository,
		HeadBranch:    parent.GetBranch(),
		Since:         task.CreatedAt,
	})
	if err != nil {
		return nil, err
	}

	if _, err := s.tasks.RecordStandPullRequest(ctx, task.ID, found.ID); err != nil {
		return nil, err
	}

	return found, nil
}

// readPullRequest is the newest pull request from a branch of the task's repository, store.ErrNotFound when there is none.
func (s *Service) readPullRequest(ctx context.Context, task taskstore.Task, branch string) (*pullrequeststore.PullRequest, error) {
	repository := xptr.Deref(task.Checkout.RepositoryURL)
	if repository == "" {
		return nil, fmt.Errorf("%w: task %q has no repository", store.ErrNotFound, task.Slug)
	}

	listed, err := s.pullRequests.ListPage(ctx, pullrequeststore.ListPageParams{
		RepositoryURL: repository,
		HeadBranch:    branch,
		Limit:         1,
	})
	if err != nil {
		return nil, err
	}
	if len(listed) == 0 {
		return nil, fmt.Errorf("%w: no pull request from %s", store.ErrNotFound, branch)
	}

	return &listed[0], nil
}

// readOnto is where the task's commits belong now, and whether they are due to move there.
func (s *Service) readOnto(ctx context.Context, task, parent taskstore.Task, pr *pullrequeststore.PullRequest) (restackOnto, bool, error) {
	dir := task.Checkout.Cwd

	if pr != nil && pr.MergedAt != nil {
		flow, err := s.workflows.Get(ctx, task.WorkflowID)
		if err != nil {
			return restackOnto{}, false, err
		}

		_, sha, err := s.git.FetchBase(ctx, dir, flow.Worktree.BaseBranch)
		if err != nil {
			return restackOnto{}, false, err
		}

		held, err := s.git.HoldsCommit(ctx, dir, sha)
		if err != nil {
			return restackOnto{}, false, err
		}
		if held {
			return restackOnto{}, false, s.tasks.SetStand(ctx, task.ID, nil)
		}

		return restackOnto{Branch: flow.Worktree.BaseBranch, SHA: sha, Merged: true}, true, nil
	}

	if pr != nil && pr.ClosedAt != nil {
		return restackOnto{}, false, s.tellParentClosed(ctx, task, parent, *pr)
	}

	tip, err := s.git.FetchBranchTip(ctx, dir, parent.GetBranch())
	if errors.Is(err, git.ErrBranchNotOnOrigin) {
		return restackOnto{}, false, nil
	}
	if err != nil {
		return restackOnto{}, false, err
	}

	if tip == task.Stand.Tip {
		return restackOnto{}, false, nil
	}
	if pr != nil && pr.HeadSHA != tip {
		return restackOnto{}, false, nil
	}

	held, err := s.git.HoldsCommit(ctx, dir, tip)
	if err != nil {
		return restackOnto{}, false, err
	}
	if held {
		return restackOnto{}, false, s.tasks.SetStandTip(ctx, task.ID, tip)
	}

	return restackOnto{Branch: parent.GetBranch(), SHA: tip}, true, nil
}

// tellParentClosed marks the task as waiting on the person and tells its writer, once.
func (s *Service) tellParentClosed(ctx context.Context, task, parent taskstore.Task, pr pullrequeststore.PullRequest) error {
	if task.Stand.Blocked == taskstore.RestackParentClosed {
		return nil
	}

	if err := s.tasks.SetRestackBlocked(ctx, task.ID, taskstore.RestackParentClosed); err != nil {
		return err
	}

	s.queueEvent(ctx, task.ID, restacked{
		TaskID: task.ID,
		Closed: &eventprompts.ParentClosed{Parent: parent.Slug, URL: pr.URL},
	})

	return nil
}

// moveOnto replays the task's own commits after the recorded tip onto where they belong, while
// its writer is at rest and its worktree holds nothing uncommitted.
func (s *Service) moveOnto(ctx context.Context, task, parent taskstore.Task, onto restackOnto) error {
	dir := task.Checkout.Cwd

	holdsTip, err := s.git.HoldsCommit(ctx, dir, task.Stand.Tip)
	if err != nil {
		return err
	}
	if !holdsTip {
		return s.tasks.SetRestackBlocked(ctx, task.ID, taskstore.RestackLost)
	}

	if task.Stand.Blocked == taskstore.RestackConflicted || task.Checkout.AdoptedBranch != "" {
		return nil
	}

	unlock, taken := s.locks.TryLock(dir)
	if !taken {
		return nil
	}
	defer unlock()

	ready, err := s.isReadyToRestack(ctx, task)
	if err != nil || !ready {
		return err
	}

	plan, err := s.planRestack(ctx, dir, task.Stand.Tip, onto.SHA)
	if err != nil {
		return err
	}

	park := buildParkRef(parkRebase, task.ID)
	if _, err := s.git.Park(ctx, dir, park); err != nil {
		return err
	}
	defer s.checkoutSync.Wake()

	err = s.git.RebaseOnto(ctx, dir, onto.SHA, plan.Cutoff)
	if errors.Is(err, git.ErrRebaseConflicted) {
		return s.backOutRestack(ctx, task, parent, restackBackOut{Onto: onto, Plan: *plan})
	}
	if err != nil {
		return err
	}

	s.releaseOlderParks(ctx, task, park)

	if onto.Merged {
		err = s.tasks.SetStand(ctx, task.ID, nil)
	} else {
		err = s.tasks.SetStandTip(ctx, task.ID, onto.SHA)
	}
	if err != nil {
		return err
	}

	pushed, err := s.pushRestack(ctx, task)
	if err != nil {
		return err
	}

	s.queueEvent(ctx, task.ID, restacked{
		TaskID: task.ID,
		Restacked: &eventprompts.Restacked{
			Parent: parent.Slug,
			Onto:   onto.Branch,
			SHA:    onto.SHA,
			Replay: plan.Replay,
			Merged: onto.Merged,
			Pushed: pushed,
		},
	})

	return nil
}

// isReadyToRestack reports whether the task's writer is at rest and its worktree clean, with no sequence part-way through.
func (s *Service) isReadyToRestack(ctx context.Context, task taskstore.Task) (bool, error) {
	writer, err := s.agents.GetTaskWriter(ctx, task.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return false, err
	}
	if writer != nil && !isAtRest(*writer) {
		return false, nil
	}

	state, err := s.git.ReadTreeState(ctx, task.Checkout.Cwd)
	if err != nil {
		return false, err
	}

	return refuseTreeState(state) == nil, nil
}

// isAtRest reports whether an agent is between turns with nobody holding it.
func isAtRest(agent agentstore.Agent) bool {
	if agent.Hold.IsHeld() || agent.State == agentstore.StateSettingUp {
		return false
	}
	if !agent.Alive {
		return true
	}

	return agent.State == agentstore.StateIdle && !agent.IsStateUnconfirmed()
}

// restackPlan is what a replay of a standing task's own commits would do.
type restackPlan struct {
	// Cutoff is the commit the replay starts after.
	Cutoff        string
	Replay        int
	ConflictPaths []string
}

// planRestack reads where a replay of a standing task's own commits onto a commit starts and how many it moves.
func (s *Service) planRestack(ctx context.Context, dir, tip, onto string) (*restackPlan, error) {
	prediction, err := s.git.PredictRebase(ctx, dir, onto)
	if err != nil {
		return nil, err
	}

	cutoff, err := s.readStandCutoff(ctx, dir, tip, prediction.Cutoff)
	if err != nil {
		return nil, err
	}

	replay, err := s.git.CountCommitsToReplay(ctx, dir, cutoff)
	if err != nil {
		return nil, err
	}

	return &restackPlan{Cutoff: cutoff, Replay: replay, ConflictPaths: prediction.ConflictPaths}, nil
}

// readStandCutoff is the commit a standing task's replay starts after: the recorded tip, or the
// predicted cutoff when that is one of the task's own commits after it.
func (s *Service) readStandCutoff(ctx context.Context, dir, tip, predicted string) (string, error) {
	if predicted == "" || predicted == tip {
		return tip, nil
	}

	newer, err := s.git.IsAncestor(ctx, git.IsAncestorParams{Dir: dir, MaybeAncestor: tip, Of: predicted})
	if err != nil {
		return "", err
	}
	if !newer {
		return tip, nil
	}

	return predicted, nil
}

// restackBackOut is a replay that conflicted, and where it was bound.
type restackBackOut struct {
	Onto restackOnto
	Plan restackPlan
}

// backOutRestack aborts a conflicted replay back to where the branch was and tells the writer the exact rebase to run.
func (s *Service) backOutRestack(ctx context.Context, task, parent taskstore.Task, params restackBackOut) error {
	if err := s.git.AbortSequence(ctx, task.Checkout.Cwd, git.SequenceRebase); err != nil {
		return err
	}

	if err := s.tasks.SetRestackBlocked(ctx, task.ID, taskstore.RestackConflicted); err != nil {
		return err
	}

	s.queueEvent(ctx, task.ID, restacked{
		TaskID: task.ID,
		Conflicted: &eventprompts.RestackConflicted{
			Parent: parent.Slug,
			Onto:   params.Onto.Branch,
			SHA:    params.Onto.SHA,
			Merged: params.Onto.Merged,
			Cutoff: params.Plan.Cutoff,
			Paths:  params.Plan.ConflictPaths,
		},
	})

	return nil
}

// pushRestack force-pushes a restacked branch origin already holds, reporting whether it did.
func (s *Service) pushRestack(ctx context.Context, task taskstore.Task) (bool, error) {
	branch := task.GetBranch()

	remote, err := s.git.CountAgainstRemote(ctx, task.Checkout.Cwd, branch)
	if err != nil || !remote.Pushed {
		return false, err
	}

	ctx = context.WithoutCancel(ctx)
	defer s.heat(task)

	if _, err := s.git.Push(ctx, git.PushParams{Dir: task.Checkout.Cwd, Branch: branch, Force: true}); err != nil {
		return false, err
	}

	return true, nil
}

// PlaceStandParams names a standing task and the commit its own commits sit on.
type PlaceStandParams struct {
	// Task is the standing task, by id or slug.
	Task   string
	Commit string
}

// PlaceStand records the commit a standing task's own commits sit on, which its branch must hold, and answers its full name.
func (s *Service) PlaceStand(ctx context.Context, params PlaceStandParams) (string, error) {
	target, err := s.eligible(ctx, params.Task)
	if err != nil {
		return "", err
	}
	if target.task.Stand == nil {
		return "", fmt.Errorf("%w: %q", errStandsOnNone, target.task.Slug)
	}

	sha, err := s.git.ResolveCommit(ctx, target.dir(), params.Commit)
	if err != nil {
		return "", err
	}

	held, err := s.git.HoldsCommit(ctx, target.dir(), sha)
	if err != nil {
		return "", err
	}
	if !held {
		return "", fmt.Errorf("%w: %s", errStandNotHeld, xsha.Short(sha))
	}

	if err := s.tasks.SetStandTip(ctx, target.task.ID, sha); err != nil {
		return "", err
	}
	s.checkoutSync.Wake()

	return sha, nil
}
