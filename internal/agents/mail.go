package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"weave/internal/fail"
	"weave/internal/logging"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	"weave/lib/xslice"
)

const maxPendingMail = 100

// MaxMailBody is the most characters one piece of mail carries.
const MaxMailBody = 10000

const maxClaimedMail = 20

const exchangeCappedBody = "weave: %s (%s) wrote to you and the send was refused, because the two of you have now exchanged %d pieces of mail, which is the limit. Nothing further will get through between you, in either direction. Whatever you were waiting on from them is not coming, so do not keep waiting: say where you got to, and tell the person what is unfinished. A row has been marked for attention in the panel so the person knows to look."

var ErrRecipientNotFound = fail.NotFound("no agent answers to that address")

var ErrRecipientAmbiguous = fail.InvalidArgument("more than one live agent holds that name")

var ErrSenderUnknown = fail.InvalidArgument("the sender is not an agent this panel knows")

var ErrSelfAddressed = fail.InvalidArgument("an agent cannot send itself mail")

var ErrMailboxFull = fail.Exhausted("the recipient's mailbox is full")

var ErrExchangeCapped = fail.Exhausted("this pair of agents has reached its mail limit")

var ErrReplyToNotFound = fail.InvalidArgument("no mail holds that id")

var ErrMailNotFound = fail.NotFound("no mail holds that id, deleting nothing")

var ErrNoticeKindMissing = fail.InvalidArgument("a notice must name a kind other than plain")

const WeaveSenderName = "weave"

type mailReadWriter interface {
	Send(ctx context.Context, params mailstore.SendParams) (*mailstore.Mail, error)
	GetByID(ctx context.Context, id string) (*mailstore.Mail, error)
	Claim(ctx context.Context, toAgentID string, limit int32) ([]mailstore.Mail, error)
	CountPending(ctx context.Context, toAgentID string) (int64, error)
	GetPendingOfKind(ctx context.Context, toAgentID string, kind mailstore.Kind, about *string) (*mailstore.Mail, error)
	ListPage(ctx context.Context, toAgentID string, limit int32, cursor *mailstore.Cursor) ([]mailstore.Mail, error)
	ListSentPage(ctx context.Context, fromAgentID string, limit int32, cursor *mailstore.Cursor) ([]mailstore.Mail, error)
	ListByID(ctx context.Context, ids []string) ([]mailstore.Mail, error)
	Delete(ctx context.Context, ids []string) error
	ListUnreadMailboxes(ctx context.Context) ([]string, error)
	CountExchanged(ctx context.Context, one, other string) (int64, error)
}

type mailAgentReadWriter interface {
	agentGetter
	ListLiveNamed(ctx context.Context, name string) ([]agentstore.Agent, error)
}

type mailConfigReader interface {
	GetDeliveryDeadline() (time.Duration, error)
	GetMaxExchange() (int, error)
}

type MailService struct {
	mailReadWriter mailReadWriter
	agents         mailAgentReadWriter
	workflows      workflowGetter
	config         mailConfigReader
}

type NewMailServiceParams struct {
	MailReadWriter mailReadWriter
	Agents         mailAgentReadWriter
	Workflows      workflowGetter
	Config         mailConfigReader
}

// NewMailService builds the service that sends, claims and lists the mail agents leave for
// each other.
func NewMailService(params NewMailServiceParams) *MailService {
	return &MailService{
		mailReadWriter: params.MailReadWriter,
		agents:         params.Agents,
		workflows:      params.Workflows,
		config:         params.Config,
	}
}

type ResolveRecipientParams struct {
	From string
	To   string
}

// ResolveRecipient turns an address into the agent it names: an id as it stands, or else the
// name of a live agent, looked for in the sender's own workflow before every workflow.
func (s *MailService) ResolveRecipient(ctx context.Context, params ResolveRecipientParams) (*agentstore.Agent, error) {
	if store.IsID(params.To) {
		recipient, err := s.agents.GetByID(ctx, params.To)
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrRecipientNotFound.Saying("no agent holds the id %q", params.To)
		}
		if err != nil {
			return nil, err
		}

		return recipient, nil
	}

	named, err := s.agents.ListLiveNamed(ctx, params.To)
	if err != nil {
		return nil, err
	}

	if params.From != "" {
		sender, err := s.findSender(ctx, params.From)
		if err != nil {
			return nil, err
		}

		nearby := xslice.Filter(named, func(agent agentstore.Agent) bool {
			return agent.WorkflowID == sender.WorkflowID
		})
		if len(nearby) == 1 {
			return &nearby[0], nil
		}
	}

	switch len(named) {
	case 0:
		return nil, ErrRecipientNotFound.Saying("no live agent is named %q", params.To)
	case 1:
		return &named[0], nil
	}

	return nil, ErrRecipientAmbiguous.Saying("%d live agents are named %q, so address one of them by id: %s", len(named), params.To, s.describeHolders(ctx, named))
}

