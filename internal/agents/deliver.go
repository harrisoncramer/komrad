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

// nudgeRetryAfter is how long a nudge stands before unread mail is taken as evidence it went
// nowhere.
const nudgeRetryAfter = time.Minute

type paneReader interface {
	IsAgentRunning(agent agentstore.Agent) bool
}

type nudgeClaimer interface {
	agentGetter
	ClaimNudge(ctx context.Context, id string, retryAfter time.Duration) (bool, error)
	SetNudgeSent(ctx context.Context, id string) error
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
	channel       harness.Channel
}

type NewDeliveryServiceParams struct {
	MailboxReader mailboxReader
	NudgeClaimer  nudgeClaimer
	PaneReader    paneReader
	Channel       harness.Channel
}

// NewDeliveryService builds the service from its dependencies.
func NewDeliveryService(params NewDeliveryServiceParams) *DeliveryService {
	return &DeliveryService{
		mailboxReader: params.MailboxReader,
		nudgeClaimer:  params.NudgeClaimer,
		paneReader:    params.PaneReader,
		channel:       params.Channel,
	}
}

// DeliverAll is one pass over every mailbox holding mail, working from the rows each time.
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
// what it managed.
func (s *DeliveryService) Deliver(ctx context.Context, agentID string) (Delivery, error) {
	agent, err := s.nudgeClaimer.GetByID(ctx, agentID)
	if errors.Is(err, store.ErrNotFound) {
		s.channel.Forget(agentID)

		return DeliveryDone, nil
	}
	if err != nil {
		return DeliveryDone, err
	}
	if agent.ArchivedAt != nil {
		s.channel.Forget(agentID)

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

	// A held agent is one a person has stopped to take its worktree, so nothing may type at it
	// until they let it go.
	if agent.HeldAt != nil {
		return DeliveryLater, nil
	}

	switch {
	case agent.State == agentstore.StateThinking && !isStateUnconfirmed(*agent):
		return DeliverySoon, nil
	case agent.State == agentstore.StateWaiting:
		return DeliveryLater, nil
	case !s.paneReader.IsAgentRunning(*agent):
		return DeliveryLater, nil
	}

	ready, err := s.channel.Ready(ctx, buildTarget(*agent))
	if err != nil {
		return DeliveryLater, err
	}
	if !ready {
		return DeliveryLater, nil
	}

	claimed, err := s.nudgeClaimer.ClaimNudge(ctx, agent.ID, nudgeRetryAfter)
	if err != nil {
		return DeliveryLater, err
	}
	if !claimed {
		return DeliveryLater, nil
	}

	// The claim is spent whether or not the nudge landed.
	delivered, err := s.nudge(ctx, *agent, pending)
	if err != nil {
		return DeliveryLater, err
	}
	if !delivered {
		return DeliveryLater, nil
	}

	if err := s.nudgeClaimer.SetNudgeSent(ctx, agent.ID); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to record the nudge weave sent an agent, so the next one will read as a first",
			append(logging.Err(err), slog.String(logging.TargetAgentID, agent.ID))...)
	}

	return DeliverySoon, nil
}

// nudge tells an agent about its mail, reporting whether the line was taken.
func (s *DeliveryService) nudge(ctx context.Context, agent agentstore.Agent, pending int64) (bool, error) {
	line := fmt.Sprintf("weave: %d piece(s) of unread mail, call read_mail", pending)

	sent, err := s.channel.Send(ctx, buildTarget(agent), line)
	if err != nil {
		return false, err
	}
	if !sent {
		return false, nil
	}

	attrs := []slog.Attr{
		slog.String(logging.TargetAgentID, agent.ID),
		slog.Int64(logging.UnreadMail, pending),
	}

	if didLastNudgeGoNowhere(agent) {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "told an agent to read its mail again, so the last time went nowhere", attrs...)
	} else {
		logging.From(ctx).LogAttrs(ctx, slog.LevelInfo, "told an agent to read its mail", attrs...)
	}

	return true, nil
}

// didLastNudgeGoNowhere reports whether the agent was told about mail before and has ended no
// turn since.
func didLastNudgeGoNowhere(agent agentstore.Agent) bool {
	if agent.NudgeSentAt == nil {
		return false
	}

	return agent.TurnEndedAt == nil || !agent.TurnEndedAt.After(*agent.NudgeSentAt)
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
