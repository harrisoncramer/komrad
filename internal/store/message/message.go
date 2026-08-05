package message

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"weave/db"
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
	Hops        int32
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
	Hops        int32
	ExpiresAt   time.Time
}

func (s *Store) Send(ctx context.Context, params SendParams) (*Message, error) {
	row, err := s.queries.InsertAgentMessage(ctx, models.InsertAgentMessageParams{
		FromAgentID: params.FromAgentID,
		FromName:    params.FromName,
		ToAgentID:   params.ToAgentID,
		Body:        params.Body,
		ReplyTo:     params.ReplyTo,
		Hops:        params.Hops,
		ExpiresAt:   params.ExpiresAt,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to send a message to agent %q: %w", params.ToAgentID, err)
	}

	sent := messageFromRow(*row)

	return &sent, nil
}

func (s *Store) GetByID(ctx context.Context, id string) (*Message, error) {
	if !store.IsID(id) {
		return nil, nil
	}

	row, err := s.queries.GetAgentMessage(ctx, id)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get message %q: %w", id, err)
	}

	got := messageFromRow(*row)

	return &got, nil
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

// ListPendingMailboxes is every agent holding unclaimed mail, live or not yet reported as
// expired, so the sweep finds a mailbox whose own delivery job is gone as well as one whose
// only mail timed out unseen. Deliver tells the two cases apart itself via CountPending, which
// excludes expired mail. Once ListExpired has reported a mailbox's expired mail, it drops out
// of this list on its own.
func (s *Store) ListPendingMailboxes(ctx context.Context) ([]string, error) {
	rows, err := s.queries.ListMailboxesWithPendingMail(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the mailboxes holding mail: %w", err)
	}

	return rows, nil
}

// ListExpired reports the mail that ran out of time before the recipient claimed it, and marks
// it reported so the next call does not hand back the same message again.
func (s *Store) ListExpired(ctx context.Context, toAgentID string) ([]Message, error) {
	if !store.IsID(toAgentID) {
		return nil, nil
	}

	rows, err := s.queries.ReportExpiredAgentMessages(ctx, toAgentID)
	if err != nil {
		return nil, fmt.Errorf("failed to report the expired mail for agent %q: %w", toAgentID, err)
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
		Hops:        row.Hops,
		ClaimedAt:   row.ClaimedAt,
		ExpiresAt:   row.ExpiresAt,
		CreatedAt:   row.CreatedAt,
	}
}