func (s *MailService) describeHolders(ctx context.Context, holders []agentstore.Agent) string {
	described := xslice.Map(holders, func(agent agentstore.Agent) string {
		workflow, err := s.workflows.Get(ctx, agent.WorkflowID)
		if err != nil {
			return fmt.Sprintf("%s in workflow %s", agent.ID, agent.WorkflowID)
		}

		return fmt.Sprintf("%s in workflow %q", agent.ID, workflow.Name)
	})

	return strings.Join(described, ", ")
}

type SendMailParams struct {
	From      string
	ToAgentID string
	Body      string
	ReplyTo   *string
}

// SendMail records one piece of ordinary mail from one agent to another.
func (s *MailService) SendMail(ctx context.Context, params SendMailParams) (*mailstore.Mail, error) {
	return s.send(ctx, sendParams{
		From:      params.From,
		ToAgentID: params.ToAgentID,
		Body:      params.Body,
		ReplyTo:   params.ReplyTo,
		Kind:      mailstore.KindPlain,
	})
}

// SendSystemMail sends mail as weave itself, still running every check an ordinary send does.
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
	// Kind is what the notice is about, which is how the panel tells it from mail an agent wrote.
	Kind mailstore.Kind
	// About is the other agent the notice concerns, and a further notice about the same agent
	// joins the one still unread rather than being sent again.
	About *string
}

// SendNotice sends a notice as weave itself, handing back the unread notice about the same
// agent instead when the recipient is still holding one.
func (s *MailService) SendNotice(ctx context.Context, params SendNoticeParams) (*mailstore.Mail, error) {
	if params.Kind == "" || params.Kind == mailstore.KindPlain {
		return nil, fmt.Errorf("%w: a notice needs a kind of its own to be told apart by", ErrNoticeKindMissing)
	}

	if params.About != nil {
		waiting, err := s.mailReadWriter.GetPendingOfKind(ctx, params.ToAgentID, params.Kind, params.About)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		if waiting != nil {
			return waiting, nil
		}
	}

	return s.send(ctx, sendParams{
		ToAgentID: params.ToAgentID,
		Body:      params.Body,
		Kind:      params.Kind,
		About:     params.About,
		System:    true,
	})
}

// familyID names a family, being a parent's own id and for a child the parent it is attached
// to.
func familyID(agent agentstore.Agent) string {
	if agent.ParentAgentID != nil {
		return *agent.ParentAgentID
	}

	return agent.ID
}

// requireSameFamily holds a child to its parent and that parent's other children, letting
// through the mail weave raises itself and the mail a person sends.
func requireSameFamily(sender *agentstore.Agent, recipient agentstore.Agent) error {
	if sender == nil {
		return nil
	}
	if sender.ParentAgentID == nil && recipient.ParentAgentID == nil {
		return nil
	}
	if familyID(*sender) == familyID(recipient) {
		return nil
	}

	return fmt.Errorf("%w: %q is outside %q's family, so reply to your parent rather than looking for another way around", ErrOutsideFamily, recipient.Name, sender.Name)
}

type sendParams struct {
	From      string
	ToAgentID string
	Body      string
	ReplyTo   *string
	Kind      mailstore.Kind
	About     *string
	// System marks mail weave raises itself rather than on a caller's behalf, which is what
	// puts weave's own name on it instead of a sender's.
	System bool
}

