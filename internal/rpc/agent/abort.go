package agent

import (
	"context"

	"komrad/internal/checkouts"
	"komrad/internal/fail"
	"komrad/internal/rpc/render"
	"komrad/internal/rpcerr"
	agentv1 "komrad/proto/gen/agent/v1"
)

// sequenceAborter undoes what an agent's worktree has part-way through.
type sequenceAborter interface {
	Abort(ctx context.Context, agentID string) (*checkouts.AbortResult, error)
}

// AbortSequence undoes the sequence part-way through the agent's worktree.
func (s *Handler) AbortSequence(ctx context.Context, req *agentv1.AbortSequenceRequest) (*agentv1.AbortSequenceResponse, *fail.Error) {
	result, err := s.aborter.Abort(ctx, req.GetAgentId())
	if err != nil {
		return nil, rpcerr.From(err, "failed to abort what the worktree had in progress")
	}

	return &agentv1.AbortSequenceResponse{
		Sequence:    render.AgentSequenceOf(result.Sequence),
		Branch:      result.Branch,
		ParkedRef:   result.ParkedRef,
		Interrupted: result.Interrupted,
	}, nil
}
