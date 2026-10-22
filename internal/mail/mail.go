package mail

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"komrad/internal/eventprompts"
	"komrad/internal/fail"
	"komrad/internal/logging"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	mailstore "komrad/internal/store/mail"
	reviewstore "komrad/internal/store/review"
	workflowstore "komrad/internal/store/workflow"
	"komrad/lib/xslice"
	"komrad/lib/xstr"
	"komrad/proto/gen/oneofs"
)

const maxPendingMail = 100

// MaxMailBody is the most characters one piece of mail carries.
const MaxMailBody = 10000

const maxClaimedMail = 20

var ErrRecipientNotFound = fail.NotFound("no agent answers to that address")

// errUnnamedRecipient is mail addressed to neither an agent's id nor its name.
var errUnnamedRecipient = fail.InvalidArgument("a recipient is named by its id or its name")

var ErrRecipientAmbiguous = fail.InvalidArgument("more than one live agent holds that name")

var ErrSenderUnknown = fail.InvalidArgument("the sender is not an agent this panel knows")

var ErrSelfAddressed = fail.InvalidArgument("an agent cannot send itself mail")

var ErrMailboxFull = fail.Exhausted("the recipient's mailbox is full")

var ErrExchangeCapped = fail.Exhausted("this pair of agents has reached its mail limit")

var ErrReplyToNotFound = fail.InvalidArgument("no mail holds that id")

var ErrMailNotFound = fail.NotFound("no mail holds that id, deleting nothing")

var ErrKindMissing = fail.InvalidArgument("mail from komrad must name a kind other than plain")

var ErrAlreadySent = fail.AlreadyExists("this idempotency key already sent its mail")

var ErrAgentNotFound = fail.NotFound("agent not found")

var ErrAgentArchived = fail.Precondition("agent is archived")

// ErrOutsideTask says a reviewer and an agent off its task tried to reach each other.
var ErrOutsideTask = fail.Denied("a reviewer mails and is mailed only by the agents on its task and its workflow's orchestrator")

const KomradSenderName = "komrad"

const namedMissing = 5

type mailConfigReader interface {
	GetDeliveryDeadline() (time.Duration, error)
	GetMaxExchange() (int, error)
}

type txRunner interface {
	InTx(ctx context.Context, fn func(store.DBTX) error) error
}

type NewMailServiceParams struct {
	Mail      *mailstore.MailStore
	Agents    *agentstore.AgentStore
	Workflows *workflowstore.WorkflowStore
	Config    mailConfigReader
	Panes     paneReader
	Tx        txRunner
}

type MailService struct {
	mail      *mailstore.MailStore
	agents    *agentstore.AgentStore
	workflows *workflowstore.WorkflowStore
	config    mailConfigReader
	panes     paneReader
	tx        txRunner
}

// NewMailService builds the service that sends, claims and lists the mail agents leave for
// each other.
func NewMailService(params NewMailServiceParams) *MailService {
	return &MailService{
		mail:      params.Mail,
		agents:    params.Agents,
		workflows: params.Workflows,
		config:    params.Config,
		panes:     params.Panes,
		tx:        params.Tx,
	}
}

// IsDormant says whether nothing runs behind an agent whose pane decides its state, so mail to it
// waits until it is resumed.
func (s *MailService) IsDormant(agent agentstore.Agent) bool {
	return agent.State.DoesPaneDecide() && !s.panes.IsAgentRunning(agent)
}

// getRecipient returns the agent an id names.
func (s *MailService) getRecipient(ctx context.Context, id string) (*agentstore.Agent, error) {
	recipient, err := s.agents.GetByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrRecipientNotFound.Saying("no agent holds the id %q", id)
	}
	if err != nil {
		return nil, err
	}

	return recipient, nil
}

type FindRecipientParams struct {
	From string
	To   oneofs.To
}

// FindRecipient returns the agent a sender addressed, live or archived when named by id, and the
// live agent of that name when named by name.
func (s *MailService) FindRecipient(ctx context.Context, params FindRecipientParams) (*agentstore.Agent, error) {
	switch {
	case params.To.ToAgentId != nil:
		return s.getRecipient(ctx, params.To.ToAgentId.String())
	case params.To.ToAgentName != nil:
		return s.resolveRecipient(ctx, params.From, *params.To.ToAgentName)
	}

	return nil, errUnnamedRecipient
}

