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

// treeReader says whether komrad is moving the checkout at a directory.
type treeReader interface {
	IsMoving(dir string) bool
}

type nudgeClaimer interface {
	GetByID(ctx context.Context, id string) (*agentstore.Agent, error)
	ClaimNudge(ctx context.Context, id string) (bool, error)
	ClearNudge(ctx context.Context, id string) error
}

type harnessGetter interface {
	GetByID(id string) (harness.Harness, error)
}

type NewDeliveryServiceParams struct {
	Mail         *mailstore.MailStore
	NudgeClaimer nudgeClaimer
	Harnesses    harnessGetter
	Trees        treeReader
	Now          func() time.Time
}

type DeliveryService struct {
	mail         *mailstore.MailStore
	nudgeClaimer nudgeClaimer
	harnesses    harnessGetter
	trees        treeReader
	now          func() time.Time
}

// NewDeliveryService builds the service from its dependencies.
func NewDeliveryService(params NewDeliveryServiceParams) *DeliveryService {
	now := params.Now
	if now == nil {
		now = time.Now
	}

	return &DeliveryService{
		mail:         params.Mail,
		nudgeClaimer: params.NudgeClaimer,
		harnesses:    params.Harnesses,
		trees:        params.Trees,
		now:          now,
	}
}

// buildTarget names an agent the way a channel reaches it.
func buildTarget(agent agentstore.Agent) harness.Target {
	return harness.Target{
		AgentID:   agent.ID,
		Pane:      agent.Pane,
		HarnessID: agent.Harness,
		SessionID: agent.SessionID,
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

// Deliver makes one attempt at telling an agent about what is waiting for it, and reports what
// it managed.
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
	if agent.State == agentstore.StateWaiting {
		return DeliveryLater, nil
	}

	claimed, err := s.nudgeClaimer.ClaimNudge(ctx, agent.ID)
	if err != nil {
		return DeliveryLater, err
	}
	if !claimed {
		return DeliveryLater, nil
	}

	delivered, err := s.nudge(ctx, *agent, pending)
	if err != nil {
		return DeliveryLater, err
	}
	if !delivered {
		return DeliveryLater, nil
	}

	return DeliverySoon, nil
}

// nudge tells an agent about its mail and gives the claim back when the line reached nothing.
func (s *DeliveryService) nudge(ctx context.Context, agent agentstore.Agent, pending int64) (bool, error) {
	hn, err := s.harnesses.GetByID(agent.Harness)
	if err != nil {
		return false, err
	}

	sent, err := hn.Channel().Send(ctx, buildTarget(agent), formatNudge(pending, s.now()))
	if err != nil {
		return false, err
	}
	if !sent {
		return false, s.nudgeClaimer.ClearNudge(ctx, agent.ID)
	}

	logging.From(ctx).LogAttrs(ctx, slog.LevelInfo, "told an agent to read its mail",
		slog.String(logging.TargetAgentID, agent.ID),
		slog.Int64(logging.UnreadMail, pending))

	return true, nil
}

// formatNudge stamps the line with the time, since Claude drops a line identical to the last one it
// took from the same sender.
func formatNudge(pending int64, at time.Time) string {
	return fmt.Sprintf("komrad: %d piece(s) of unread mail at %s, call read_mail", pending, at.Format(time.TimeOnly))
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
