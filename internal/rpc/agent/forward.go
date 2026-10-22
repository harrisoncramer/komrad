package agent

import (
	"context"

	"komrad/internal/caller"
	"komrad/internal/fail"
	"komrad/internal/rpcerr"
	"komrad/internal/transcripts"
	"komrad/lib/dates"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1komrad"
)

// ForwardLastTurns sends another agent's newest turns to a third agent as mail komrad composes.
func (s *Handler) ForwardLastTurns(ctx context.Context, req *agentv1komrad.ForwardLastTurnsRequest) (*agentv1.ForwardLastTurnsResponse, *fail.Error) {
	logCrossAgentRead(ctx, req.FromAgentId.String(), "agent read another agent's transcript")

	forwarded, err := s.transcripts.ForwardLastTurns(ctx, transcripts.ForwardLastTurnsParams{
		From:      caller.From(ctx).AgentID,
		SourceID:  req.FromAgentId.String(),
		ToAgentID: req.ToAgentId.String(),
		Turns:     xptr.Deref(req.Turns),
		MaxBytes:  xptr.Deref(req.MaxBytes),
		Note:      xptr.Deref(req.Note),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to forward the last turns")
	}

	return &agentv1.ForwardLastTurnsResponse{
		MailId:     forwarded.Mail.ID,
		ToAgentId:  forwarded.Mail.ToAgentID,
		CreatedAt:  dates.Stamp(forwarded.Mail.CreatedAt),
		Turns:      int32(forwarded.Turns),
		Characters: int32(forwarded.Characters),
		Warning:    buildDormantWarning(forwarded.Recipient, forwarded.Dormant),
	}, nil
}
