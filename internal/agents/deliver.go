package agents

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"weave/internal/harness"
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

// nudgeRetryAfter is how long a nudge stands before the mail still being unread is taken as
// evidence it went nowhere. Keys typed at a pane can be swallowed with nothing to say so, and
// an agent that never saw the line has no reason to move, so an agent moving is not something
// this can wait for. A nudge that landed is answered in seconds, so a minute is long enough
// that mail outliving one means it was lost.
const nudgeRetryAfter = time.Minute

type paneReader interface {
	IsAgentRunning(agent agentstore.Agent) bool
}

type paneTyper interface {
	IsPaneInMode(pane string) bool
	CapturePane(pane string) string
	SendText(target, text string) error
	SendKeys(target string, keys ...string) error
}

type nudgeClaimer interface {
	agentFinder
	ClaimNudge(ctx context.Context, id string, retryAfter time.Duration) (bool, error)
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
	harnesses     harnessGetter
}

type NewDeliveryServiceParams struct {
	MailboxReader mailboxReader
	NudgeClaimer  nudgeClaimer
	PaneReader    paneReader
	PaneTyper     paneTyper
	Harnesses     harnessGetter
}

// NewDeliveryService builds the service from its dependencies.
func NewDeliveryService(p NewDeliveryServiceParams) *DeliveryService {
	return &DeliveryService{
		mailboxReader: p.MailboxReader,
		nudgeClaimer:  p.NudgeClaimer,
		paneReader:    p.PaneReader,
		paneTyper:     p.PaneTyper,
		harnesses:     p.Harnesses,
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
	case !s.canPaneTakeKeys(*agent):
		return DeliveryLater, nil
	}

	claimed, err := s.nudgeClaimer.ClaimNudge(ctx, agent.ID, nudgeRetryAfter)
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

// canPaneTakeKeys reports whether the pane is showing the harness's own idle prompt, which is
// the only frame that has been seen to take both the line and the newline that submits it. A
// CLI still coming up holds keys typed at it in the terminal buffer and reads them once it is
// ready, but the newline does not survive that wait: the line lands in the prompt and sits
// there unsent until the retry a minute later. The pane owning its process says nothing about
// this, since the pane is there from the moment it opens.
//
// A harness this binary cannot read is let through, since no frame of its can ever be
// recognised and mail to it would otherwise never go anywhere.
func (s *DeliveryService) canPaneTakeKeys(agent agentstore.Agent) bool {
	hn, err := s.harnesses.GetByID(agent.Harness)
	if err != nil {
		return true
	}

	return hn.ReadActivity(s.paneTyper.CapturePane(agent.Pane)) == harness.ActivityIdle
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

	level := slog.LevelInfo
	message := "told an agent to read its mail"
	if agent.NudgedAt != nil {
		level = slog.LevelWarn
		message = "told an agent to read its mail again, so the last time went nowhere"
	}

	logging.From(ctx).LogAttrs(ctx, level, message,
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
