package checkouts

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"komrad/internal/eventprompts"
	"komrad/internal/forge"
	"komrad/internal/git"
	"komrad/internal/logging"
	"komrad/internal/store"
	pullrequeststore "komrad/internal/store/pullrequest"
	taskstore "komrad/internal/store/task"
)

// rebaseAwaitWindow is how long the cascade waits on GitHub to rebase a child in a GitHub stack.
const rebaseAwaitWindow = time.Minute

// followStackParams is a child whose own pull request is open, its parent, and the parent's pull request, nil while it has none.
type followStackParams struct {
	Task     taskstore.Task
	Parent   taskstore.Task
	ParentPR *pullrequeststore.PullRequest
	Own      pullrequeststore.PullRequest
}

// followStack leaves a child in a GitHub stack to GitHub while GitHub rebases it, moves its worktree once
// GitHub has, and reports whether that claims this pass.
func (s *Service) followStack(ctx context.Context, params followStackParams) (bool, error) {
	own := params.Own
	if own.RebaseAwait == nil {
		return s.isAwaitingBaseChange(params), nil
	}

	if own.HeadSHA != own.RebaseAwait.From {
		return true, s.adoptStackRebase(ctx, params)
	}

	conflicting := own.MergeReadiness.Mergeability.Value == forge.MergeabilityConflicting
	if !conflicting && s.now().Before(own.RebaseAwait.Since.Add(rebaseAwaitWindow)) {
		return true, nil
	}

	return false, s.pullRequests.ClearRebaseAwait(ctx, own.ID)
}

// isAwaitingBaseChange says whether the child's pull request is in a GitHub stack whose parent merged
// a moment ago, before GitHub changed the child's base branch.
func (s *Service) isAwaitingBaseChange(params followStackParams) bool {
	merged := params.ParentPR != nil && params.ParentPR.MergedAt != nil
	if params.Own.Stack == nil || !merged || params.Own.BaseBranch != params.Parent.GetBranch() {
		return false
	}

	return s.now().Before(params.ParentPR.MergedAt.Add(rebaseAwaitWindow))
}

// adoptStackRebase moves the child's worktree onto the branch GitHub rebased, replaying the writer's
// unpushed commits on top, while its writer is at rest and its worktree holds nothing uncommitted.
func (s *Service) adoptStackRebase(ctx context.Context, params followStackParams) error {
	task := params.Task
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

	head, err := s.git.FetchBranchTip(ctx, dir, task.GetBranch())
	if err != nil {
		return err
	}

	onto, err := s.readStackOnto(ctx, task, params)
	if err != nil {
		return err
	}

	moved, err := s.replayUnpushed(ctx, replayUnpushedParams{Task: task, Parent: params.Parent, Own: params.Own, Onto: *onto, Head: head})
	if err != nil || !moved {
		return err
	}

	if err := s.recordStackOnto(ctx, task, *onto); err != nil {
		return err
	}

	commits, err := s.git.CountCommitsToReplay(ctx, dir, onto.SHA)
	if err != nil {
		return err
	}

	s.queueEvent(ctx, task.ID, cascaded{
		TaskID: task.ID,
		StackRebased: &eventprompts.StackRebased{
			Parent:  params.Parent.Slug,
			Onto:    onto.Branch,
			SHA:     onto.SHA,
			Commits: commits,
			Merged:  onto.Merged,
		},
	})

	return s.pullRequests.ClearRebaseAwait(ctx, params.Own.ID)
}

// readStackOnto is where GitHub rebased the child onto: the base branch once the parent merged, and the
// parent's newest commit otherwise.
func (s *Service) readStackOnto(ctx context.Context, task taskstore.Task, params followStackParams) (*cascadeOnto, error) {
	if params.ParentPR != nil && params.ParentPR.MergedAt != nil {
		return s.fetchBaseOnto(ctx, task)
	}

	newest, err := s.git.FetchBranchTip(ctx, task.Checkout.Cwd, params.Parent.GetBranch())
	if err != nil {
		return nil, err
	}

	return &cascadeOnto{Branch: params.Parent.GetBranch(), SHA: newest}, nil
}

// replayUnpushedParams is a child and its pull request waiting on GitHub, where GitHub rebased it onto, and the head GitHub pushed.
type replayUnpushedParams struct {
	Task   taskstore.Task
	Parent taskstore.Task
	Own    pullrequeststore.PullRequest
	Onto   cascadeOnto
	Head   string
}

// replayUnpushed moves the child's branch to the head GitHub pushed with the writer's unpushed commits
// on top, and reports whether it did.
func (s *Service) replayUnpushed(ctx context.Context, params replayUnpushedParams) (bool, error) {
	task := params.Task
	dir := task.Checkout.Cwd

	holdsHead, err := s.git.HoldsCommit(ctx, dir, params.Head)
	if err != nil || holdsHead {
		return holdsHead, err
	}

	pushed := params.Own.RebaseAwait.From
	holdsPushed, err := s.git.HoldsCommit(ctx, dir, pushed)
	if err != nil {
		return false, err
	}
	if !holdsPushed {
		logging.From(ctx).LogAttrs(ctx, slog.LevelInfo, "a child's branch no longer holds what it pushed, so the cascade rebases it",
			slog.String(logging.TaskID, task.ID))

		return false, s.pullRequests.ClearRebaseAwait(ctx, params.Own.ID)
	}

	park := buildParkRef(parkRebase, task.ID)
	if _, err := s.git.Park(ctx, dir, park); err != nil {
		return false, err
	}
	defer s.checkoutSync.Wake()

	err = s.git.RebaseOnto(ctx, dir, params.Head, pushed)
	if errors.Is(err, git.ErrRebaseConflicted) {
		onto := params.Onto
		onto.SHA = params.Head

		if err := s.backOutCascade(ctx, backOutCascadeParams{
			Task:   task,
			Parent: params.Parent,
			Onto:   onto,
			Plan:   cascadePlan{Cutoff: pushed},
		}); err != nil {
			return false, err
		}

		return false, s.pullRequests.ClearRebaseAwait(ctx, params.Own.ID)
	}
	if err != nil {
		return false, err
	}

	s.releaseOlderParks(ctx, task, park)

	return true, nil
}

// recordStackOnto records where GitHub rebased the child onto, once the child's branch holds it or its parent merged.
func (s *Service) recordStackOnto(ctx context.Context, task taskstore.Task, onto cascadeOnto) error {
	if !onto.Merged {
		held, err := s.git.HoldsCommit(ctx, task.Checkout.Cwd, onto.SHA)
		if err != nil || !held {
			return err
		}
	}

	return s.recordOnto(ctx, task, onto)
}

// retargetPullRequest takes a re-targeted child's pull request out of its GitHub stack and points it at
// the workflow's base branch.
func (s *Service) retargetPullRequest(ctx context.Context, target checkoutTarget) {
	own, err := s.readPullRequest(ctx, *target.task, target.task.GetBranch())
	if errors.Is(err, store.ErrNotFound) {
		return
	}
	if err == nil && (own.Stack == nil || own.IsFinished()) {
		return
	}
	if err == nil {
		err = s.retargeter.RetargetPullRequest(ctx, own.URL, target.workflow.Worktree.BaseBranch)
	}
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to point a re-targeted child's pull request at the base branch",
			append([]slog.Attr{slog.String(logging.TaskID, target.task.ID)}, logging.Err(err)...)...)
	}
}
