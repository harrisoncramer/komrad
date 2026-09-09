package agent

import (
	"context"

	"weave/internal/agents"
	"weave/internal/caller"
	"weave/internal/rpcerr"
	"weave/lib/pagination"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"

	mailstore "weave/internal/store/mail"

	"connectrpc.com/connect"
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
func (s *Service) SendMail(ctx context.Context, req *connect.Request[agentv1.SendMailRequest]) (*connect.Response[agentv1.SendMailResponse], error) {
	sent, err := s.mail.SendMail(ctx, agents.SendMailParams{
		From:      caller.From(ctx).AgentID,
		ToAgentID: req.Msg.GetToAgentId(),
		Body:      req.Msg.GetBody(),
		ReplyTo:   req.Msg.ReplyTo,
	})
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to send the mail")
	}

	return connect.NewResponse(&agentv1.SendMailResponse{Mail: renderMail(*sent)}), nil
}

// ReadMail claims an agent's waiting mail, defaulting to the caller's own mailbox.
func (s *Service) ReadMail(ctx context.Context, req *connect.Request[agentv1.ReadMailRequest]) (*connect.Response[agentv1.ReadMailResponse], error) {
	id, err := scopedAgentID(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}

	logCrossAgentRead(ctx, id, "agent read another agent's mail")

	claimed, err := s.mail.ClaimMail(ctx, id)
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to read the mail")
	}

	return connect.NewResponse(&agentv1.ReadMailResponse{
		Mail: xslice.Map(claimed, renderMail),
	}), nil
}

// ListMail returns a page of an agent's mail without claiming any of it.
func (s *Service) ListMail(ctx context.Context, req *connect.Request[agentv1.ListMailRequest]) (*connect.Response[agentv1.ListMailResponse], error) {
	id, err := scopedAgentID(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}

	logCrossAgentRead(ctx, id, "agent listed another agent's mail")

	paginator, err := pagination.New[mailstore.Cursor, mail](req.Msg.GetPagination())
	if err != nil {
		return nil, err
	}

	listed, err := s.mail.ListMail(ctx, agents.ListMailParams{
		AgentID: id,
		Sent:    req.Msg.GetDirection() == agentv1.MailDirection_MAIL_DIRECTION_SENT,
		Limit:   int32(paginator.Limit()),
		Cursor:  paginator.Cursor(),
	})
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to list the mail")
	}

	resources := xslice.Map(listed, renderMailPage)

	next, err := paginator.NextToken(resources)
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to build the next page token")
	}

	response := &agentv1.ListMailResponse{NextPageToken: next}
	response.Mail = xslice.Map(resources, func(resource mail) *agentv1.Mail { return resource.Mail })

	return connect.NewResponse(response), nil
}

// DeleteMail permanently removes mail by id, so the recipient never sees it.
func (s *Service) DeleteMail(ctx context.Context, req *connect.Request[agentv1.DeleteMailRequest]) (*connect.Response[emptypb.Empty], error) {
	if err := s.mail.DeleteMail(ctx, req.Msg.GetIds()); err != nil {
		return nil, rpcerr.Wrap(err, "failed to delete mail")
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}
