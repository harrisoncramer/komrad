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

type messageReadWriter interface {
	Send(ctx context.Context, params messagestore.SendParams) (*messagestore.Message, error)
	Claim(ctx context.Context, toAgentID string, limit int32) ([]messagestore.Message, error)
	CountPending(ctx context.Context, toAgentID string) (int64, error)
}

// deadlineReader is how long a message stays deliverable, read per send so editing the
// config reaches the next message rather than the next restart.
type deadlineReader interface {
	GetDeliveryDeadline() (time.Duration, error)
}

type agentFinder interface {
	GetByID(ctx context.Context, id string) (*agentstore.Agent, error)
}

type MessageService struct {
	messageReadWriter messageReadWriter
	agentFinder       agentFinder
	deadlineReader    deadlineReader
}

type NewMessageServiceParams struct {
	MessageReadWriter messageReadWriter
	AgentFinder       agentFinder
	DeadlineReader    deadlineReader
}

func NewMessageService(p NewMessageServiceParams) *MessageService {
	return &MessageService{
		messageReadWriter: p.MessageReadWriter,
		agentFinder:       p.AgentFinder,
		deadlineReader:    p.DeadlineReader,
	}
}

type SendMessageParams struct {
	From      string
	ToAgentID string
	Body      string
	ReplyTo   *string
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

	deadline, err := s.deadlineReader.GetDeliveryDeadline()
	if err != nil {
		return nil, err
	}

	send := messagestore.SendParams{
		ToAgentID: recipient.ID,
		Body:      params.Body,
		ReplyTo:   params.ReplyTo,
		ExpiresAt: time.Now().Add(deadline),
	}
	if sender != nil {
		send.FromAgentID = &sender.ID
		send.FromName = sender.Name
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
