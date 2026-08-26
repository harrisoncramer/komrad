// Package review stores the conversations a person pins to the lines of an agent's diff, and
// the messages in them, whether the person or the agent wrote them.
package review

import (
	"context"
	"fmt"
	"time"

	"weave/db"
	"weave/db/models"
	"weave/internal/store"
)

// AuthorKind is who wrote a message.
type AuthorKind string

const (
	// AuthorHuman is the person reviewing.
	AuthorHuman AuthorKind = AuthorKind(models.ReviewAuthorKindHuman)
	// AuthorAgent is the agent whose diff is being reviewed.
	AuthorAgent AuthorKind = AuthorKind(models.ReviewAuthorKindAgent)
)

// Thread is one conversation pinned to a place in an agent's diff.
//
// Each side has an activity stamp and a read stamp, and unread is the one compared against
// the other. Reading stamps the read column from the activity column of the row that was
// shown, never from the clock, so a message written between the read and the stamp stays
// unread instead of being marked read without being handed over.
type Thread struct {
	ID      string
	AgentID string

	FilePath string

	OldStart int32
	OldEnd   int32
	NewStart int32
	NewEnd   int32

	QuotedLines []string

	LastHumanActivityAt *time.Time
	LastAgentActivityAt *time.Time

	AgentReadAt *time.Time
	HumanReadAt *time.Time

	UnreadByAgent bool
	UnreadByHuman bool

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Message is one thing said in a thread.
type Message struct {
	ID       string
	ThreadID string

	AuthorKind    AuthorKind
	AuthorAgentID *string

	Body string

	CreatedAt time.Time
	UpdatedAt time.Time
}

type Store struct {
	queries *models.Queries
}

// New builds a review store over db, which is either the pool or a transaction.
func New(db store.DBTX) *Store {
	return &Store{queries: models.New(db)}
}

type CreateThreadParams struct {
	AgentID  string
	FilePath string

	OldStart int32
	OldEnd   int32
	NewStart int32
	NewEnd   int32

	QuotedLines []string

	Body          string
	AuthorKind    AuthorKind
	AuthorAgentID *string
}

// CreateThread pins a conversation to a place in a diff and writes its first message.
//
// Both rows and the activity stamp are one statement, so there is no window where a thread
// exists with nothing said in it, and the stamp is the same NOW() the message was written
// at rather than a second clock reading that would always be later.
func (s *Store) CreateThread(ctx context.Context, params CreateThreadParams) (*Thread, error) {
	row, err := s.queries.InsertReviewThreadWithMessage(ctx, models.InsertReviewThreadWithMessageParams{
		AgentID:       params.AgentID,
		FilePath:      params.FilePath,
		OldStart:      params.OldStart,
		OldEnd:        params.OldEnd,
		NewStart:      params.NewStart,
		NewEnd:        params.NewEnd,
		QuotedLines:   params.QuotedLines,
		AuthorKind:    models.ReviewAuthorKind(params.AuthorKind),
		AuthorAgentID: params.AuthorAgentID,
		Body:          params.Body,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open a review thread on %q: %w", params.FilePath, err)
	}

	return &Thread{
		ID:                  row.ID,
		AgentID:             row.AgentID,
		FilePath:            row.FilePath,
		OldStart:            row.OldStart,
		OldEnd:              row.OldEnd,
		NewStart:            row.NewStart,
		NewEnd:              row.NewEnd,
		QuotedLines:         row.QuotedLines,
		LastHumanActivityAt: row.LastHumanActivityAt,
		LastAgentActivityAt: row.LastAgentActivityAt,
		AgentReadAt:         row.AgentReadAt,
		HumanReadAt:         row.HumanReadAt,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
	}, nil
}

type ListThreadsParams struct {
	AgentID string
	// Reader is the side asking, which decides whose read stamp MarkRead moves.
	Reader AuthorKind
	// MarkRead claims what is returned, in the same statement that returns it. The unread
	// flags on the answer are the ones the caller was shown, not the ones left behind.
	MarkRead bool
}

// ListThreads is every thread on an agent, in the order they run down the diff.
func (s *Store) ListThreads(ctx context.Context, params ListThreadsParams) ([]Thread, error) {
	if !store.IsID(params.AgentID) {
		return nil, nil
	}

	rows, err := s.queries.ListReviewThreads(ctx, models.ListReviewThreadsParams{
		AgentID:  params.AgentID,
		Reader:   models.ReviewAuthorKind(params.Reader),
		MarkRead: params.MarkRead,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list the review threads on agent %q: %w", params.AgentID, err)
	}

	threads := make([]Thread, 0, len(rows))
	for _, row := range rows {
		thread := Thread{
			ID:                  row.ID,
			AgentID:             row.AgentID,
			FilePath:            row.FilePath,
			OldStart:            row.OldStart,
			OldEnd:              row.OldEnd,
			NewStart:            row.NewStart,
			NewEnd:              row.NewEnd,
			QuotedLines:         row.QuotedLines,
			LastHumanActivityAt: row.LastHumanActivityAt,
			LastAgentActivityAt: row.LastAgentActivityAt,
			AgentReadAt:         row.AgentReadAt,
			HumanReadAt:         row.HumanReadAt,
			CreatedAt:           row.CreatedAt,
			UpdatedAt:           row.UpdatedAt,
		}
		if row.UnreadByAgent != nil {
			thread.UnreadByAgent = *row.UnreadByAgent
		}
		if row.UnreadByHuman != nil {
			thread.UnreadByHuman = *row.UnreadByHuman
		}

		threads = append(threads, thread)
	}

	return threads, nil
}

// GetThread is one thread, without the read stamps resolved into unread flags. It answers
// nil for an id that names nothing, which is what the caller turns into its own not found.
func (s *Store) GetThread(ctx context.Context, id string) (*Thread, error) {
	if !store.IsID(id) {
		return nil, nil
	}

	row, err := s.queries.GetReviewThread(ctx, id)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read the review thread %q: %w", id, err)
	}

	thread := threadFromRow(*row)

	return &thread, nil
}

// MarkThreadRead stamps one side's read column from the other side's activity column, which
// is how a person opening a single conversation catches up on it without claiming the rest.
func (s *Store) MarkThreadRead(ctx context.Context, id string, reader AuthorKind) (*Thread, error) {
	if !store.IsID(id) {
		return nil, nil
	}

	row, err := s.queries.MarkReviewThreadRead(ctx, models.MarkReviewThreadReadParams{
		ID:     id,
		Reader: models.ReviewAuthorKind(reader),
	})
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to mark the review thread %q read: %w", id, err)
	}

	thread := threadFromRow(*row)

	return &thread, nil
}

// CountUnreadForAgent is how many open threads hold something the agent has not been told
// about, which is what a notification says and what decides whether there is anything to say.
func (s *Store) CountUnreadForAgent(ctx context.Context, agentID string) (int64, error) {
	if !store.IsID(agentID) {
		return 0, nil
	}

	count, err := s.queries.CountUnreadReviewThreads(ctx, agentID)
	if err != nil {
		return 0, fmt.Errorf("failed to count unread review threads for agent %q: %w", agentID, err)
	}

	return count, nil
}

// DeleteThread removes a thread and every message in it.
func (s *Store) DeleteThread(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.DeleteReviewThread(ctx, id); err != nil {
		return fmt.Errorf("failed to delete the review thread %q: %w", id, err)
	}

	return nil
}

type AddMessageParams struct {
	ThreadID string

	AuthorKind AuthorKind
	// AuthorAgentID is the agent that wrote it, set only when an agent did. It is a link to
	// which agent rather than the record of who wrote it, since deleting an agent clears it.
	AuthorAgentID *string

	Body string
}

// AddMessage writes a message into a thread and moves the writer's activity stamp, both in
// one statement.
func (s *Store) AddMessage(ctx context.Context, params AddMessageParams) (*Message, error) {
	row, err := s.queries.InsertReviewMessage(ctx, models.InsertReviewMessageParams{
		ThreadID:      params.ThreadID,
		AuthorKind:    models.ReviewAuthorKind(params.AuthorKind),
		AuthorAgentID: params.AuthorAgentID,
		Body:          params.Body,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to write a message in the review thread %q: %w", params.ThreadID, err)
	}

	return &Message{
		ID:            row.ID,
		ThreadID:      row.ThreadID,
		AuthorKind:    AuthorKind(row.AuthorKind),
		AuthorAgentID: row.AuthorAgentID,
		Body:          row.Body,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}, nil
}

// EditMessage rewrites a message and moves its author's activity stamp with it, so the other
// side sees a rewrite as something new rather than as the text it was already shown.
func (s *Store) EditMessage(ctx context.Context, id, body string) (*Message, error) {
	if !store.IsID(id) {
		return nil, nil
	}

	row, err := s.queries.UpdateReviewMessageBody(ctx, models.UpdateReviewMessageBodyParams{
		ID:   id,
		Body: body,
	})
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to rewrite the review message %q: %w", id, err)
	}

	return &Message{
		ID:            row.ID,
		ThreadID:      row.ThreadID,
		AuthorKind:    AuthorKind(row.AuthorKind),
		AuthorAgentID: row.AuthorAgentID,
		Body:          row.Body,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}, nil
}

// GetMessage is one message. It answers nil for an id that names nothing.
func (s *Store) GetMessage(ctx context.Context, id string) (*Message, error) {
	if !store.IsID(id) {
		return nil, nil
	}

	row, err := s.queries.GetReviewMessage(ctx, id)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read the review message %q: %w", id, err)
	}

	message := messageFromRow(*row)

	return &message, nil
}

