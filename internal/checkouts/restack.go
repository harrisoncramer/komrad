package checkouts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"komrad/internal/eventprompts"
	"komrad/internal/fail"
	"komrad/internal/git"
	"komrad/internal/logging"
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
	// errOntoUnmoved is a task whose own commits need not move.
	errOntoUnmoved = fail.Precondition("the task's own commits need not move")
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
	if !task.Checkout.IsUsable() {
		return nil
	}

	handled, err := s.settleMoveOntoBase(ctx, task)
	if err != nil || handled {
		return err
	}

	if task.Stand.Blocked == taskstore.RestackLost {
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
	if pr != nil && pr.MergedAt == nil && pr.ClosedAt != nil {
		return s.tellParentClosed(ctx, task, *parent, *pr)
	}

	onto, err := s.findOnto(ctx, task, *parent, pr)
	if errors.Is(err, errOntoUnmoved) {
		return nil
	}
	if err != nil {
		return err
	}

	return s.moveOnto(ctx, task, *parent, *onto)
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

	children, err := s.tasks.ListStandingOn(ctx, []string{task.ID})
	if err != nil {
		return false, err
	}

	for _, child := range children {
		if err := s.tasks.SetStand(ctx, child.ID, &taskstore.Stand{
			ParentID:      parent.ID,
			Tip:           child.Stand.Tip,
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

	repository, err := requireRepository(task)
	if err != nil {
		return nil, err
	}

	found, err := s.pullRequests.GetStandPullRequest(ctx, pullrequeststore.GetStandPullRequestParams{
		RepositoryURL: repository,
		HeadBranch:    parent.GetBranch(),
		Since:         task.CreatedAt,
	})
	if err != nil {
		return nil, err
	}

	if err := s.tasks.RecordStandPullRequest(ctx, task.ID, found.ID); err != nil {
		return nil, err
	}

	return found, nil
}

// readPullRequest is the newest pull request from a branch of the task's repository, store.ErrNotFound when there is none.
func (s *Service) readPullRequest(ctx context.Context, task taskstore.Task, branch string) (*pullrequeststore.PullRequest, error) {
	repository, err := requireRepository(task)
	if err != nil {
		return nil, err
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

// requireRepository is the address of the task's repository, store.ErrNotFound while it has none.
func requireRepository(task taskstore.Task) (string, error) {
	repository := xptr.Deref(task.Checkout.RepositoryURL)
	if repository == "" {
		return "", fmt.Errorf("%w: task %q has no repository", store.ErrNotFound, task.Slug)
	}

	return repository, nil
}

// findOnto is where the task's own commits belong now, errOntoUnmoved while they need not move.
func (s *Service) findOnto(ctx context.Context, task, parent taskstore.Task, pr *pullrequeststore.PullRequest) (*restackOnto, error) {
	dir := task.Checkout.Cwd

	if pr != nil && pr.MergedAt != nil {
		flow, err := s.workflows.Get(ctx, task.WorkflowID)
		if err != nil {
			return nil, err
		}

		_, sha, err := s.git.FetchBase(ctx, dir, flow.Worktree.BaseBranch)
		if err != nil {
			return nil, err
		}

		return &restackOnto{Branch: flow.Worktree.BaseBranch, SHA: sha, Merged: true}, nil
	}

	tip, err := s.git.FetchBranchTip(ctx, dir, parent.GetBranch())
	if errors.Is(err, git.ErrBranchNotOnOrigin) {
		return nil, errOntoUnmoved
	}
	if err != nil {
		return nil, err
	}
	if tip == task.Stand.Tip || (pr != nil && pr.HeadSHA != tip) {
		return nil, errOntoUnmoved
	}

	return &restackOnto{Branch: parent.GetBranch(), SHA: tip}, nil
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

	unlock, taken := s.locks.TryLock(dir)
	if !taken {
		return nil
	}
	defer unlock()

	ready, err := s.isReadyToRestack(ctx, task)
	if err != nil || !ready {
		return err
	}

	holdsOnto, err := s.git.HoldsCommit(ctx, dir, onto.SHA)
	if err != nil {
		return err
	}
	if holdsOnto {
		return s.recordOnto(ctx, task, onto)
	}

	if task.Stand.Blocked == taskstore.RestackConflicted || task.Checkout.AdoptedBranch != "" {
		return nil
	}

	holdsTip, err := s.git.HoldsCommit(ctx, dir, task.Stand.Tip)
	if err != nil {
		return err
	}
	if !holdsTip {
		return s.tellLost(ctx, task, parent)
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
		return s.backOutRestack(ctx, backOutRestackParams{Task: task, Parent: parent, Onto: onto, Plan: *plan})
	}
	if err != nil {
		return err
	}

	s.releaseOlderParks(ctx, task, park)

	if err := s.recordOnto(ctx, task, onto); err != nil {
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
			Pushed: s.pushRestack(ctx, task),
		},
	})

	return nil
}

// recordOnto records that the task's own commits sit where they belong, ending its stand once its parent merged.
func (s *Service) recordOnto(ctx context.Context, task taskstore.Task, onto restackOnto) error {
	if onto.Merged {
		return s.tasks.SetStand(ctx, task.ID, nil)
	}

	return s.tasks.SetStandTip(ctx, task.ID, onto.SHA)
}

// settleMoveOntoBase settles a move onto the base branch that stopped on conflicts once the writer finishes or
// aborts it, and reports whether the move claims this pass.
func (s *Service) settleMoveOntoBase(ctx context.Context, task taskstore.Task) (bool, error) {
	raw, err := s.tasks.GetLastRebase(ctx, task.ID)
	if err != nil || raw == nil {
		return false, err
	}

	var outcome rebaseOutcome
	if err := json.Unmarshal(raw, &outcome); err != nil {
		return false, fmt.Errorf("failed to read the last rebase of task %q: %w", task.Slug, err)
	}

	last := outcome.Result
	if last == nil || !last.OntoBase || !last.Conflicted || outcome.Settled {
		return false, nil
	}

	dir := task.Checkout.Cwd

	unlock, taken := s.locks.TryLock(dir)
	if !taken {
		return true, nil
	}
	defer unlock()

	ready, err := s.isReadyToRestack(ctx, task)
	if err != nil || !ready {
		return true, err
	}

	finished, err := s.hasLeftForBase(ctx, task, last.BaseSHA)
	if err != nil {
		return false, err
	}
	if finished {
		if err := s.tasks.SetStand(ctx, task.ID, nil); err != nil {
			return false, err
		}
	}

	outcome.Settled = true
	encoded, err := json.Marshal(outcome)
	if err != nil {
		return false, fmt.Errorf("failed to encode the last rebase of task %q: %w", task.Slug, err)
	}
	if err := s.tasks.SetLastRebase(ctx, task.ID, encoded); err != nil {
		return false, err
	}

	return finished, nil
}

// hasLeftForBase reports whether the task's branch dropped the commit of its parent it stood on and holds the base commit.
func (s *Service) hasLeftForBase(ctx context.Context, task taskstore.Task, base string) (bool, error) {
	holdsTip, err := s.git.HoldsCommit(ctx, task.Checkout.Cwd, task.Stand.Tip)
	if err != nil || holdsTip {
		return false, err
	}

	return s.git.HoldsCommit(ctx, task.Checkout.Cwd, base)
}

// tellLost marks the task as lost, since its branch no longer holds the commit of its parent it stood on, and tells its writer.
func (s *Service) tellLost(ctx context.Context, task, parent taskstore.Task) error {
	if err := s.tasks.SetRestackBlocked(ctx, task.ID, taskstore.RestackLost); err != nil {
		return err
	}

	s.queueEvent(ctx, task.ID, restacked{
		TaskID: task.ID,
		Lost:   &eventprompts.RestackLost{Parent: parent.Slug, Tip: task.Stand.Tip},
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

// readStandCutoff is the commit a standing task's replay starts after, the predicted cutoff when it
// is one of the task's own commits past the recorded tip and the recorded tip otherwise.
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

// backOutRestackParams is a replay that conflicted, and where it was bound.
type backOutRestackParams struct {
	Task   taskstore.Task
	Parent taskstore.Task
	Onto   restackOnto
	Plan   restackPlan
}

// backOutRestack aborts a conflicted replay back to where the branch was and tells the writer the exact rebase to run.
func (s *Service) backOutRestack(ctx context.Context, params backOutRestackParams) error {
	if err := s.git.AbortSequence(ctx, params.Task.Checkout.Cwd, git.SequenceRebase); err != nil {
		return err
	}

	if err := s.tasks.SetRestackBlocked(ctx, params.Task.ID, taskstore.RestackConflicted); err != nil {
		return err
	}

	s.queueEvent(ctx, params.Task.ID, restacked{
		TaskID: params.Task.ID,
		Conflicted: &eventprompts.RestackConflicted{
			Parent: params.Parent.Slug,
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
func (s *Service) pushRestack(ctx context.Context, task taskstore.Task) bool {
	branch := task.GetBranch()

	remote, err := s.git.CountAgainstRemote(ctx, task.Checkout.Cwd, branch)
	if err != nil {
		logPushRestackFailed(ctx, task, err)

		return false
	}
	if !remote.Pushed {
		return false
	}

	ctx = context.WithoutCancel(ctx)
	defer s.heat(task)

	if _, err := s.git.Push(ctx, git.PushParams{Dir: task.Checkout.Cwd, Branch: branch, Force: true}); err != nil {
		logPushRestackFailed(ctx, task, err)

		return false
	}

	return true
}

// logPushRestackFailed logs a restacked branch komrad could not push, which its writer is then told to push.
func logPushRestackFailed(ctx context.Context, task taskstore.Task, err error) {
	logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to push a restacked branch",
		append([]slog.Attr{slog.String(logging.TaskID, task.ID)}, logging.Err(err)...)...)
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
