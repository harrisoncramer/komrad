package tasks

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"komrad/internal/fail"
	"komrad/internal/store"
	pullrequeststore "komrad/internal/store/pullrequest"
	taskstore "komrad/internal/store/task"
	"komrad/lib/xptr"
)

var (
	// errStandNoWorktree is a task stood on that has no worktree to cut from.
	errStandNoWorktree = fail.Precondition("a task stands only on a task with a worktree on a branch of its own")
	// errStandOnBase is a task stood on whose checkout is on the workflow's base branch.
	errStandOnBase = fail.Precondition("this task is on the workflow's base branch, so start the new task without a parent")
	// ErrStandJoined is a launch onto a task already made that also names a task to stand on.
	ErrStandJoined = fail.InvalidArgument("only a new task stands on another, so leave the task to stand on unset when joining one")
	// errStoodOn is a delete of a task another undeleted task stands on.
	errStoodOn = fail.Precondition("another task stands on this one, so archive it instead, or delete the tasks standing on it first")
)

// standGit reads the tip of a branch on origin.
type standGit interface {
	FetchBranchTip(ctx context.Context, dir, branch string) (string, error)
}

// PlanStandParams names the workflow a new task is made in and the task it is to stand on.
type PlanStandParams struct {
	WorkflowID string
	// Parent is the task to stand on, by id or slug.
	Parent string
}

// PlanStand reads what a new task standing on another would record: the parent, its branch's
// tip on origin, and its open pull request.
func (s *TaskService) PlanStand(ctx context.Context, params PlanStandParams) (*taskstore.Stand, error) {
	parent, err := s.GetTaskForWorkflow(ctx, params.Parent, params.WorkflowID)
	if err != nil {
		return nil, err
	}

	branch := xptr.Deref(parent.Checkout.Branch)
	if !parent.Checkout.IsUsable() || branch == "" {
		return nil, errStandNoWorktree.Saying("task %q has no worktree on a branch", parent.Slug)
	}

	flow, err := s.workflows.Get(ctx, params.WorkflowID)
	if err != nil {
		return nil, err
	}
	if branch == flow.Worktree.BaseBranch {
		return nil, errStandOnBase.Saying("task %q is on %s", parent.Slug, branch)
	}

	tip, err := s.git.FetchBranchTip(ctx, parent.Checkout.Cwd, branch)
	if err != nil {
		return nil, fmt.Errorf("failed to read where %s stands on origin: %w", branch, err)
	}

	stand := &taskstore.Stand{ParentID: parent.ID, Tip: tip}

	pullRequest, err := s.pullRequests.FindStand(ctx, pullrequeststore.FindStandParams{
		RepositoryURL: xptr.Deref(parent.Checkout.RepositoryURL),
		HeadBranch:    branch,
		Since:         time.Now(),
	})
	switch {
	case err == nil:
		stand.PullRequestID = pullRequest.ID
	case !errors.Is(err, store.ErrNotFound):
		return nil, err
	}

	return stand, nil
}

// RefuseStoodOn turns down a delete of tasks that another undeleted task, outside the delete, stands on.
func (s *TaskService) RefuseStoodOn(ctx context.Context, ids []string) error {
	standing, err := s.tasks.ListStandingOn(ctx, ids)
	if err != nil {
		return err
	}

	for _, task := range standing {
		if !slices.Contains(ids, task.ID) {
			return fmt.Errorf("%w: %q stands on it", errStoodOn, task.Slug)
		}
	}

	return nil
}