// ListMessages is every message in the given threads, oldest first within each.
func (s *Store) ListMessages(ctx context.Context, threadIDs []string) ([]Message, error) {
	if len(threadIDs) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ListReviewMessagesByThreads(ctx, threadIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to list the messages of %d review thread(s): %w", len(threadIDs), err)
	}

	messages := make([]Message, 0, len(rows))
	for _, row := range rows {
		messages = append(messages, messageFromRow(*row))
	}

	return messages, nil
}

// DeleteMessage removes one message, leaving the thread it was in.
func (s *Store) DeleteMessage(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.DeleteReviewMessage(ctx, id); err != nil {
		return fmt.Errorf("failed to delete the review message %q: %w", id, err)
	}

	return nil
}

func threadFromRow(row models.ReviewThread) Thread {
	return Thread{
		ID:                  row.ID,
		AgentID:             row.AgentID,
		FilePath:            row.FilePath,
		OldStart:            row.OldStart,
		OldEnd:              row.OldEnd,
		NewStart:            row.NewStart,
		NewEnd:              row.NewEnd,
		QuotedLines:         row.QuotedLines,
		LastHumanActivityAt: row.LastHumanActivityAt,
		LastAgentActivityAt: row.LastAgentActivityAt,
		AgentReadAt:         row.AgentReadAt,
		HumanReadAt:         row.HumanReadAt,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
	}
}

func messageFromRow(row models.ReviewMessage) Message {
	return Message{
		ID:            row.ID,
		ThreadID:      row.ThreadID,
		AuthorKind:    AuthorKind(row.AuthorKind),
		AuthorAgentID: row.AuthorAgentID,
		Body:          row.Body,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
}
