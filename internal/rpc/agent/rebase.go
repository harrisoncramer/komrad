package agent

import (
	"context"

	"komrad/internal/checkouts"
	"komrad/internal/fail"
	"komrad/internal/rpcerr"
	agentv1 "komrad/proto/gen/agent/v1"
)

// worktreeRebaser brings a task's existing worktree up to date in place.
type worktreeRebaser interface {
	PlanRebase(ctx context.Context, params checkouts.RebaseParams) (*checkouts.RebasePlan, error)
	Rebase(ctx context.Context, params checkouts.RebaseParams) (*checkouts.RebaseResult, error)
	PlaceStand(ctx context.Context, params checkouts.PlaceStandParams) (string, error)
}

// PlanRebase reads what a rebase would do without doing it.
func (s *Handler) PlanRebase(ctx context.Context, req *agentv1.PlanRebaseRequest) (*agentv1.PlanRebaseResponse, *fail.Error) {
	plan, err := s.rebaser.PlanRebase(ctx, checkouts.RebaseParams{Task: req.GetTask(), OntoBase: req.GetOntoBase()})
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

// RebaseWorktree moves the task's branch onto the latest base in place.
func (s *Handler) RebaseWorktree(ctx context.Context, req *agentv1.RebaseWorktreeRequest) (*agentv1.RebaseWorktreeResponse, *fail.Error) {
	result, err := s.rebaser.Rebase(ctx, checkouts.RebaseParams{Task: req.GetTask(), OntoBase: req.GetOntoBase()})
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

// PlaceStand records the commit a standing task's own commits sit on.
func (s *Handler) PlaceStand(ctx context.Context, req *agentv1.PlaceStandRequest) (*agentv1.PlaceStandResponse, *fail.Error) {
	tip, err := s.rebaser.PlaceStand(ctx, checkouts.PlaceStandParams{Task: req.GetTask(), Commit: req.GetCommit()})
	if err != nil {
		return nil, rpcerr.From(err, "failed to record the commit the task stands on")
	}

	return &agentv1.PlaceStandResponse{Tip: tip}, nil
}
