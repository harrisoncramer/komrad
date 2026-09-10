package agent

import (
	"context"
	"errors"

	"weave/internal/agents"
	"weave/internal/rpcerr"
	agentv1 "weave/proto/gen/agent/v1"

	"connectrpc.com/connect"
)

// ErrResetUnimplemented stands in for the destructive half of a refresh, which is not built.
var ErrResetUnimplemented = errors.New("resetting a worktree is not implemented yet, only rebasing is")

// worktreeRefresher brings an agent's existing worktree up to date in place.
type worktreeRefresher interface {
	PlanRebase(ctx context.Context, agentID string) (*agents.RebasePlan, error)
	Rebase(ctx context.Context, agentID string) (*agents.RebaseResult, error)
}

// PlanRefresh reads what a rebase would do without doing it.
func (s *Service) PlanRefresh(
	ctx context.Context,
	req *connect.Request[agentv1.PlanRefreshRequest],
) (*connect.Response[agentv1.PlanRefreshResponse], error) {
	plan, err := s.refresher.PlanRebase(ctx, req.Msg.GetAgentId())
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to work out what a rebase would do")
	}

	return connect.NewResponse(&agentv1.PlanRefreshResponse{
		BaseRef:       plan.BaseRef,
		BaseSha:       plan.BaseSHA,
		Commits:       int32(plan.Commits),
		Branch:        plan.Branch,
		LandedCommits: int32(plan.Landed),
		ReplayCommits: int32(plan.Replay),
		Merges:        int32(plan.Merges),
		ConflictPaths: plan.ConflictPaths,
	}), nil
}

// RebaseWorktree moves the agent's branch onto the latest base in place.
func (s *Service) RebaseWorktree(
	ctx context.Context,
	req *connect.Request[agentv1.RebaseWorktreeRequest],
) (*connect.Response[agentv1.RebaseWorktreeResponse], error) {
	result, err := s.refresher.Rebase(ctx, req.Msg.GetAgentId())
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to rebase the worktree")
	}

	return connect.NewResponse(&agentv1.RebaseWorktreeResponse{
		BaseRef:    result.BaseRef,
		BaseSha:    result.BaseSHA,
		Commits:    int32(result.Commits),
		Branch:     result.Branch,
		Conflicted: result.Conflicted,
		Moved:      result.Moved,
	}), nil
}

// ResetWorktree is the destructive half, which is designed in rfcs/0042 and not yet built.
func (s *Service) ResetWorktree(
	_ context.Context,
	_ *connect.Request[agentv1.ResetWorktreeRequest],
) (*connect.Response[agentv1.ResetWorktreeResponse], error) {
	return nil, rpcerr.Wrap(ErrResetUnimplemented, "failed to reset the worktree")
}
