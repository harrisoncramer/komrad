package agent

import (
	"context"

	"weave/internal/agents"
	"weave/internal/fail"
	"weave/internal/rpcerr"
	agentv1 "weave/proto/gen/agent/v1"
)

// worktreeRebaser brings an agent's existing worktree up to date in place.
type worktreeRebaser interface {
	PlanRebase(ctx context.Context, agentID string) (*agents.RebasePlan, error)
	Rebase(ctx context.Context, agentID string) (*agents.RebaseResult, error)
}

// PlanRebase reads what a rebase would do without doing it.
func (s *AgentService) PlanRebase(ctx context.Context, req *agentv1.PlanRebaseRequest) (*agentv1.PlanRebaseResponse, *fail.Error) {
	plan, err := s.rebaser.PlanRebase(ctx, req.GetAgentId())
	if err != nil {
		return nil, rpcerr.From(err, "failed to work out what a rebase would do")
	}

	return &agentv1.PlanRebaseResponse{
		BaseRef:       plan.BaseRef,
		BaseSha:       plan.BaseSHA,
		Commits:       int32(plan.Commits),
		Branch:        plan.Branch,
		LandedCommits: int32(plan.Landed),
		ReplayCommits: int32(plan.Replay),
		Merges:        int32(plan.Merges),
		ConflictPaths: plan.ConflictPaths,
	}, nil
}

// RebaseWorktree moves the agent's branch onto the latest base in place.
func (s *AgentService) RebaseWorktree(ctx context.Context, req *agentv1.RebaseWorktreeRequest) (*agentv1.RebaseWorktreeResponse, *fail.Error) {
	result, err := s.rebaser.Rebase(ctx, req.GetAgentId())
	if err != nil {
		return nil, rpcerr.From(err, "failed to rebase the worktree")
	}

	return &agentv1.RebaseWorktreeResponse{
		BaseRef:       result.BaseRef,
		BaseSha:       result.BaseSHA,
		Commits:       int32(result.Commits),
		Branch:        result.Branch,
		Conflicted:    result.Conflicted,
		Moved:         result.Moved,
		LandedCommits: int32(result.Landed),
		ReplayCommits: int32(result.Replay),
	}, nil
}
