package agent

import (
	"context"
	"fmt"

	"weave/internal/caller"
	"weave/internal/fail"
	"weave/internal/mail"
	"weave/internal/rpcerr"
	"weave/lib/pagination"
	"weave/lib/xptr"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"

	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"

	"google.golang.org/protobuf/types/known/emptypb"
)

type mailer interface {
	ResolveRecipient(ctx context.Context, params mail.ResolveRecipientParams) (*agentstore.Agent, error)
	SendMail(ctx context.Context, params mail.SendMailParams) (*mailstore.Mail, error)
	ClaimMail(ctx context.Context, agentID string) ([]mailstore.Mail, error)
	ListMail(ctx context.Context, params mail.ListMailParams) ([]mailstore.Mail, error)
	DeleteMail(ctx context.Context, ids []string) error
}

// SendMail records mail from the calling agent to the recipient the request names.
func (s *AgentService) SendMail(ctx context.Context, req *agentv1.SendMailRequest) (*agentv1.SendMailResponse, *fail.Error) {
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
		Mail:    renderMail(*sent),
		Warning: s.buildDormantWarning(*recipient),
	}, nil
}

func (s *AgentService) buildDormantWarning(recipient agentstore.Agent) *string {
	if s.liveness.IsAgentRunning(recipient) || !recipient.State.DoesPaneDecide() {
		return nil
	}

	return xptr.Ptr(fmt.Sprintf("%s is dormant, nothing runs behind it, so this mail waits until it is resumed", recipient.Name))
}

// ReadMail claims an agent's waiting mail, defaulting to the caller's own mailbox.
func (s *AgentService) ReadMail(ctx context.Context, req *agentv1.ReadMailRequest) (*agentv1.ReadMailResponse, *fail.Error) {
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
		Mail: xslice.Map(claimed, renderMail),
	}, nil
}

// ListMail returns a page of an agent's mail without claiming any of it.
func (s *AgentService) ListMail(ctx context.Context, req *agentv1.ListMailRequest) (*agentv1.ListMailResponse, *fail.Error) {
	id, failure := scopedAgentID(ctx, req.GetId())
	if failure != nil {
		return nil, failure
	}

	logCrossAgentRead(ctx, id, "agent listed another agent's mail")

	paginator, err := pagination.New[mailstore.Cursor, mailPage](req.GetPagination())
	if err != nil {
		return nil, rpcerr.From(err, "failed to read the pagination")
	}

	listed, err := s.mail.ListMail(ctx, mail.ListMailParams{
		AgentID: id,
		Sent:    req.GetDirection() == agentv1.MailDirection_MAIL_DIRECTION_SENT,
		Limit:   int32(paginator.Limit()),
		Cursor:  paginator.Cursor(),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the mail")
	}

	resources := xslice.Map(listed, renderMailPage)

	next, err := paginator.NextToken(resources)
	if err != nil {
		return nil, rpcerr.From(err, "failed to build the next page token")
	}

	response := &agentv1.ListMailResponse{NextPageToken: next}
	response.Mail = xslice.Map(resources, func(resource mailPage) *agentv1.Mail { return resource.Mail })

	return response, nil
}

// DeleteMail permanently removes mail by id, so the recipient never sees it.
func (s *AgentService) DeleteMail(ctx context.Context, req *agentv1.DeleteMailRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.mail.DeleteMail(ctx, req.GetIds()); err != nil {
		return nil, rpcerr.From(err, "failed to delete mail")
	}

	return &emptypb.Empty{}, nil
}
