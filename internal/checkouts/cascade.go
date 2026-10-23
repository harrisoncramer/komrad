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
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/oneofs"
	taskv1 "komrad/proto/gen/task/v1"

	"github.com/google/uuid"
)

var (
	// errNoParent is a fork point picked for a task without a parent.
	errNoParent = fail.Precondition("this task does not depend on another task")
	// errForkPointNotHeld is a fork point the task's branch does not hold.
	errForkPointNotHeld = fail.Precondition("the task's branch does not hold this commit, so it cannot be the fork point")
	// errOntoUnmoved is a task whose own commits need not move.
	errOntoUnmoved = fail.Precondition("the task's own commits need not move")
	// errForkPointLost is a child whose branch holds none of its parent's own commits.
	errForkPointLost = fail.Precondition("the task's branch holds none of its parent's own commits")
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
	return errors.Join(s.cascadeChildren(ctx, ""), s.retargetTasksLeftOnParentBranches(ctx))
}

// CascadeMerged moves each child of the pull request at the address, which has just merged, onto the base branch.
func (s *Service) CascadeMerged(ctx context.Context, address string) error {
	return s.cascadeChildren(ctx, address)
}

// cascadeChildren brings each child up to date, or only the children of the pull request at mergedURL when it is set.
func (s *Service) cascadeChildren(ctx context.Context, mergedURL string) error {
	children, err := s.tasks.ListChildren(ctx)
	if err != nil {
		return err
	}

	var errs []error
	for _, task := range children {
		if err := s.cascadeTask(ctx, task, mergedURL); err != nil {
			errs = append(errs, fmt.Errorf("failed to cascade task %q: %w", task.Slug, err))
		}
	}

	return errors.Join(errs...)
}

