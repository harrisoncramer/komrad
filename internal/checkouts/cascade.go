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
	// errNoParent is a fork point picked for a task without a parent.
	errNoParent = fail.Precondition("this task does not depend on another task")
	// errForkPointNotHeld is a fork point the task's branch does not hold.
	errForkPointNotHeld = fail.Precondition("the task's branch does not hold this commit, so it cannot be the fork point")
	// errOntoUnmoved is a task whose own commits need not move.
	errOntoUnmoved = fail.Precondition("the task's own commits need not move")
)

// cascadeOnto is the commit the cascade rebases a child's own commits onto.
type cascadeOnto struct {
	// Branch is the parent's branch, or the workflow's base branch once the parent merged.
	Branch string
	SHA    string
	Merged bool
}

// Cascade is one pass over every child, rebasing each whose parent moved onto the parent's newest commit.
func (s *Service) Cascade(ctx context.Context) error {
	return s.cascadeChildren(ctx, "")
}

// CascadeMerged moves each child of the pull request at the address, which has just merged, onto the base branch.
func (s *Service) CascadeMerged(ctx context.Context, address string) error {
	return s.cascadeChildren(ctx, address)
}

// cascadeChildren brings each child up to date, only those whose parent pull request is at merged when it is set.
func (s *Service) cascadeChildren(ctx context.Context, merged string) error {
	children, err := s.tasks.ListChildren(ctx)
	if err != nil {
		return err
	}

	var errs []error
	for _, task := range children {
		if err := s.cascadeTask(ctx, task, merged); err != nil {
			errs = append(errs, fmt.Errorf("failed to cascade task %q: %w", task.Slug, err))
		}
	}

	return errors.Join(errs...)
}

// cascadeTask brings one child up to date with its parent, taking the parent pull request at merged,
// when it is set, as merged and skipping a child whose parent pull request is another.
func (s *Service) cascadeTask(ctx context.Context, task taskstore.Task, merged string) error {
	if !task.Checkout.IsUsable() {
		return nil
	}

	handled, err := s.settleRetarget(ctx, task)
	if err != nil || handled {
		return err
	}

	if task.Parent.Blocked == taskstore.CascadeOrphaned {
		return nil
	}

	parent, err := s.tasks.Get(ctx, task.Parent.TaskID)
	if err != nil {
		return err
	}

	settled, err := s.settleMergedIntoParent(ctx, task, *parent)
	if err != nil || settled {
		return err
	}

	pr, err := s.readParentPullRequest(ctx, task, *parent)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if merged != "" && (pr == nil || pr.URL != merged) {
		return nil
	}
	parentMerged := pr != nil && (pr.MergedAt != nil || pr.URL == merged)
	if pr != nil && !parentMerged && pr.ClosedAt != nil {
		return s.tellParentClosed(ctx, task, *parent, *pr)
	}

	var onto *cascadeOnto
	if parentMerged {
		onto, err = s.fetchBaseOnto(ctx, task)
	} else {
		onto, err = s.findOnto(ctx, task, *parent, pr)
	}
	if errors.Is(err, errOntoUnmoved) {
		return nil
	}
	if err != nil {
		return err
	}

	return s.moveOnto(ctx, task, *parent, *onto)
}

// settleMergedIntoParent clears the parent of a child whose own pull request merged into its
// parent's branch, hands the child's children to that parent, and reports whether it did.
func (s *Service) settleMergedIntoParent(ctx context.Context, task, parent taskstore.Task) (bool, error) {
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

	children, err := s.tasks.ListChildrenOf(ctx, []string{task.ID})
	if err != nil {
		return false, err
	}

	for _, child := range children {
		if err := s.tasks.SetParent(ctx, child.ID, &taskstore.Parent{
			TaskID:        parent.ID,
			ForkPoint:     child.Parent.ForkPoint,
			PullRequestID: task.Parent.PullRequestID,
		}); err != nil {
			return false, err
		}
	}

	return true, s.tasks.SetParent(ctx, task.ID, nil)
}

