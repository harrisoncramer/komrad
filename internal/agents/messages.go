package agents

import (
	"context"
	"errors"
	"fmt"
	"time"

	agentstore "weave/internal/store/agent"
	messagestore "weave/internal/store/message"
)

const maxPendingMessages = 100

const maxClaimedMessages = 20

var ErrRecipientNotFound = errors.New("no agent holds that id")

var ErrSenderUnknown = errors.New("the sender is not an agent this panel knows")

var ErrSelfAddressed = errors.New("an agent cannot send itself a message")

var ErrMailboxFull = errors.New("the recipient's mailbox is full")

var ErrThreadCapped = errors.New("this thread has reached its hop limit")

var ErrReplyToNotFound = errors.New("no message holds that id")

// weaveSenderName marks a message as weave's own voice rather than the panel operator's,
// since both send with no agent id and nothing else tells them apart downstream.
const weaveSenderName = "weave"

type messageReadWriter interface {
	Send(ctx context.Context, params messagestore.SendParams) (*messagestore.Message, error)
	GetByID(ctx context.Context, id string) (*messagestore.Message, error)
	Claim(ctx context.Context, toAgentID string, limit int32) ([]messagestore.Message, error)
	CountPending(ctx context.Context, toAgentID string) (int64, error)
}

// deadlineReader is how long a message stays deliverable, read per send so editing the
// config reaches the next message rather than the next restart.
type deadlineReader interface {
	GetDeliveryDeadline() (time.Duration, error)
}

// hopLimitReader is how many messages may pass along one reply thread, read per send the
// way the deadline is.
type hopLimitReader interface {
	GetMaxHops() (int, error)
}

type agentFinder interface {
	GetByID(ctx context.Context, id string) (*agentstore.Agent, error)
}

type MessageService struct {
	messageReadWriter messageReadWriter
	agentFinder       agentFinder
	deadlineReader    deadlineReader
	hopLimitReader    hopLimitReader
}

type NewMessageServiceParams struct {
	MessageReadWriter messageReadWriter
	AgentFinder       agentFinder
	DeadlineReader    deadlineReader
	HopLimitReader    hopLimitReader
}

func NewMessageService(p NewMessageServiceParams) *MessageService {
	return &MessageService{
		messageReadWriter: p.MessageReadWriter,
		agentFinder:       p.AgentFinder,
		deadlineReader:    p.DeadlineReader,
		hopLimitReader:    p.HopLimitReader,
	}
}

type SendMessageParams struct {
	From      string
	ToAgentID string
	Body      string
	ReplyTo   *string
	// System marks the message as weave's own, not a person's or another agent's. Nothing
	// outside this package may set it: the tool surface that lets an agent or the panel send
	// mail has no such field, so this is reachable only from weave's own Go callers.
	System bool
}

func (s *MessageService) SendMessage(ctx context.Context, params SendMessageParams) (*messagestore.Message, error) {
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

	pending, err := s.messageReadWriter.CountPending(ctx, recipient.ID)
	if err != nil {
		return nil, err
	}
	if pending >= maxPendingMessages {
		return nil, fmt.Errorf("%w: %q is holding %d unread", ErrMailboxFull, recipient.Name, pending)
	}

	hops := int32(1)
	if params.ReplyTo != nil {
		parent, err := s.messageReadWriter.GetByID(ctx, *params.ReplyTo)
		if err != nil {
			return nil, err
		}
		if parent == nil {
			return nil, fmt.Errorf("%w: %q", ErrReplyToNotFound, *params.ReplyTo)
		}
		hops = parent.Hops + 1
	}

	maxHops, err := s.hopLimitReader.GetMaxHops()
	if err != nil {
		return nil, err
	}
	if int(hops) > maxHops {
		return nil, fmt.Errorf("%w: %d hops is the limit for this thread, say so and stop rather than replying again", ErrThreadCapped, maxHops)
	}

	deadline, err := s.deadlineReader.GetDeliveryDeadline()
	if err != nil {
		return nil, err
	}

	send := messagestore.SendParams{
		ToAgentID: recipient.ID,
		Body:      params.Body,
		ReplyTo:   params.ReplyTo,
		Hops:      hops,
		ExpiresAt: time.Now().Add(deadline),
	}
	if sender != nil {
		send.FromAgentID = &sender.ID
		send.FromName = sender.Name
	} else if params.System {
		send.FromName = weaveSenderName
	}

	sent, err := s.messageReadWriter.Send(ctx, send)
	if err != nil {
		return nil, err
	}

	return sent, nil
}

func (s *MessageService) ClaimMessages(ctx context.Context, agentID string) ([]messagestore.Message, error) {
	agent, err := s.agentFinder.GetByID(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, fmt.Errorf("%w: %q", ErrAgentNotFound, agentID)
	}

	claimed, err := s.messageReadWriter.Claim(ctx, agent.ID, maxClaimedMessages)
	if err != nil {
		return nil, err
	}

	return claimed, nil
}

func (s *MessageService) findSender(ctx context.Context, from string) (*agentstore.Agent, error) {
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
