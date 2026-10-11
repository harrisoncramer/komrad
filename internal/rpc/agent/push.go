package agent

import (
	"context"

	"komrad/internal/checkouts"
	"komrad/internal/fail"
	"komrad/internal/rpcerr"
	agentv1 "komrad/proto/gen/agent/v1"
)

// worktreePusher sends a task's branch to origin.
type worktreePusher interface {
	PlanPush(ctx context.Context, task string) (*checkouts.PushPlan, error)
	Push(ctx context.Context, task string) (*checkouts.PushResult, error)
}

// PlanPush reads what a push would do without doing it.
func (s *Handler) PlanPush(ctx context.Context, req *agentv1.PlanPushRequest) (*agentv1.PlanPushResponse, *fail.Error) {
	plan, err := s.pusher.PlanPush(ctx, req.GetTask())
	if err != nil {
		return nil, rpcerr.From(err, "failed to work out what a push would do")
	}

	return &agentv1.PlanPushResponse{
		Branch:           plan.Branch,
		Commits:          int32(plan.Commits),
		Forced:           plan.Forced,
		UncommittedFiles: int32(plan.Uncommitted),
		InSync:           plan.InSync,
	}, nil
}

// PushWorktree sends the task's branch to origin.
func (s *Handler) PushWorktree(ctx context.Context, req *agentv1.PushWorktreeRequest) (*agentv1.PushWorktreeResponse, *fail.Error) {
	result, err := s.pusher.Push(ctx, req.GetTask())
	if err != nil {
		return nil, rpcerr.From(err, "failed to push the worktree")
	}

	return &agentv1.PushWorktreeResponse{
		Branch:  result.Branch,
		Commits: int32(result.Commits),
		Forced:  result.Forced,
		Sha:     result.SHA,
	}, nil
}
