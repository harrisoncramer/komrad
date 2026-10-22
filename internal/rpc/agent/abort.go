package agent

import (
	"context"

	"komrad/internal/checkouts"
	"komrad/internal/fail"
	"komrad/internal/rpc/render"
	"komrad/internal/rpcerr"
	agentv1 "komrad/proto/gen/agent/v1"
)

// sequenceAborter undoes what a task's worktree has part-way through.
type sequenceAborter interface {
	Abort(ctx context.Context, task string) (*checkouts.AbortResult, error)
}

// AbortSequence undoes the sequence part-way through the task's worktree.
func (s *Handler) AbortSequence(ctx context.Context, req *agentv1.AbortSequenceRequest) (*agentv1.AbortSequenceResponse, *fail.Error) {
	result, err := s.aborter.Abort(ctx, req.GetTask())
	if err != nil {
		return nil, rpcerr.From(err, "failed to abort what the worktree had in progress")
	}

	return &agentv1.AbortSequenceResponse{
		Sequence:  render.SequenceOf(result.Sequence),
		Branch:    result.Branch,
		ParkedRef: result.ParkedRef,
	}, nil
}
