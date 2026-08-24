package agents

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"weave/internal/logging"
	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
)

// Delivery is what one pass at an agent's mailbox achieved, which is what decides when the
// next pass should be.
type Delivery int

const (
	// DeliveryDone means the mailbox holds nothing deliverable, so nothing has to come back
	// to it.
	DeliveryDone Delivery = iota
	// DeliverySoon means mail is waiting on something about to change: an agent mid-turn, or
	// one that has just been told to read.
	DeliverySoon
	// DeliveryLater means mail is waiting on something that may not change for a while: an
	// agent with no pane, one sitting on a permission prompt, one whose pane a person is
	// reading back through, or one that has already been told once and has not moved since.
	DeliveryLater
)

// nudgeEnterDelay is how long the pane is given to take the typed line before the newline
// that submits it. Sent together, the agent's input box can still be catching up and the
// newline is dropped.
const nudgeEnterDelay = 300 * time.Millisecond

// paneReader is how delivery finds out whether an agent's pane is still there to type into.
type paneReader interface {
	IsAgentRunning(agent agentstore.Agent) bool
}

// paneTyper is how the nudge gets into a pane: whether the pane will take keys at all,
// then the line as text and the newline that submits it as a key.
type paneTyper interface {
	IsPaneInMode(pane string) bool
	SendText(target, text string) error
	SendKeys(target string, keys ...string) error
}

// nudgeClaimer reads an agent and takes the right to tell it about waiting mail.
type nudgeClaimer interface {
	agentFinder
	ClaimNudge(ctx context.Context, id string) (bool, error)
}

// mailboxReader is what delivery needs to know about an agent's mail without taking it.
// Nothing here claims mail: what a recipient reads is the recipient's own call. ListExpired is
// the one exception to "reader" in the name — it marks what it reports, so a piece of mail is
// only ever logged as expired once.
type mailboxReader interface {
	ListPendingMailboxes(ctx context.Context) ([]string, error)
	CountPending(ctx context.Context, toAgentID string) (int64, error)
	ListExpired(ctx context.Context, toAgentID string) ([]mailstore.Mail, error)
}

// DeliveryService gets an agent to read its mail. It does not deliver the mail itself: the
// hooks an agent CLI runs do that, and this is what makes sure something runs them, by
// typing at an agent that has stopped and would otherwise never look.
type DeliveryService struct {
	mailboxReader mailboxReader
	nudgeClaimer  nudgeClaimer
	paneReader    paneReader
	paneTyper     paneTyper
}

// NewDeliveryServiceParams carries the delivery service's dependencies.
type NewDeliveryServiceParams struct {
	MailboxReader mailboxReader
	NudgeClaimer  nudgeClaimer
	PaneReader    paneReader
	PaneTyper     paneTyper
}

// NewDeliveryService builds the service from its dependencies.
func NewDeliveryService(p NewDeliveryServiceParams) *DeliveryService {
	return &DeliveryService{
		mailboxReader: p.MailboxReader,
		nudgeClaimer:  p.NudgeClaimer,
		paneReader:    p.PaneReader,
		paneTyper:     p.PaneTyper,
	}
}

// DeliverAll is one pass over every mailbox holding mail. It re-derives what to do from the
// rows each time rather than from anything it remembers, so a pass that was missed costs a
// delay and nothing else.
func (s *DeliveryService) DeliverAll(ctx context.Context) error {
	mailboxes, err := s.mailboxReader.ListPendingMailboxes(ctx)
	if err != nil {
		return err
	}

	for _, agentID := range mailboxes {
		if _, err := s.Deliver(ctx, agentID); err != nil {
			return err
		}
	}

	return nil
}

// Deliver makes one attempt at getting an agent to read what is waiting for it, and reports
// what it managed, which is what the tests assert on.
func (s *DeliveryService) Deliver(ctx context.Context, agentID string) (Delivery, error) {
	agent, err := s.nudgeClaimer.GetByID(ctx, agentID)
	if err != nil {
		return DeliveryDone, err
	}
	if agent == nil || agent.ArchivedAt != nil {
		return DeliveryDone, nil
	}

	pending, err := s.mailboxReader.CountPending(ctx, agent.ID)
	if err != nil {
		return DeliveryDone, err
	}
	if pending == 0 {
		s.reportExpired(ctx, *agent)

		return DeliveryDone, nil
	}

	switch {
	case agent.State == agentstore.StateThinking:
		return DeliverySoon, nil
	case agent.State == agentstore.StateWaiting:
		return DeliveryLater, nil
	case !s.paneReader.IsAgentRunning(*agent):
		return DeliveryLater, nil
	case s.paneTyper.IsPaneInMode(agent.Pane):
		// Keys sent to a pane in a mode reach the mode rather than the agent. Answering before
		// the claim is taken is what leaves the mail to be delivered once the mode is over.
		return DeliveryLater, nil
	}

	// The claim is what stops an agent being typed at twice for one stretch of sitting idle,
	// and it is taken before anything is typed so two passes racing cannot both go on.
	claimed, err := s.nudgeClaimer.ClaimNudge(ctx, agent.ID)
	if err != nil {
		return DeliveryLater, err
	}
	if !claimed {
		return DeliveryLater, nil
	}

	if err := s.nudge(ctx, *agent, pending); err != nil {
		return DeliveryLater, err
	}

	return DeliverySoon, nil
}

// nudge types one line into the agent's pane telling it to read its mail. The mail itself
// stays in the store: a long or multi-line message typed at a pane is what corrupts, and
// the agent can fetch it in full over the API.
func (s *DeliveryService) nudge(ctx context.Context, agent agentstore.Agent, pending int64) error {
	line := fmt.Sprintf("weave: %d piece(s) of unread mail, call read_mail", pending)

	if err := s.paneTyper.SendText(agent.Pane, line); err != nil {
		return fmt.Errorf("failed to type at agent %q: %w", agent.ID, err)
	}

	time.Sleep(nudgeEnterDelay)

	if err := s.paneTyper.SendKeys(agent.Pane, "Enter"); err != nil {
		return fmt.Errorf("failed to submit the nudge to agent %q: %w", agent.ID, err)
	}

	logging.From(ctx).LogAttrs(ctx, slog.LevelInfo, "told an agent to read its mail",
		slog.String(logging.TargetAgentID, agent.ID),
		slog.Int64(logging.UnreadMail, pending))

	return nil
}

// reportExpired writes a line for each piece of mail that ran out of time, since mail nobody
// ever saw is otherwise indistinguishable from mail that was never sent.
func (s *DeliveryService) reportExpired(ctx context.Context, agent agentstore.Agent) {
	expired, err := s.mailboxReader.ListExpired(ctx, agent.ID)
	if err != nil || len(expired) == 0 {
		return
	}

	for _, item := range expired {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "a piece of mail expired before it was read",
			slog.String(logging.TargetAgentID, agent.ID),
			slog.String(logging.MailID, item.ID))
	}
}
