package agents

import (
	"context"
	"errors"
	"fmt"
	"time"

	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	"weave/lib/xslice"
)

const maxPendingMail = 100

const maxClaimedMail = 20

var ErrRecipientNotFound = errors.New("no agent holds that id")

var ErrSenderUnknown = errors.New("the sender is not an agent this panel knows")

var ErrSelfAddressed = errors.New("an agent cannot send itself mail")

var ErrMailboxFull = errors.New("the recipient's mailbox is full")

var ErrThreadCapped = errors.New("this thread has reached its hop limit")

var ErrReplyToNotFound = errors.New("no mail holds that id")

var ErrMailNotFound = errors.New("no mail holds that id, deleting nothing")

var ErrNoticeKindMissing = errors.New("a notice must name a kind other than plain")

const WeaveSenderName = "weave"

type mailReadWriter interface {
	Send(ctx context.Context, params mailstore.SendParams) (*mailstore.Mail, error)
	GetByID(ctx context.Context, id string) (*mailstore.Mail, error)
	Claim(ctx context.Context, toAgentID string, limit int32) ([]mailstore.Mail, error)
	CountPending(ctx context.Context, toAgentID string) (int64, error)
	ListPage(ctx context.Context, toAgentID string, limit int32, cursor *mailstore.Cursor) ([]mailstore.Mail, error)
	ListByID(ctx context.Context, ids []string) ([]mailstore.Mail, error)
	Delete(ctx context.Context, ids []string) error
	ListUnreadMailboxes(ctx context.Context) ([]string, error)
}

type mailConfigReader interface {
	GetDeliveryDeadline() (time.Duration, error)
	GetMaxHops() (int, error)
}

type MailService struct {
	mailReadWriter mailReadWriter
	agentFinder    agentFinder
	config         mailConfigReader
}

type NewMailServiceParams struct {
	MailReadWriter mailReadWriter
	AgentFinder    agentFinder
	Config         mailConfigReader
}

func NewMailService(p NewMailServiceParams) *MailService {
	return &MailService{
		mailReadWriter: p.MailReadWriter,
		agentFinder:    p.AgentFinder,
		config:         p.Config,
	}
}

type SendMailParams struct {
	From      string
	ToAgentID string
	Body      string
	ReplyTo   *string
}

func (s *MailService) SendMail(ctx context.Context, params SendMailParams) (*mailstore.Mail, error) {
	return s.send(ctx, sendParams{
		From:      params.From,
		ToAgentID: params.ToAgentID,
		Body:      params.Body,
		ReplyTo:   params.ReplyTo,
		Kind:      mailstore.KindPlain,
	})
}

// SendSystemMail sends mail as weave itself, still running every check an ordinary send does
// for an archived recipient, a full mailbox, and the hop cap.
func (s *MailService) SendSystemMail(ctx context.Context, toAgentID, body string) (*mailstore.Mail, error) {
	return s.send(ctx, sendParams{
		ToAgentID: toAgentID,
		Body:      body,
		Kind:      mailstore.KindPlain,
		System:    true,
	})
}

type SendNoticeParams struct {
	ToAgentID string
	Body      string
	// Kind is what the notice is about, and is what stops a second copy queueing up behind
	// the first.
	Kind mailstore.Kind
}

// SendNotice sends mail as weave itself under a kind the recipient holds at most one unclaimed
// copy of, answering nil and no error when one is already waiting.
func (s *MailService) SendNotice(ctx context.Context, params SendNoticeParams) (*mailstore.Mail, error) {
	if params.Kind == "" || params.Kind == mailstore.KindPlain {
		return nil, fmt.Errorf("%w: a notice needs a kind of its own to be deduplicated by", ErrNoticeKindMissing)
	}

	return s.send(ctx, sendParams{
		ToAgentID: params.ToAgentID,
		Body:      params.Body,
		Kind:      params.Kind,
		System:    true,
	})
}

type sendParams struct {
	From      string
	ToAgentID string
	Body      string
	ReplyTo   *string
	Kind      mailstore.Kind
	// System marks mail weave raises itself rather than on a caller's behalf, which is what
	// puts weave's own name on it instead of a sender's.
	System bool
}

