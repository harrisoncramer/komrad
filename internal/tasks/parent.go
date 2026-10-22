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
	"komrad/proto/gen/oneofs"

	"github.com/google/uuid"
)

var (
	// errParentNoWorktree is a parent with no worktree on a branch of its own.
	errParentNoWorktree = fail.Precondition("a task can only depend on a task with its own worktree")
	// errParentOnBase is a parent whose checkout is on the workflow's base branch.
	errParentOnBase = fail.Precondition("that task works on the workflow's base branch, so start the new task without a parent")
	// ErrDependsOnJoined is a launch that joins an existing task and also names a parent.
	ErrDependsOnJoined = fail.InvalidArgument("only a new task can depend on another, so leave depends_on unset when joining a task")
	// errHasChildren is a delete of a task that still has undeleted children.
	errHasChildren = fail.Precondition("other tasks depend on this task, so archive it, or delete those tasks first")
)

// branchTipFetcher reads the newest commit of a branch on origin.
type branchTipFetcher interface {
	FetchBranchTip(ctx context.Context, dir, branch string) (string, error)
}

// PlanParentParams names the workflow a new task is made in and the task it depends on.
type PlanParentParams struct {
	WorkflowID uuid.UUID
	// Parent names the task the new task depends on.
	Parent oneofs.Task
}

// PlanParent reads the parent a new task would record: the parent task, the newest commit of its
// branch on origin as the fork point, and its open pull request.
func (s *TaskService) PlanParent(ctx context.Context, params PlanParentParams) (*taskstore.Parent, error) {
	parent, err := s.GetTaskForWorkflow(ctx, params.Parent, params.WorkflowID)
	if err != nil {
		return nil, err
	}

	branch := xptr.Deref(parent.Checkout.Branch)
	if !parent.Checkout.IsUsable() || branch == "" {
		return nil, errParentNoWorktree.Saying("task %q has no worktree on a branch", parent.Slug)
	}

	flow, err := s.workflows.Get(ctx, params.WorkflowID)
	if err != nil {
		return nil, err
	}
	if branch == flow.Worktree.BaseBranch {
		return nil, errParentOnBase.Saying("task %q is on %s", parent.Slug, branch)
	}

	forkPoint, err := s.git.FetchBranchTip(ctx, parent.Checkout.Cwd, branch)
	if err != nil {
		return nil, fmt.Errorf("failed to read the newest commit of %s on origin: %w", branch, err)
	}

	planned := &taskstore.Parent{TaskID: parent.ID, ForkPoint: forkPoint}

	pullRequest, err := s.pullRequests.GetParentPullRequest(ctx, pullrequeststore.GetParentPullRequestParams{
		RepositoryURL: xptr.Deref(parent.Checkout.RepositoryURL),
		HeadBranch:    branch,
		Since:         time.Now(),
	})
	switch {
	case err == nil:
		planned.PullRequestID = &pullRequest.ID
	case !errors.Is(err, store.ErrNotFound):
		return nil, err
	}

	return planned, nil
}

// RefuseParentDelete turns down a delete of tasks with an undeleted child outside the delete.
func (s *TaskService) RefuseParentDelete(ctx context.Context, ids []uuid.UUID) error {
	children, err := s.tasks.ListChildrenOf(ctx, ids)
	if err != nil {
		return err
	}

	for _, child := range children {
		if !slices.Contains(ids, child.ID) {
			return fmt.Errorf("%w: %q depends on it", errHasChildren, child.Slug)
		}
	}

	return nil
}
