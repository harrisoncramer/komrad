package message

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"weave/db/models"
	"weave/internal/store"
	"weave/lib/xslice"
)

type Message struct {
	ID          string
	FromAgentID *string
	FromName    string
	ToAgentID   string
	Body        string
	ReplyTo     *string
	ClaimedAt   *time.Time
	ExpiresAt   time.Time
	CreatedAt   time.Time
}

type Store struct {
	queries *models.Queries
}

func New(db store.DBTX) *Store {
	return &Store{queries: models.New(db)}
}

type SendParams struct {
	FromAgentID *string
	FromName    string
	ToAgentID   string
	Body        string
	ReplyTo     *string
	ExpiresAt   time.Time
}

func (s *Store) Send(ctx context.Context, params SendParams) (*Message, error) {
	row, err := s.queries.InsertAgentMessage(ctx, models.InsertAgentMessageParams{
		FromAgentID: params.FromAgentID,
		FromName:    params.FromName,
		ToAgentID:   params.ToAgentID,
		Body:        params.Body,
		ReplyTo:     params.ReplyTo,
		ExpiresAt:   params.ExpiresAt,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to send a message to agent %q: %w", params.ToAgentID, err)
	}

	sent := messageFromRow(*row)

	return &sent, nil
}

func (s *Store) Claim(ctx context.Context, toAgentID string, limit int32) ([]Message, error) {
	if !store.IsID(toAgentID) {
		return nil, nil
	}

	rows, err := s.queries.ClaimAgentMessages(ctx, models.ClaimAgentMessagesParams{
		ToAgentID: toAgentID,
		Lim:       limit,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to claim the mail for agent %q: %w", toAgentID, err)
	}

	claimed := xslice.Map(rows, func(row *models.AgentMessage) Message {
		return messageFromRow(*row)
	})

	slices.SortFunc(claimed, func(a, b Message) int {
		return strings.Compare(a.ID, b.ID)
	})

	return claimed, nil
}

func (s *Store) CountPending(ctx context.Context, toAgentID string) (int64, error) {
	if !store.IsID(toAgentID) {
		return 0, nil
	}

	count, err := s.queries.CountPendingAgentMessages(ctx, toAgentID)
	if err != nil {
		return 0, fmt.Errorf("failed to count the mail waiting for agent %q: %w", toAgentID, err)
	}

	return count, nil
}

// ListPendingMailboxes is every agent holding mail that is neither claimed nor expired, so
// the sweep can find a mailbox whose own delivery job is gone.
func (s *Store) ListPendingMailboxes(ctx context.Context) ([]string, error) {
	rows, err := s.queries.ListMailboxesWithPendingMail(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the mailboxes holding mail: %w", err)
	}

	return rows, nil
}

func (s *Store) ListExpired(ctx context.Context, toAgentID string) ([]Message, error) {
	if !store.IsID(toAgentID) {
		return nil, nil
	}

	rows, err := s.queries.ListExpiredAgentMessages(ctx, toAgentID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the expired mail for agent %q: %w", toAgentID, err)
	}

	return xslice.Map(rows, func(row *models.AgentMessage) Message {
		return messageFromRow(*row)
	}), nil
}

func messageFromRow(row models.AgentMessage) Message {
	return Message{
		ID:          row.ID,
		FromAgentID: row.FromAgentID,
		FromName:    row.FromName,
		ToAgentID:   row.ToAgentID,
		Body:        row.Body,
		ReplyTo:     row.ReplyTo,
		ClaimedAt:   row.ClaimedAt,
		ExpiresAt:   row.ExpiresAt,
		CreatedAt:   row.CreatedAt,
	}
}