func (s *MailService) send(ctx context.Context, params sendParams) (*mailstore.Mail, error) {
	recipient, err := s.agents.GetByID(ctx, params.ToAgentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %q", ErrRecipientNotFound, params.ToAgentID)
	}
	if err != nil {
		return nil, err
	}
	if recipient.ArchivedAt != nil {
		return nil, fmt.Errorf("%w: %q", ErrAgentArchived, params.ToAgentID)
	}

	var sender *agentstore.Agent
	if params.From != "" {
		var err error
		sender, err = s.findSender(ctx, params.From)
		if err != nil {
			return nil, err
		}
	}
	if sender != nil && sender.ID == recipient.ID {
		return nil, ErrSelfAddressed
	}
	if err := requireSameFamily(sender, *recipient); err != nil {
		return nil, err
	}

	// The cap decides before the mailbox does: a full mailbox is a wait and the cap is permanent.
	if sender != nil && params.Kind == mailstore.KindPlain {
		if err := s.holdToTheExchangeCap(ctx, *sender, *recipient); err != nil {
			return nil, err
		}
	}

	pending, err := s.mailReadWriter.CountPending(ctx, recipient.ID)
	if err != nil {
		return nil, err
	}
	if pending >= maxPendingMail {
		return nil, fmt.Errorf("%w: %q is holding %d unread", ErrMailboxFull, recipient.Name, pending)
	}

	if params.ReplyTo != nil {
		if _, err := s.mailReadWriter.GetByID(ctx, *params.ReplyTo); errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("%w: %q", ErrReplyToNotFound, *params.ReplyTo)
		} else if err != nil {
			return nil, err
		}
	}

	deadline, err := s.config.GetDeliveryDeadline()
	if err != nil {
		return nil, err
	}

	send := mailstore.SendParams{
		ToAgentID:    recipient.ID,
		Body:         params.Body,
		Kind:         params.Kind,
		ReplyTo:      params.ReplyTo,
		AboutAgentID: params.About,
		Deadline:     deadline,
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

// holdToTheExchangeCap refuses a send once two agents have written each other as much mail as
// the limit allows, counting the pair rather than one thread of it.
func (s *MailService) holdToTheExchangeCap(ctx context.Context, sender, recipient agentstore.Agent) error {
	maxExchange, err := s.config.GetMaxExchange()
	if err != nil {
		return err
	}

	exchanged, err := s.mailReadWriter.CountExchanged(ctx, sender.ID, recipient.ID)
	if err != nil {
		return err
	}
	if int(exchanged) < maxExchange {
		return nil
	}

	s.tellTheCapWasReached(ctx, sender, recipient, exchanged)

	return fmt.Errorf("%w: you and %q have exchanged %d pieces of mail, which is the limit, so nothing further will get through between you; say where you got to and tell the person what is unfinished", ErrExchangeCapped, recipient.Name, exchanged)
}

// tellTheCapWasReached tells the recipient that mail meant for it was refused.
func (s *MailService) tellTheCapWasReached(ctx context.Context, sender, recipient agentstore.Agent, exchanged int64) {
	attrs := []slog.Attr{
		slog.String(logging.AgentID, sender.ID),
		slog.String(logging.TargetAgentID, recipient.ID),
		slog.Int64(logging.ExchangedMail, exchanged),
	}

	body := fmt.Sprintf(exchangeCappedBody, sender.Name, sender.ID, exchanged)
	if _, err := s.SendNotice(ctx, SendNoticeParams{
		ToAgentID: recipient.ID,
		Body:      body,
		Kind:      mailstore.KindExchangeCapped,
		About:     &sender.ID,
	}); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to tell an agent its mail was refused by the cap",
			append(attrs, logging.Err(err)...)...)
	}
}

// ClaimMail takes the mail waiting for an agent, emptying its mailbox rather than showing it.
func (s *MailService) ClaimMail(ctx context.Context, agentID string) ([]mailstore.Mail, error) {
	agent, err := s.agents.GetByID(ctx, agentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrAgentNotFound.Saying("no agent with id %q", agentID)
	}
	if err != nil {
		return nil, err
	}

	claimed, err := s.mailReadWriter.Claim(ctx, agent.ID, maxClaimedMail)
	if err != nil {
		return nil, err
	}

	return claimed, nil
}

type ListMailParams struct {
	AgentID string
	// Sent reads the mail the agent wrote rather than the mail written to it.
	Sent   bool
	Limit  int32
	Cursor *mailstore.Cursor
}

// ListMail returns one page of one half of an agent's correspondence, most recent first and
// without claiming any of it: the mail addressed to it, or the mail it sent.
func (s *MailService) ListMail(ctx context.Context, params ListMailParams) ([]mailstore.Mail, error) {
	agent, err := s.agents.GetByID(ctx, params.AgentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrAgentNotFound.Saying("no agent with id %q", params.AgentID)
	}
	if err != nil {
		return nil, err
	}

	read := s.mailReadWriter.ListPage
	if params.Sent {
		read = s.mailReadWriter.ListSentPage
	}

	listed, err := read(ctx, agent.ID, params.Limit, params.Cursor)
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
	sender, err := s.agents.GetByID(ctx, from)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %q", ErrSenderUnknown, from)
	}
	if err != nil {
		return nil, err
	}

	return sender, nil
}