func (s *MailService) send(ctx context.Context, params sendParams) (*mailstore.Mail, error) {
	recipient, err := s.agentFinder.GetByID(ctx, params.ToAgentID)
	if err != nil {
		return nil, err
	}
	if recipient == nil {
		return nil, fmt.Errorf("%w: %q", ErrRecipientNotFound, params.ToAgentID)
	}
	if recipient.ArchivedAt != nil {
		return nil, fmt.Errorf("%w: %q", ErrAgentArchived, params.ToAgentID)
	}

	sender, err := s.findSender(ctx, params.From)
	if err != nil {
		return nil, err
	}
	if sender != nil && sender.ID == recipient.ID {
		return nil, ErrSelfAddressed
	}

	pending, err := s.mailReadWriter.CountPending(ctx, recipient.ID)
	if err != nil {
		return nil, err
	}
	if pending >= maxPendingMail {
		return nil, fmt.Errorf("%w: %q is holding %d unread", ErrMailboxFull, recipient.Name, pending)
	}

	hops := int32(1)
	if params.ReplyTo != nil {
		parent, err := s.mailReadWriter.GetByID(ctx, *params.ReplyTo)
		if err != nil {
			return nil, err
		}
		if parent == nil {
			return nil, fmt.Errorf("%w: %q", ErrReplyToNotFound, *params.ReplyTo)
		}
		hops = parent.Hops + 1
	}

	maxHops, err := s.config.GetMaxHops()
	if err != nil {
		return nil, err
	}
	if int(hops) > maxHops {
		return nil, fmt.Errorf("%w: %d hops is the limit for this thread, say so and stop rather than replying again", ErrThreadCapped, maxHops)
	}

	deadline, err := s.config.GetDeliveryDeadline()
	if err != nil {
		return nil, err
	}

	send := mailstore.SendParams{
		ToAgentID: recipient.ID,
		Body:      params.Body,
		Kind:      params.Kind,
		ReplyTo:   params.ReplyTo,
		Hops:      hops,
		Deadline:  deadline,
	}
	if sender != nil {
		send.FromAgentID = &sender.ID
		send.FromName = sender.Name
	} else if params.System {
		send.FromName = WeaveSenderName
	}

	sent, err := s.mailReadWriter.Send(ctx, send)
	if err != nil {
		return nil, err
	}

	return sent, nil
}

func (s *MailService) ClaimMail(ctx context.Context, agentID string) ([]mailstore.Mail, error) {
	agent, err := s.agentFinder.GetByID(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, fmt.Errorf("%w: %q", ErrAgentNotFound, agentID)
	}

	claimed, err := s.mailReadWriter.Claim(ctx, agent.ID, maxClaimedMail)
	if err != nil {
		return nil, err
	}

	return claimed, nil
}

// ListMail returns one page of the mail addressed to an agent, most recent first,
// without claiming any of it.
func (s *MailService) ListMail(ctx context.Context, agentID string, limit int32, cursor *mailstore.Cursor) ([]mailstore.Mail, error) {
	agent, err := s.agentFinder.GetByID(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, fmt.Errorf("%w: %q", ErrAgentNotFound, agentID)
	}

	listed, err := s.mailReadWriter.ListPage(ctx, agent.ID, limit, cursor)
	if err != nil {
		return nil, err
	}

	return listed, nil
}

// DeleteMail permanently removes a batch of mail by id, deleting nothing at all unless every
// id holds mail.
func (s *MailService) DeleteMail(ctx context.Context, ids []string) error {
	found, err := s.mailReadWriter.ListByID(ctx, ids)
	if err != nil {
		return fmt.Errorf("failed to look up %d mail: %w", len(ids), err)
	}
	if len(found) != len(ids) {
		present := xslice.KeyBy(found, func(mail mailstore.Mail) string { return mail.ID })

		missing := xslice.Filter(ids, func(id string) bool {
			_, ok := present[id]

			return !ok
		})

		return fmt.Errorf("%w: %s", ErrMailNotFound, describeMissing(missing))
	}

	if err := s.mailReadWriter.Delete(ctx, ids); err != nil {
		return fmt.Errorf("failed to delete %d mail: %w", len(ids), err)
	}

	return nil
}

// ListUnreadMailboxes is every agent holding mail nobody has read yet.
func (s *MailService) ListUnreadMailboxes(ctx context.Context) ([]string, error) {
	unread, err := s.mailReadWriter.ListUnreadMailboxes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the mailboxes holding unread mail: %w", err)
	}

	return unread, nil
}

func (s *MailService) findSender(ctx context.Context, from string) (*agentstore.Agent, error) {
	if from == "" {
		return nil, nil
	}

	sender, err := s.agentFinder.GetByID(ctx, from)
	if err != nil {
		return nil, err
	}
	if sender == nil {
		return nil, fmt.Errorf("%w: %q", ErrSenderUnknown, from)
	}

	return sender, nil
}
