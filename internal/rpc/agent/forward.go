package agent

import (
	"context"
	"errors"

	"komrad/internal/agenterr"
	"komrad/internal/caller"
	"komrad/internal/fail"
	"komrad/internal/mail"
	"komrad/internal/rpcerr"
	"komrad/internal/store"
	"komrad/internal/transcripts"
	"komrad/lib/dates"
	agentv1 "komrad/proto/gen/agent/v1"
)

// ForwardLastTurns sends another agent's newest turns to a third agent as mail komrad composes.
func (s *Handler) ForwardLastTurns(ctx context.Context, req *agentv1.ForwardLastTurnsRequest) (*agentv1.ForwardLastTurnsResponse, *fail.Error) {
	logCrossAgentRead(ctx, req.GetFromAgentId(), "agent read another agent's transcript")

	source, err := s.agents.GetAgent(ctx, req.GetFromAgentId())
	if errors.Is(err, store.ErrNotFound) {
		return nil, agenterr.ErrAgentNotFound.Saying("no agent with id %q", req.GetFromAgentId())
	}
	if err != nil {
		return nil, rpcerr.From(err, "failed to look up agent")
	}

	forward, err := s.transcripts.ComposeForward(ctx, transcripts.ComposeForwardParams{
		Source:   *source,
		Turns:    req.GetTurns(),
		MaxBytes: req.GetMaxBytes(),
		Note:     req.GetNote(),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the last turns")
	}

	recipient, err := s.agents.GetAgent(ctx, req.GetToAgentId())
	if errors.Is(err, store.ErrNotFound) {
		return nil, mail.ErrRecipientNotFound.Saying("no agent with id %q", req.GetToAgentId())
	}
	if err != nil {
		return nil, rpcerr.From(err, "failed to look up the recipient")
	}

	sent, err := s.mail.SendMail(ctx, mail.SendMailParams{
		From:      caller.From(ctx).AgentID,
		ToAgentID: recipient.ID,
		Body:      forward.Body,
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to send the mail")
	}

	return &agentv1.ForwardLastTurnsResponse{
		MailId:     sent.ID,
		ToAgentId:  sent.ToAgentID,
		CreatedAt:  dates.Stamp(sent.CreatedAt),
		Turns:      int32(forward.Turns),
		Characters: int32(forward.Characters),
		Warning:    s.buildDormantWarning(*recipient),
	}, nil
}