// cascadeTask brings one child up to date with its parent, treating the parent's pull request as merged
// when it is at mergedURL.
func (s *Service) cascadeTask(ctx context.Context, task taskstore.Task, mergedURL string) error {
	if !task.Checkout.IsUsable() {
		return nil
	}

	handled, err := s.settleRetarget(ctx, task)
	if err != nil || handled {
		return err
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
	if mergedURL != "" && (pr == nil || pr.URL != mergedURL) {
		return nil
	}
	parentMerged := pr != nil && (pr.MergedAt != nil || pr.URL == mergedURL)
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

	children, err := s.tasks.ListChildrenOf(ctx, []uuid.UUID{task.ID})
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
	if id := task.Parent.PullRequestID; id != nil {
		return s.pullRequests.GetByID(ctx, *id)
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
	if task.Parent.Blocked == taskv1.CascadeBlock_CASCADE_BLOCK_PARENT_CLOSED {
		return nil
	}

	if err := s.tasks.SetCascadeBlocked(ctx, task.ID, taskv1.CascadeBlock_CASCADE_BLOCK_PARENT_CLOSED); err != nil {
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

	changed, err := s.hasParentChanged(ctx, task)
	if err != nil || changed {
		return err
	}

	ready, err := s.isReadyToCascade(ctx, task)
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

	if task.Parent.Blocked == taskv1.CascadeBlock_CASCADE_BLOCK_CONFLICTED || task.Checkout.AdoptedBranch != "" {
		return nil
	}

	holdsForkPoint, err := s.git.HoldsCommit(ctx, dir, task.Parent.ForkPoint)
	if err != nil {
		return err
	}
	if !holdsForkPoint {
		forkPoint, err := s.findNewerForkPoint(ctx, task, onto)
		if errors.Is(err, errForkPointLost) {
			return s.tellOrphaned(ctx, task, parent)
		}
		if err != nil {
			return err
		}
		if err := s.tasks.SetForkPoint(ctx, task.ID, forkPoint); err != nil {
			return err
		}
		task.Parent.ForkPoint = forkPoint
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
	if err := s.recordOnto(ctx, task, onto); err != nil {
		return err
	}

	s.queueEvent(ctx, task.ID, cascaded{
		TaskID: task.ID,
		Rebased: &eventprompts.CascadeRebased{
			Parent:  parent.Slug,
			Onto:    onto.Branch,
			SHA:     onto.SHA,
			Commits: plan.Commits,
			Merged:  onto.Merged,
			Pushed:  pushed,
		},
	})

	return nil
}

// recordOnto records the child's new fork point, or leaves its parent once the parent merged.
func (s *Service) recordOnto(ctx context.Context, task taskstore.Task, onto cascadeOnto) error {
	if onto.Merged {
		return s.leaveParent(ctx, task)
	}

	return s.tasks.SetForkPoint(ctx, task.ID, onto.SHA)
}

// hasParentChanged reports whether the child's parent or fork point changed since the task was read.
func (s *Service) hasParentChanged(ctx context.Context, task taskstore.Task) (bool, error) {
	current, err := s.tasks.Get(ctx, task.ID)
	if err != nil {
		return false, err
	}

	return current.Parent == nil || current.Parent.TaskID != task.Parent.TaskID || current.Parent.ForkPoint != task.Parent.ForkPoint, nil
}

// leaveParent clears the child's parent and points its pull request off the parent's branch at the
// base branch.
func (s *Service) leaveParent(ctx context.Context, task taskstore.Task) error {
	if task.Parent == nil {
		return nil
	}

	parent, err := s.tasks.Get(ctx, task.Parent.TaskID)
	if errors.Is(err, store.ErrNotFound) {
		return s.tasks.ClearParent(ctx, task.ID, "")
	}
	if err != nil {
		return err
	}

	branch := parent.GetBranch()
	if err := s.tasks.ClearParent(ctx, task.ID, branch); err != nil {
		return err
	}

	if err := s.retarget(ctx, task, branch); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to point a task's pull request at the base branch",
			append([]slog.Attr{slog.String(logging.TaskID, task.ID.String())}, logging.Err(err)...)...)
	}

	return nil
}

// retargetTasksLeftOnParentBranches points the pull request of each task that left its parent and
// still targets the parent's branch at the base branch.
func (s *Service) retargetTasksLeftOnParentBranches(ctx context.Context) error {
	tasks, err := s.pullRequests.ListTasksLeftOnParentBranches(ctx)
	if err != nil {
		return err
	}

	var errs []error
	for _, left := range tasks {
		if err := s.retargetTask(ctx, left); err != nil {
			errs = append(errs, fmt.Errorf("failed to re-target task %q: %w", left.TaskID, err))
		}
	}

	return errors.Join(errs...)
}

// retargetTask points the task's pull request at the base branch while nothing else holds its worktree.
func (s *Service) retargetTask(ctx context.Context, left pullrequeststore.LeftParent) error {
	task, err := s.tasks.Get(ctx, left.TaskID)
	if err != nil {
		return err
	}
	if !task.Checkout.IsUsable() {
		return nil
	}

	unlock, taken := s.locks.TryLock(task.Checkout.Cwd)
	if !taken {
		return nil
	}
	defer unlock()

	return s.retarget(ctx, *task, left.Branch)
}

// retarget points the task's open pull request off the branch it left at the workflow's base branch
// once origin holds the task's branch as it stands, and tells its writer when the forge refuses.
func (s *Service) retarget(ctx context.Context, task taskstore.Task, from string) error {
	own, err := s.readPullRequest(ctx, task, task.GetBranch())
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if own.IsFinished() || own.BaseBranch != from {
		return s.tasks.ClearLeftParentBranch(ctx, task.ID)
	}

	flow, err := s.workflows.Get(ctx, task.WorkflowID)
	if err != nil {
		return err
	}
	base := flow.Worktree.BaseBranch

	remote, err := s.git.CountAgainstRemote(ctx, task.Checkout.Cwd, task.GetBranch())
	if err != nil {
		return err
	}
	if !remote.Pushed || remote.Unpushed > 0 || remote.Unpulled > 0 {
		return nil
	}

	host, err := s.setBase(ctx, own.URL, base)
	if err != nil {
		return s.tellRetargetRefused(ctx, task.ID, *own, eventprompts.RetargetRefused{URL: own.URL, Onto: base, Reason: err.Error()})
	}
	s.pullRequestSync.ReadOften(host)

	return s.tasks.ClearLeftParentBranch(ctx, task.ID)
}

// setBase points the pull request at the address at the base branch and says which host it lives on.
func (s *Service) setBase(ctx context.Context, address, base string) (string, error) {
	host, connection, err := s.forges.ForAddress(ctx, address)
	if err != nil {
		return "", err
	}

	if err := connection.SetBase(ctx, address, base); err != nil {
		return "", err
	}

	return host, nil
}

// tellRetargetRefused records that the forge refused to point the pull request at the base branch as
// it stands and tells the task's writer to point it themselves.
func (s *Service) tellRetargetRefused(ctx context.Context, taskID uuid.UUID, pr pullrequeststore.PullRequest, refused eventprompts.RetargetRefused) error {
	if err := s.pullRequests.RecordRetargetRefused(ctx, pr); err != nil {
		return err
	}

	s.queueEvent(ctx, taskID, cascaded{TaskID: taskID, Refused: &refused})

	return nil
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
	if !last.OntoBase || !last.Conflicted || outcome.Settled {
		return false, nil
	}

	dir := task.Checkout.Cwd

	unlock, taken := s.locks.TryLock(dir)
	if !taken {
		return true, nil
	}
	defer unlock()

	changed, err := s.hasParentChanged(ctx, task)
	if err != nil || changed {
		return true, err
	}

	ready, err := s.isReadyToCascade(ctx, task)
	if err != nil || !ready {
		return true, err
	}

	finished, err := s.hasLeftForBase(ctx, task, last.BaseSHA)
	if err != nil {
		return false, err
	}
	if finished {
		if err := s.leaveParent(ctx, task); err != nil {
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

// findNewerForkPoint is the newest of the parent's own commits the child's branch holds, errForkPointLost when it holds none.
func (s *Service) findNewerForkPoint(ctx context.Context, task taskstore.Task, onto cascadeOnto) (string, error) {
	if onto.Merged {
		return "", errForkPointLost
	}

	dir := task.Checkout.Cwd

	shared, err := s.git.ReadMergeBase(ctx, dir, onto.SHA)
	if errors.Is(err, git.ErrBaseUnrelated) {
		return "", errForkPointLost
	}
	if err != nil {
		return "", err
	}

	base, err := s.fetchBaseOnto(ctx, task)
	if err != nil {
		return "", err
	}

	landed, err := s.git.IsAncestor(ctx, git.IsAncestorParams{Dir: dir, MaybeAncestor: shared, Of: base.SHA})
	if err != nil {
		return "", err
	}
	if landed {
		return "", errForkPointLost
	}

	return shared, nil
}

// tellOrphaned marks a child whose branch holds neither its fork point nor any of its parent's own
// commits as orphaned and tells its writer, once.
func (s *Service) tellOrphaned(ctx context.Context, task, parent taskstore.Task) error {
	if task.Parent.Blocked == taskv1.CascadeBlock_CASCADE_BLOCK_ORPHANED {
		return nil
	}

	if err := s.tasks.SetCascadeBlocked(ctx, task.ID, taskv1.CascadeBlock_CASCADE_BLOCK_ORPHANED); err != nil {
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

// isAtRest reports whether an agent is between turns.
func isAtRest(agent agentstore.Agent) bool {
	if agent.State == agentv1.AgentState_AGENT_STATE_SETTING_UP {
		return false
	}
	if !agent.Alive {
		return true
	}

	return agent.State == agentv1.AgentState_AGENT_STATE_IDLE && !agent.IsStateUnconfirmed()
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
	if err := s.git.AbortSequence(ctx, params.Task.Checkout.Cwd, taskv1.Sequence_SEQUENCE_REBASE); err != nil {
		return err
	}

	if err := s.tasks.SetCascadeBlocked(ctx, params.Task.ID, taskv1.CascadeBlock_CASCADE_BLOCK_CONFLICTED); err != nil {
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
		append([]slog.Attr{slog.String(logging.TaskID, task.ID.String())}, logging.Err(err)...)...)
}

// PickForkPointParams names a child and its new fork point.
type PickForkPointParams struct {
	// Task names the child.
	Task   oneofs.Task
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
	s.cascade.Wake()

	return sha, nil
}