// readParentPullRequest is the child's parent pull request, recording the first one opened from the
// parent's branch since the child began while none is recorded, and store.ErrNotFound while there is none.
func (s *Service) readParentPullRequest(ctx context.Context, task, parent taskstore.Task) (*pullrequeststore.PullRequest, error) {
	if id := task.Parent.PullRequestID; id != "" {
		return s.pullRequests.GetByID(ctx, id)
	}

	repository, err := requireRepository(task)
	if err != nil {
		return nil, err
	}

	found, err := s.pullRequests.GetParentPullRequest(ctx, pullrequeststore.GetParentPullRequestParams{
		RepositoryURL: repository,
		HeadBranch:    parent.GetBranch(),
		Since:         task.CreatedAt,
	})
	if err != nil {
		return nil, err
	}

	if err := s.tasks.RecordParentPullRequest(ctx, task.ID, found.ID); err != nil {
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

// findOnto is the parent's newest commit the child's own commits belong on, errOntoUnmoved while they need not move.
func (s *Service) findOnto(ctx context.Context, task, parent taskstore.Task, pr *pullrequeststore.PullRequest) (*cascadeOnto, error) {
	newest, err := s.git.FetchBranchTip(ctx, task.Checkout.Cwd, parent.GetBranch())
	if errors.Is(err, git.ErrBranchNotOnOrigin) {
		return nil, errOntoUnmoved
	}
	if err != nil {
		return nil, err
	}
	if newest == task.Parent.ForkPoint || (pr != nil && pr.HeadSHA != newest) {
		return nil, errOntoUnmoved
	}

	return &cascadeOnto{Branch: parent.GetBranch(), SHA: newest}, nil
}

// fetchBaseOnto is the newest commit of the workflow's base branch, where a child belongs once its parent merged.
func (s *Service) fetchBaseOnto(ctx context.Context, task taskstore.Task) (*cascadeOnto, error) {
	flow, err := s.workflows.Get(ctx, task.WorkflowID)
	if err != nil {
		return nil, err
	}

	_, sha, err := s.git.FetchBase(ctx, task.Checkout.Cwd, flow.Worktree.BaseBranch)
	if err != nil {
		return nil, err
	}

	return &cascadeOnto{Branch: flow.Worktree.BaseBranch, SHA: sha, Merged: true}, nil
}

// tellParentClosed blocks the child's cascade on its closed parent pull request and tells its writer, once.
func (s *Service) tellParentClosed(ctx context.Context, task, parent taskstore.Task, pr pullrequeststore.PullRequest) error {
	if task.Parent.Blocked == taskstore.CascadeParentClosed {
		return nil
	}

	if err := s.tasks.SetCascadeBlocked(ctx, task.ID, taskstore.CascadeParentClosed); err != nil {
		return err
	}

	s.queueEvent(ctx, task.ID, cascaded{
		TaskID: task.ID,
		Closed: &eventprompts.ParentClosed{Parent: parent.Slug, URL: pr.URL},
	})

	return nil
}

// moveOnto rebases the child's own commits after its fork point onto where they belong, while its
// writer is at rest and its worktree holds nothing uncommitted.
func (s *Service) moveOnto(ctx context.Context, task, parent taskstore.Task, onto cascadeOnto) error {
	dir := task.Checkout.Cwd

	unlock, taken := s.locks.TryLock(dir)
	if !taken {
		return nil
	}
	defer unlock()

	ready, err := s.isReadyToCascade(ctx, task)
	if err != nil || !ready {
		return err
	}

	holdsOnto, err := s.git.HoldsCommit(ctx, dir, onto.SHA)
	if err != nil {
		return err
	}
	if holdsOnto {
		_, err := s.recordOnto(ctx, task, onto)

		return err
	}

	if task.Parent.Blocked == taskstore.CascadeConflicted || task.Checkout.AdoptedBranch != "" {
		return nil
	}

	holdsForkPoint, err := s.git.HoldsCommit(ctx, dir, task.Parent.ForkPoint)
	if err != nil {
		return err
	}
	if !holdsForkPoint {
		return s.tellOrphaned(ctx, task, parent)
	}

	plan, err := s.planCascade(ctx, dir, task.Parent.ForkPoint, onto.SHA)
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
		return s.backOutCascade(ctx, backOutCascadeParams{Task: task, Parent: parent, Onto: onto, Plan: *plan})
	}
	if err != nil {
		return err
	}

	s.releaseOlderParks(ctx, task, park)

	pushed := s.pushCascade(ctx, task)
	retargeted, err := s.recordOnto(ctx, task, onto)
	if err != nil {
		return err
	}

	s.queueEvent(ctx, task.ID, cascaded{
		TaskID: task.ID,
		Rebased: &eventprompts.CascadeRebased{
			Parent:     parent.Slug,
			Onto:       onto.Branch,
			SHA:        onto.SHA,
			Commits:    plan.Commits,
			Merged:     onto.Merged,
			Pushed:     pushed,
			Retargeted: retargeted,
		},
	})

	return nil
}

// recordOnto records the child's new fork point, or leaves its parent once the parent merged, reporting
// whether the child's pull request targets where its commits now sit.
func (s *Service) recordOnto(ctx context.Context, task taskstore.Task, onto cascadeOnto) (bool, error) {
	if onto.Merged {
		return s.leaveParent(ctx, task, onto.Branch)
	}

	return true, s.tasks.SetForkPoint(ctx, task.ID, onto.SHA)
}

// leaveParent points the child's open pull request at the base branch and clears its parent, reporting
// whether the pull request, when it has one, now targets the base branch.
func (s *Service) leaveParent(ctx context.Context, task taskstore.Task, base string) (bool, error) {
	retargeted, err := s.retarget(ctx, task, base)
	if err != nil {
		return false, err
	}

	return retargeted, s.tasks.SetParent(ctx, task.ID, nil)
}

// retarget points the child's open pull request at the base branch through its forge, reporting whether
// it now targets it, and logs a forge that refused.
func (s *Service) retarget(ctx context.Context, task taskstore.Task, base string) (bool, error) {
	own, err := s.readPullRequest(ctx, task, task.GetBranch())
	if errors.Is(err, store.ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if own.IsFinished() || own.BaseBranch == base {
		return true, nil
	}

	host, f, err := s.forges.ForAddress(ctx, own.URL)
	if err == nil {
		err = f.SetBase(ctx, own.URL, base)
	}
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to point a child's pull request at the base branch",
			append([]slog.Attr{slog.String(logging.TaskID, task.ID)}, logging.Err(err)...)...)

		return false, nil
	}
	s.pullRequestSync.ReadOften(host)

	return true, nil
}

