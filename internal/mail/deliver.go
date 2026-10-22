package mail

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"komrad/internal/harness"
	"komrad/internal/logging"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	mailstore "komrad/internal/store/mail"
)

type Delivery int

const (
	DeliveryDone Delivery = iota
	DeliverySoon
	DeliveryLater
)

// nudgeRetryAfter is how long a nudge stands before unread mail counts as evidence it went
// nowhere.
const nudgeRetryAfter = time.Minute

type paneReader interface {
	IsAgentRunning(agent agentstore.Agent) bool
}

// treeReader says whether komrad is moving the checkout at a directory.
type treeReader interface {
	IsMoving(dir string) bool
}

type nudgeClaimer interface {
	GetByID(ctx context.Context, id string) (*agentstore.Agent, error)
	ClaimNudge(ctx context.Context, id string, retryAfter time.Duration) (bool, error)
}

type NewDeliveryServiceParams struct {
	Mail         *mailstore.MailStore
	NudgeClaimer nudgeClaimer
	PaneReader   paneReader
	Channel      harness.Channel
	Trees        treeReader
}

type DeliveryService struct {
	mail         *mailstore.MailStore
	nudgeClaimer nudgeClaimer
	paneReader   paneReader
	channel      harness.Channel
	trees        treeReader
}

// NewDeliveryService builds the service from its dependencies.
func NewDeliveryService(params NewDeliveryServiceParams) *DeliveryService {
	return &DeliveryService{
		mail:         params.Mail,
		nudgeClaimer: params.NudgeClaimer,
		paneReader:   params.PaneReader,
		channel:      params.Channel,
		trees:        params.Trees,
	}
}

// buildTarget names an agent the way a channel reaches it.
func buildTarget(agent agentstore.Agent) harness.Target {
	return harness.Target{
		AgentID:   agent.ID,
		Pane:      agent.Pane,
		HarnessID: agent.Harness,
	}
}

// DeliverAll is one pass over every mailbox holding mail, working from the rows each time.
func (s *DeliveryService) DeliverAll(ctx context.Context) error {
	mailboxes, err := s.mail.ListPendingMailboxes(ctx)
	if err != nil {
		return err
	}

	var errs []error
	for _, agentID := range mailboxes {
		if _, err := s.Deliver(ctx, agentID); err != nil {
			errs = append(errs, fmt.Errorf("failed to deliver to agent %q: %w", agentID, err))
		}
	}

	return errors.Join(errs...)
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

	pending, err := s.mail.CountPending(ctx, agent.ID)
	if err != nil {
		return DeliveryDone, err
	}
	if pending == 0 {
		s.reportExpired(ctx, *agent)

		return DeliveryDone, nil
	}

	if s.trees != nil && agent.Cwd != "" && s.trees.IsMoving(agent.Cwd) {
		return DeliveryLater, nil
	}

	switch {
	case agent.State == agentstore.StateThinking && !agent.IsStateUnconfirmed():
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

	return DeliverySoon, nil
}

// ClaimTurnEndNudge takes the nudge for an agent whose turn just ended, returning the line to hand
// it, or empty when nothing waits or the nudge is already out.
func (s *DeliveryService) ClaimTurnEndNudge(ctx context.Context, agentID string) (string, error) {
	agent, err := s.nudgeClaimer.GetByID(ctx, agentID)
	if err != nil {
		return "", err
	}

	pending, err := s.mail.CountPending(ctx, agent.ID)
	if err != nil {
		return "", err
	}
	if pending == 0 {
		return "", nil
	}

	claimed, err := s.nudgeClaimer.ClaimNudge(ctx, agent.ID, nudgeRetryAfter)
	if err != nil {
		return "", err
	}
	if !claimed {
		return "", nil
	}

	logging.From(ctx).LogAttrs(ctx, slog.LevelInfo, "told an agent to read its mail as its turn ended",
		slog.String(logging.TargetAgentID, agent.ID),
		slog.Int64(logging.UnreadMail, pending))

	return formatNudge(pending), nil
}

// nudge tells an agent about its mail, reporting whether the line was taken.
func (s *DeliveryService) nudge(ctx context.Context, agent agentstore.Agent, pending int64) (bool, error) {
	sent, err := s.channel.Send(ctx, buildTarget(agent), formatNudge(pending))
	if err != nil {
		return false, err
	}
	if !sent {
		return false, nil
	}

	logging.From(ctx).LogAttrs(ctx, slog.LevelInfo, "told an agent to read its mail",
		slog.String(logging.TargetAgentID, agent.ID),
		slog.Int64(logging.UnreadMail, pending))

	return true, nil
}

func formatNudge(pending int64) string {
	return fmt.Sprintf("komrad: %d piece(s) of unread mail, call read_mail", pending)
}

func (s *DeliveryService) reportExpired(ctx context.Context, agent agentstore.Agent) {
	expired, err := s.mail.ListExpired(ctx, agent.ID)
	if err != nil || len(expired) == 0 {
		return
	}

	for _, item := range expired {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "a piece of mail expired before it was read",
			slog.String(logging.TargetAgentID, agent.ID),
			slog.String(logging.MailID, item.ID))
	}
}
