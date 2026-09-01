package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"weave/internal/harness"
	"weave/internal/logging"
	"weave/internal/store"
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

// nudgeRetryAfter is how long a nudge stands before unread mail is taken as evidence it went
// nowhere, since keys typed at a pane can be swallowed silently and a nudge that landed is
// answered in seconds.
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
	agentGetter
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

// DeliverAll is one pass over every mailbox holding mail, working from the rows each time so
// that a missed pass costs a delay and nothing else.
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
	if errors.Is(err, store.ErrNotFound) {
		return DeliveryDone, nil
	}
	if err != nil {
		return DeliveryDone, err
	}
	if agent.ArchivedAt != nil {
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

// canPaneTakeKeys reports whether the pane is showing the harness's own empty idle prompt,
// which is the only frame seen to take both the line and the newline that submits it.
func (s *DeliveryService) canPaneTakeKeys(agent agentstore.Agent) bool {
	hn, err := s.harnesses.GetByID(agent.Harness)
	if err != nil {
		// No frame of an unknown harness can be recognised, so its mail would otherwise never go
		// anywhere.
		return true
	}

	// A CLI still starting up holds the line in the terminal buffer but loses the newline, so
	// the line sits in the prompt unsent until the retry, and a prompt already holding typed
	// text is not idle either, since the newline would send whatever somebody was halfway
	// through writing along with the nudge.
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