// settleRetarget settles a re-target onto the base branch that stopped on conflicts once the writer
// finishes or aborts it, and reports whether the re-target claims this pass.
func (s *Service) settleRetarget(ctx context.Context, task taskstore.Task) (bool, error) {
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

	ready, err := s.isReadyToCascade(ctx, task)
	if err != nil || !ready {
		return true, err
	}

	finished, err := s.hasLeftForBase(ctx, task, last.BaseSHA)
	if err != nil {
		return false, err
	}
	if finished {
		flow, err := s.workflows.Get(ctx, task.WorkflowID)
		if err != nil {
			return false, err
		}
		if _, err := s.leaveParent(ctx, task, flow.Worktree.BaseBranch); err != nil {
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

// hasLeftForBase reports whether the child's branch dropped its fork point and holds the base commit.
func (s *Service) hasLeftForBase(ctx context.Context, task taskstore.Task, base string) (bool, error) {
	holdsForkPoint, err := s.git.HoldsCommit(ctx, task.Checkout.Cwd, task.Parent.ForkPoint)
	if err != nil || holdsForkPoint {
		return false, err
	}

	return s.git.HoldsCommit(ctx, task.Checkout.Cwd, base)
}

// tellOrphaned marks a child whose branch no longer holds its fork point as orphaned and tells its writer.
func (s *Service) tellOrphaned(ctx context.Context, task, parent taskstore.Task) error {
	if err := s.tasks.SetCascadeBlocked(ctx, task.ID, taskstore.CascadeOrphaned); err != nil {
		return err
	}

	s.queueEvent(ctx, task.ID, cascaded{
		TaskID:   task.ID,
		Orphaned: &eventprompts.CascadeOrphaned{Parent: parent.Slug, ForkPoint: task.Parent.ForkPoint},
	})

	return nil
}

// isReadyToCascade reports whether the task's writer is at rest and its worktree clean, with no sequence part-way through.
func (s *Service) isReadyToCascade(ctx context.Context, task taskstore.Task) (bool, error) {
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

// cascadePlan is what a rebase of a child's own commits would do.
type cascadePlan struct {
	// Cutoff is the commit the rebase starts after.
	Cutoff string
	// Commits is how many of the child's own commits the rebase moves.
	Commits       int
	ConflictPaths []string
}

// planCascade reads where a rebase of a child's own commits onto a commit starts and how many it moves.
func (s *Service) planCascade(ctx context.Context, dir, forkPoint, onto string) (*cascadePlan, error) {
	prediction, err := s.git.PredictRebase(ctx, dir, onto)
	if err != nil {
		return nil, err
	}

	cutoff, err := s.readForkPointCutoff(ctx, dir, forkPoint, prediction.Cutoff)
	if err != nil {
		return nil, err
	}

	commits, err := s.git.CountCommitsToReplay(ctx, dir, cutoff)
	if err != nil {
		return nil, err
	}

	return &cascadePlan{Cutoff: cutoff, Commits: commits, ConflictPaths: prediction.ConflictPaths}, nil
}

// readForkPointCutoff is the commit a child's rebase starts after: the predicted cutoff when it is one
// of the child's own commits past the fork point, and the fork point otherwise.
func (s *Service) readForkPointCutoff(ctx context.Context, dir, forkPoint, predicted string) (string, error) {
	if predicted == "" || predicted == forkPoint {
		return forkPoint, nil
	}

	newer, err := s.git.IsAncestor(ctx, git.IsAncestorParams{Dir: dir, MaybeAncestor: forkPoint, Of: predicted})
	if err != nil {
		return "", err
	}
	if !newer {
		return forkPoint, nil
	}

	return predicted, nil
}

// backOutCascadeParams is a cascade rebase that conflicted, and the commit it was rebasing onto.
type backOutCascadeParams struct {
	Task   taskstore.Task
	Parent taskstore.Task
	Onto   cascadeOnto
	Plan   cascadePlan
}

// backOutCascade aborts a conflicted rebase back to where the branch was and tells the writer the exact rebase to run.
func (s *Service) backOutCascade(ctx context.Context, params backOutCascadeParams) error {
	if err := s.git.AbortSequence(ctx, params.Task.Checkout.Cwd, git.SequenceRebase); err != nil {
		return err
	}

	if err := s.tasks.SetCascadeBlocked(ctx, params.Task.ID, taskstore.CascadeConflicted); err != nil {
		return err
	}

	s.queueEvent(ctx, params.Task.ID, cascaded{
		TaskID: params.Task.ID,
		Conflicted: &eventprompts.CascadeConflicted{
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

// pushCascade force-pushes a rebased child's branch that origin already holds, reporting whether it did.
func (s *Service) pushCascade(ctx context.Context, task taskstore.Task) bool {
	branch := task.GetBranch()

	remote, err := s.git.CountAgainstRemote(ctx, task.Checkout.Cwd, branch)
	if err != nil {
		logPushCascadeFailed(ctx, task, err)

		return false
	}
	if !remote.Pushed {
		return false
	}

	ctx = context.WithoutCancel(ctx)
	defer s.readOften(task)

	if _, err := s.git.Push(ctx, git.PushParams{Dir: task.Checkout.Cwd, Branch: branch, Force: true}); err != nil {
		logPushCascadeFailed(ctx, task, err)

		return false
	}

	return true
}

// logPushCascadeFailed logs a rebased child's branch komrad could not push, which its writer is then told to push.
func logPushCascadeFailed(ctx context.Context, task taskstore.Task, err error) {
	logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to push a child's rebased branch",
		append([]slog.Attr{slog.String(logging.TaskID, task.ID)}, logging.Err(err)...)...)
}

// PickForkPointParams names a child and its new fork point.
type PickForkPointParams struct {
	// Task is the child, by id or slug.
	Task   string
	Commit string
}

// PickForkPoint records a child's fork point, which its branch must hold, and answers the full commit.
func (s *Service) PickForkPoint(ctx context.Context, params PickForkPointParams) (string, error) {
	target, err := s.eligible(ctx, params.Task)
	if err != nil {
		return "", err
	}
	if target.task.Parent == nil {
		return "", fmt.Errorf("%w: %q", errNoParent, target.task.Slug)
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
		return "", fmt.Errorf("%w: %s", errForkPointNotHeld, xsha.Short(sha))
	}

	if err := s.tasks.SetForkPoint(ctx, target.task.ID, sha); err != nil {
		return "", err
	}
	s.checkoutSync.Wake()

	return sha, nil
}
