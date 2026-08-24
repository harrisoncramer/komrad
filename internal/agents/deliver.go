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

type Delivery int

const (
	DeliveryDone Delivery = iota

	DeliverySoon

	DeliveryLater
)

const nudgeEnterDelay = 300 * time.Millisecond

type paneReader interface {
	IsAgentRunning(agent agentstore.Agent) bool
}

type paneTyper interface {
	IsPaneInMode(pane string) bool
	SendText(target, text string) error
	SendKeys(target string, keys ...string) error
}

type nudgeClaimer interface {
	agentFinder
	ClaimNudge(ctx context.Context, id string) (bool, error)
}

type mailboxReader interface {
	ListPendingMailboxes(ctx context.Context) ([]string, error)
	CountPending(ctx context.Context, toAgentID string) (int64, error)
	ListExpired(ctx context.Context, toAgentID string) ([]mailstore.Mail, error)
}

type DeliveryService struct {
	mailboxReader mailboxReader
	nudgeClaimer  nudgeClaimer
	paneReader    paneReader
	paneTyper     paneTyper
}

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
	case agent.State == agentstore.StateThinking && !isStateUnconfirmed(*agent):
		return DeliverySoon, nil
	case agent.State == agentstore.StateWaiting:
		return DeliveryLater, nil
	case !s.paneReader.IsAgentRunning(*agent):
		return DeliveryLater, nil
	case s.paneTyper.IsPaneInMode(agent.Pane):

		return DeliveryLater, nil
	}

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