// resolveRecipient returns the live agent a name names, looked for in the sender's own workflow
// before every workflow.
func (s *MailService) resolveRecipient(ctx context.Context, from, name string) (*agentstore.Agent, error) {
	named, err := s.agents.ListLiveNamed(ctx, name)
	if err != nil {
		return nil, err
	}

	if from != "" {
		sender, err := s.findSender(ctx, from)
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
		return nil, ErrRecipientNotFound.Saying("no live agent is named %q", name)
	case 1:
		return &named[0], nil
	}

	return nil, ErrRecipientAmbiguous.Saying("%d live agents are named %q, so address one of them by id: %s", len(named), name, s.describeHolders(ctx, named))
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

type SendScheduledMailParams struct {
	ToAgentID      string
	ScheduleName   string
	Body           string
	IdempotencyKey string
}

// SendScheduledMail sends a schedule's prompt on behalf of the person who wrote the schedule.
func (s *MailService) SendScheduledMail(ctx context.Context, params SendScheduledMailParams) (*mailstore.Mail, error) {
	sent, err := s.mail.GetByIdempotencyKey(ctx, params.IdempotencyKey)
	if err == nil {
		return sent, fmt.Errorf("%w: %q", ErrAlreadySent, params.IdempotencyKey)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	return s.send(ctx, sendParams{
		ToAgentID:      params.ToAgentID,
		Body:           params.Body,
		Kind:           mailstore.KindPlain,
		SenderName:     "schedule " + params.ScheduleName,
		IdempotencyKey: params.IdempotencyKey,
	})
}

var errSubjectMissing = fail.InvalidArgument("mail from komrad must name the subject it is about")

var ErrNothingUnread = fail.NotFound("the recipient holds no unread mail of this kind about this subject")

type SendFromKomradParams struct {
	ToAgentID string
	Mail      eventprompts.Mail
	// SubjectID is the agent, document, pull request or worktree the mail is about.
	SubjectID string
	// OnlyRewrite writes nothing when the recipient holds no unread mail of this kind about this subject.
	OnlyRewrite bool
	// Holding are the events true of the subject now, which keeps those an earlier unread mail started.
	Holding []string
}

// SendFromKomrad writes mail as komrad itself, rewriting the unread mail of the same kind about the
// same subject when the recipient still holds it.
func (s *MailService) SendFromKomrad(ctx context.Context, params SendFromKomradParams) (*mailstore.Mail, error) {
	if params.Mail == nil {
		return nil, fmt.Errorf("%w: mail from komrad needs something to say", ErrKindMissing)
	}
	kind := params.Mail.Kind()
	if kind == "" || kind == mailstore.KindPlain {
		return nil, fmt.Errorf("%w: mail from komrad needs a kind of its own to be told apart by", ErrKindMissing)
	}
	if params.SubjectID == "" {
		return nil, errSubjectMissing
	}

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

	held, err := s.mail.GetUnreadAbout(ctx, recipient.ID, kind, params.SubjectID)
	if errors.Is(err, store.ErrNotFound) && params.OnlyRewrite {
		return nil, ErrNothingUnread
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	workflow, err := s.getWorkflow(ctx, recipient.WorkflowID)
	if err != nil {
		return nil, err
	}

	prompts := eventprompts.Prompts(workflow.EventPrompts)
	if held == nil {
		if prompts.AreAllMuted(params.Mail.Events()) {
			return nil, ErrNothingUnread
		}
		if err := s.requireRoomFor(ctx, *recipient, kind); err != nil {
			return nil, err
		}
	}

	deadline, err := s.config.GetDeliveryDeadline()
	if err != nil {
		return nil, err
	}

	carried := carryEvents(held, params)

	return s.mail.UpsertFromKomrad(ctx, mailstore.UpsertFromKomradParams{
		FromName:  KomradSenderName,
		ToAgentID: recipient.ID,
		Body:      prompts.Append(params.Mail.Body(), carried, MaxMailBody),
		Kind:      kind,
		SubjectID: params.SubjectID,
		Events:    carried,
		Deadline:  deadline,
	})
}

// requireRoomFor refuses mail to a full mailbox, except a person's comment on the recipient's work,
// which the person's own mail never counts against.
func (s *MailService) requireRoomFor(ctx context.Context, recipient agentstore.Agent, kind mailstore.Kind) error {
	if kind == mailstore.KindReviewComment {
		return nil
	}

	return s.requireRoom(ctx, recipient)
}

// InTx is this service writing mail through a transaction.
func (s *MailService) InTx(db store.DBTX) *MailService {
	bound := *s
	bound.mail = mailstore.NewMailStore(db)
	bound.workflows = workflowstore.NewWorkflowStore(db)

	return &bound
}

func (s *MailService) requireRoom(ctx context.Context, recipient agentstore.Agent) error {
	pending, err := s.mail.CountPending(ctx, recipient.ID)
	if err != nil {
		return err
	}
	if pending >= maxPendingMail {
		return fmt.Errorf("%w: %q is holding %d unread", ErrMailboxFull, recipient.Name, pending)
	}

	return nil
}

// requireSameTask holds a reviewer to the agents on its task and its workflow's orchestrator, both
// ways, letting through the mail komrad raises itself and the mail a person sends.
func requireSameTask(sender *agentstore.Agent, recipient agentstore.Agent) error {
	switch {
	case sender == nil:
	case sender.Role != agentstore.RoleReviewer && recipient.Role != agentstore.RoleReviewer:
	case isOrchestratorOf(*sender, recipient) || isOrchestratorOf(recipient, *sender):
	case sender.TaskID != nil && recipient.TaskID != nil && *sender.TaskID == *recipient.TaskID:
	default:
		return fmt.Errorf("%w: %q and %q work on different tasks, so mail the agents on your own task or your workflow's orchestrator", ErrOutsideTask, sender.Name, recipient.Name)
	}

	return nil
}

func isOrchestratorOf(orchestrator, agent agentstore.Agent) bool {
	return orchestrator.Role == agentstore.RoleOrchestrator && orchestrator.WorkflowID == agent.WorkflowID
}

type sendParams struct {
	From      string
	ToAgentID string
	Body      string
	ReplyTo   *string
	Kind      mailstore.Kind
	// SenderName names mail sent on a person's behalf by something other than an agent.
	SenderName     string
	IdempotencyKey string
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
	if err := requireSameTask(sender, *recipient); err != nil {
		return nil, err
	}

	// The cap decides before the mailbox does: a full mailbox is a wait and the cap is permanent.
	if sender != nil && params.Kind == mailstore.KindPlain {
		if err := s.holdToTheExchangeCap(ctx, *sender, *recipient); err != nil {
			return nil, err
		}
	}

	if err := s.requireRoom(ctx, *recipient); err != nil {
		return nil, err
	}

	if params.ReplyTo != nil {
		if _, err := s.mail.GetByID(ctx, *params.ReplyTo); errors.Is(err, store.ErrNotFound) {
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
		ToAgentID: recipient.ID,
		Body:      params.Body,
		Kind:      params.Kind,
		ReplyTo:   params.ReplyTo,
		Deadline:  deadline,
	}
	if params.IdempotencyKey != "" {
		send.IdempotencyKey = &params.IdempotencyKey
	}
	switch {
	case sender != nil:
		send.FromAgentID = &sender.ID
		send.FromName = sender.Name
	default:
		send.FromName = params.SenderName
	}

	sent, err := s.mail.Send(ctx, send)
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

	exchanged, err := s.mail.CountExchanged(ctx, sender.ID, recipient.ID)
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

	if _, err := s.SendFromKomrad(ctx, SendFromKomradParams{
		ToAgentID: recipient.ID,
		Mail: eventprompts.ExchangeCapped{
			SenderName: sender.Name,
			SenderID:   sender.ID,
			Exchanged:  exchanged,
		},
		SubjectID: sender.ID,
	}); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to tell an agent its mail was refused by the cap",
			append(attrs, logging.Err(err)...)...)
	}
}

// ListWaitingMail is the mail waiting for an agent, as much of it as one claim takes, oldest
// first, without claiming any of it.
func (s *MailService) ListWaitingMail(ctx context.Context, agentID string) ([]mailstore.Mail, error) {
	agent, err := s.getAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}

	return s.mail.ListWaiting(ctx, agent.ID, maxClaimedMail)
}

// ClaimMailParams is the waiting mail an agent takes and the snapshot it is read on.
type ClaimMailParams struct {
	AgentID string
	// IDs is the mail to claim, as ListWaitingMail listed it.
	IDs []string
	// ReadSnapshotID is the snapshot taken of the agent's task before the claim, which the comments
	// its review_comment mail carries are read on, nil when the task has no worktree.
	ReadSnapshotID *string
}

// ClaimMail takes the given mail waiting for an agent, emptying it from the mailbox rather than
// showing it, and in the same transaction stamps the comments its review_comment mail carries as
// read on the snapshot.
func (s *MailService) ClaimMail(ctx context.Context, params ClaimMailParams) ([]mailstore.Mail, error) {
	agent, err := s.getAgent(ctx, params.AgentID)
	if err != nil {
		return nil, err
	}

	var claimed []mailstore.Mail
	err = s.tx.InTx(ctx, func(tx store.DBTX) error {
		var err error
		claimed, err = mailstore.NewMailStore(tx).ClaimByID(ctx, agent.ID, params.IDs)
		if err != nil {
			return err
		}

		comments := xslice.Filter(claimed, func(m mailstore.Mail) bool { return m.Kind == mailstore.KindReviewComment })
		if len(comments) == 0 {
			return nil
		}

		ids := xslice.Map(comments, func(m mailstore.Mail) string { return m.ID })
		_, err = reviewstore.NewReviewStore(tx).MarkMessagesRead(ctx, ids, params.ReadSnapshotID)

		return err
	})
	if err != nil {
		return nil, err
	}

	return claimed, nil
}

// getAgent is the agent a mailbox belongs to.
func (s *MailService) getAgent(ctx context.Context, agentID string) (*agentstore.Agent, error) {
	agent, err := s.agents.GetByID(ctx, agentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrAgentNotFound.Saying("no agent with id %q", agentID)
	}
	if err != nil {
		return nil, err
	}

	return agent, nil
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
	agent, err := s.getAgent(ctx, params.AgentID)
	if err != nil {
		return nil, err
	}

	read := s.mail.ListPage
	if params.Sent {
		read = s.mail.ListSentPage
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
	found, err := s.mail.ListByID(ctx, ids)
	if err != nil {
		return fmt.Errorf("failed to look up %d mail: %w", len(ids), err)
	}
	if len(found) != len(ids) {
		present := xslice.KeyBy(found, func(mail mailstore.Mail) string { return mail.ID })

		missing := xslice.Filter(ids, func(id string) bool {
			_, ok := present[id]

			return !ok
		})

		return fmt.Errorf("%w: %s", ErrMailNotFound, xstr.JoinSome(missing, namedMissing))
	}

	if err := s.mail.Delete(ctx, ids); err != nil {
		return fmt.Errorf("failed to delete %d mail: %w", len(ids), err)
	}

	return nil
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

func carryEvents(held *mailstore.Mail, params SendFromKomradParams) []string {
	var carried []string
	if held != nil {
		carried = slices.DeleteFunc(slices.Clone(held.Events), func(event string) bool { return !slices.Contains(params.Holding, event) })
	}
	for _, event := range params.Mail.Events() {
		if !slices.Contains(carried, event) {
			carried = append(carried, event)
		}
	}

	return carried
}

func (s *MailService) getWorkflow(ctx context.Context, workflowID string) (*workflowstore.Workflow, error) {
	workflow, err := s.workflows.Get(ctx, workflowID)
	if errors.Is(err, store.ErrNotFound) {
		return &workflowstore.Workflow{}, nil
	}

	return workflow, err
}
