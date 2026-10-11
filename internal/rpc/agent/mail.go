package agent

import (
	"context"
	"fmt"

	"komrad/internal/caller"
	"komrad/internal/fail"
	"komrad/internal/mail"
	"komrad/internal/rpc/render"
	"komrad/internal/rpcerr"
	"komrad/internal/rpcpage"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"

	agentstore "komrad/internal/store/agent"
	mailstore "komrad/internal/store/mail"

	"google.golang.org/protobuf/types/known/emptypb"
)

type mailer interface {
	ResolveRecipient(ctx context.Context, params mail.ResolveRecipientParams) (*agentstore.Agent, error)
	SendMail(ctx context.Context, params mail.SendMailParams) (*mailstore.Mail, error)
	ClaimMail(ctx context.Context, agentID string) ([]mailstore.Mail, error)
	ListMail(ctx context.Context, params mail.ListMailParams) ([]mailstore.Mail, error)
	DeleteMail(ctx context.Context, ids []string) error
	IsDormant(agent agentstore.Agent) bool
}

// SendMail records mail from the calling agent to the recipient the request names.
func (s *Handler) SendMail(ctx context.Context, req *agentv1.SendMailRequest) (*agentv1.SendMailResponse, *fail.Error) {
	from := caller.From(ctx).AgentID

	recipient, err := s.mail.ResolveRecipient(ctx, mail.ResolveRecipientParams{
		From: from,
		To:   req.GetTo(),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to find the recipient")
	}

	sent, err := s.mail.SendMail(ctx, mail.SendMailParams{
		From:      from,
		ToAgentID: recipient.ID,
		Body:      req.GetBody(),
		ReplyTo:   req.ReplyTo,
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to send the mail")
	}

	return &agentv1.SendMailResponse{
		Mail:    render.Mail(*sent),
		Warning: buildDormantWarning(*recipient, s.mail.IsDormant(*recipient)),
	}, nil
}

func buildDormantWarning(recipient agentstore.Agent, dormant bool) *string {
	if !dormant {
		return nil
	}

	return xptr.Ptr(fmt.Sprintf("%s is dormant, nothing runs behind it, so this mail waits until resume_agent wakes it with id %s", recipient.Name, recipient.ID))
}

// ReadMail claims an agent's waiting mail, defaulting to the caller's own mailbox.
func (s *Handler) ReadMail(ctx context.Context, req *agentv1.ReadMailRequest) (*agentv1.ReadMailResponse, *fail.Error) {
	id, failure := scopedAgentID(ctx, req.GetId())
	if failure != nil {
		return nil, failure
	}

	logCrossAgentRead(ctx, id, "agent read another agent's mail")

	claimed, err := s.mail.ClaimMail(ctx, id)
	if err != nil {
		return nil, rpcerr.From(err, "failed to read the mail")
	}

	return &agentv1.ReadMailResponse{
		Mail: xslice.Map(claimed, render.Mail),
	}, nil
}

// ListMail returns a page of an agent's mail without claiming any of it.
func (s *Handler) ListMail(ctx context.Context, req *agentv1.ListMailRequest) (*agentv1.ListMailResponse, *fail.Error) {
	id, failure := scopedAgentID(ctx, req.GetId())
	if failure != nil {
		return nil, failure
	}

	logCrossAgentRead(ctx, id, "agent listed another agent's mail")

	page, failure := rpcpage.List(req.GetPagination(), func(limit int64, cursor *mailstore.Cursor) ([]mailPage, *fail.Error) {
		listed, err := s.mail.ListMail(ctx, mail.ListMailParams{
			AgentID: id,
			Sent:    req.GetDirection() == agentv1.MailDirection_MAIL_DIRECTION_SENT,
			Limit:   int32(limit),
			Cursor:  cursor,
		})
		if err != nil {
			return nil, rpcerr.From(err, "failed to list the mail")
		}

		return xslice.Map(listed, renderMailPage), nil
	})
	if failure != nil {
		return nil, failure
	}

	response := &agentv1.ListMailResponse{NextPageToken: page.Next}
	response.Mail = xslice.Map(page.Resources, func(resource mailPage) *agentv1.Mail { return resource.Mail })

	return response, nil
}

// DeleteMail permanently removes mail by id, so the recipient never sees it.
func (s *Handler) DeleteMail(ctx context.Context, req *agentv1.DeleteMailRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.mail.DeleteMail(ctx, req.GetIds()); err != nil {
		return nil, rpcerr.From(err, "failed to delete mail")
	}

	return &emptypb.Empty{}, nil
}
