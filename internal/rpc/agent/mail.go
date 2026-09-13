package agent

import (
	"context"

	"weave/internal/agents"
	"weave/internal/caller"
	"weave/internal/fail"
	"weave/internal/rpcerr"
	"weave/lib/pagination"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"

	mailstore "weave/internal/store/mail"

	"google.golang.org/protobuf/types/known/emptypb"
)

type mailer interface {
	SendMail(ctx context.Context, params agents.SendMailParams) (*mailstore.Mail, error)
	ClaimMail(ctx context.Context, agentID string) ([]mailstore.Mail, error)
	ListMail(ctx context.Context, params agents.ListMailParams) ([]mailstore.Mail, error)
	DeleteMail(ctx context.Context, ids []string) error
	ListUnreadMailboxes(ctx context.Context) ([]string, error)
}

// SendMail records mail from the calling agent to the recipient the request names.
func (s *Service) SendMail(ctx context.Context, req *agentv1.SendMailRequest) (*agentv1.SendMailResponse, *fail.Error) {
	sent, err := s.mail.SendMail(ctx, agents.SendMailParams{
		From:      caller.From(ctx).AgentID,
		ToAgentID: req.GetToAgentId(),
		Body:      req.GetBody(),
		ReplyTo:   req.ReplyTo,
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to send the mail")
	}

	return &agentv1.SendMailResponse{Mail: renderMail(*sent)}, nil
}

// ReadMail claims an agent's waiting mail, defaulting to the caller's own mailbox.
func (s *Service) ReadMail(ctx context.Context, req *agentv1.ReadMailRequest) (*agentv1.ReadMailResponse, *fail.Error) {
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
func (s *Service) ListMail(ctx context.Context, req *agentv1.ListMailRequest) (*agentv1.ListMailResponse, *fail.Error) {
	id, failure := scopedAgentID(ctx, req.GetId())
	if failure != nil {
		return nil, failure
	}

	logCrossAgentRead(ctx, id, "agent listed another agent's mail")

	paginator, err := pagination.New[mailstore.Cursor, mail](req.GetPagination())
	if err != nil {
		return nil, rpcerr.From(err, "failed to read the pagination")
	}

	listed, err := s.mail.ListMail(ctx, agents.ListMailParams{
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
	response.Mail = xslice.Map(resources, func(resource mail) *agentv1.Mail { return resource.Mail })

	return response, nil
}

// DeleteMail permanently removes mail by id, so the recipient never sees it.
func (s *Service) DeleteMail(ctx context.Context, req *agentv1.DeleteMailRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.mail.DeleteMail(ctx, req.GetIds()); err != nil {
		return nil, rpcerr.From(err, "failed to delete mail")
	}

	return &emptypb.Empty{}, nil
}
