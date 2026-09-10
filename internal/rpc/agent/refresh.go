package agent

import (
	"context"

	"weave/internal/agents"
	"weave/internal/rpcerr"
	agentv1 "weave/proto/gen/agent/v1"

	"connectrpc.com/connect"
)

// worktreeRefresher brings an agent's existing worktree up to date in place.
type worktreeRefresher interface {
	PlanRebase(ctx context.Context, agentID string) (*agents.RebasePlan, error)
	Rebase(ctx context.Context, agentID string) (*agents.RebaseResult, error)
	Reset(ctx context.Context, agentID string) (*agents.ResetResult, error)
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
		BaseRef:        plan.BaseRef,
		BaseSha:        plan.BaseSHA,
		Commits:        int32(plan.Commits),
		Branch:         plan.Branch,
		TrackedChanges: plan.TrackedChanges,
		LandedCommits:  int32(plan.Landed),
		ReplayCommits:  int32(plan.Replay),
		Merges:         int32(plan.Merges),
		ConflictPaths:  plan.ConflictPaths,
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
		BaseRef:       result.BaseRef,
		BaseSha:       result.BaseSHA,
		Commits:       int32(result.Commits),
		Branch:        result.Branch,
		Conflicted:    result.Conflicted,
		Moved:         result.Moved,
		LandedCommits: int32(result.Landed),
		ReplayCommits: int32(result.Replay),
	}), nil
}

// ResetWorktree puts the agent's branch and tracked files back to the base, discarding its work.
func (s *Service) ResetWorktree(
	ctx context.Context,
	req *connect.Request[agentv1.ResetWorktreeRequest],
) (*connect.Response[agentv1.ResetWorktreeResponse], error) {
	result, err := s.refresher.Reset(ctx, req.Msg.GetAgentId())
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to reset the worktree")
	}

	return connect.NewResponse(&agentv1.ResetWorktreeResponse{
		BaseRef:            result.BaseRef,
		BaseSha:            result.BaseSHA,
		Branch:             result.Branch,
		Commits:            int32(result.Commits),
		Rounds:             int32(result.Rounds),
		ParkedRef:          result.ParkedRef,
		StaleReviewHistory: result.StaleReviewHistory,
	}), nil
}
