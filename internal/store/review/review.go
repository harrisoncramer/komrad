// Package review stores the conversations a person pins to the lines of an agent's diff, and
// the messages in them, whether the person or the agent wrote them.
package review

import (
	"context"
	"fmt"
	"time"

	"weave/db/models"
	"weave/internal/store"
)

// ReviewAuthorKind is who wrote a message.
type ReviewAuthorKind string

const (
	// ReviewAuthorHuman is the person reviewing.
	ReviewAuthorHuman ReviewAuthorKind = ReviewAuthorKind(models.ReviewAuthorKindHuman)
	// ReviewAuthorAgent is the agent whose diff is being reviewed.
	ReviewAuthorAgent ReviewAuthorKind = ReviewAuthorKind(models.ReviewAuthorKindAgent)
)

// ReviewThread is one conversation pinned to a place in an agent's diff. UnreadByAgent and
// UnreadByHuman are read off the watermarks rather than stored: each says whether the other
// side has written something since this one last looked.
type ReviewThread struct {
	ID      string
	AgentID string

	FilePath string

	OldStart int32
	OldEnd   int32
	NewStart int32
	NewEnd   int32

	QuotedLines []string

	ResolvedAt *time.Time

	LastHumanActivityAt time.Time
	AgentReadAt         *time.Time
	HumanReadAt         *time.Time

	UnreadByAgent bool
	UnreadByHuman bool

	CreatedAt time.Time
	UpdatedAt time.Time
}

// IsResolved reports whether the thread has been settled.
func (t ReviewThread) IsResolved() bool { return t.ResolvedAt != nil }

// ReviewMessage is one thing said in a thread.
type ReviewMessage struct {
	ID       string
	ThreadID string

	AuthorKind    ReviewAuthorKind
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

type CreateReviewThreadParams struct {
	AgentID  string
	FilePath string

	OldStart int32
	OldEnd   int32
	NewStart int32
	NewEnd   int32

	QuotedLines []string
}

// CreateReviewThread pins a new conversation to a place in a diff. It holds no message of its own;
// the first message is written separately, which is what advances the activity watermark.
func (s *Store) CreateReviewThread(ctx context.Context, params CreateReviewThreadParams) (*ReviewThread, error) {
	row, err := s.queries.InsertReviewThread(ctx, models.InsertReviewThreadParams{
		AgentID:     params.AgentID,
		FilePath:    params.FilePath,
		OldStart:    params.OldStart,
		OldEnd:      params.OldEnd,
		NewStart:    params.NewStart,
		NewEnd:      params.NewEnd,
		QuotedLines: params.QuotedLines,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open a review thread on %q: %w", params.FilePath, err)
	}

	thread := reviewThreadFromRow(*row)

	return &thread, nil
}

// ListReviewThreads is every thread on an agent, in the order they run down the diff.
func (s *Store) ListReviewThreads(ctx context.Context, agentID string) ([]ReviewThread, error) {
	rows, err := s.queries.ListReviewThreads(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the review threads on agent %q: %w", agentID, err)
	}

	threads := make([]ReviewThread, 0, len(rows))
	for _, row := range rows {
		thread := ReviewThread{
			ID:                  row.ID,
			AgentID:             row.AgentID,
			FilePath:            row.FilePath,
			OldStart:            row.OldStart,
			OldEnd:              row.OldEnd,
			NewStart:            row.NewStart,
			NewEnd:              row.NewEnd,
			QuotedLines:         row.QuotedLines,
			ResolvedAt:          row.ResolvedAt,
			LastHumanActivityAt: row.LastHumanActivityAt,
			AgentReadAt:         row.AgentReadAt,
			HumanReadAt:         row.HumanReadAt,
			UnreadByHuman:       row.UnreadByHuman,
			CreatedAt:           row.CreatedAt,
			UpdatedAt:           row.UpdatedAt,
		}
		if row.UnreadByAgent != nil {
			thread.UnreadByAgent = *row.UnreadByAgent
		}

		threads = append(threads, thread)
	}

	return threads, nil
}

// GetReviewThread is one thread, without the watermarks resolved into unread flags.
func (s *Store) GetReviewThread(ctx context.Context, id string) (*ReviewThread, error) {
	row, err := s.queries.GetReviewThread(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to read the review thread %q: %w", id, err)
	}

	thread := reviewThreadFromRow(*row)

	return &thread, nil
}

// SetReviewThreadResolved settles a thread or reopens it.
func (s *Store) SetReviewThreadResolved(ctx context.Context, id string, resolved bool) (*ReviewThread, error) {
	var resolvedAt *time.Time
	if resolved {
		now := time.Now()
		resolvedAt = &now
	}

	row, err := s.queries.SetReviewThreadResolved(ctx, models.SetReviewThreadResolvedParams{
		ID:         id,
		ResolvedAt: resolvedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to settle the review thread %q: %w", id, err)
	}

	thread := reviewThreadFromRow(*row)

	return &thread, nil
}

// RecordReviewThreadActivity reopens a thread and, for a message the person wrote, advances the activity watermark
// the agent's unread flag is measured against. A thread the agent replies to reopens too,
// since a settled thread somebody is still talking in was not settled.
func (s *Store) RecordReviewThreadActivity(ctx context.Context, threadID string, kind ReviewAuthorKind) error {
	err := s.queries.TouchReviewThread(ctx, models.TouchReviewThreadParams{
		ID:    threadID,
		Human: kind == ReviewAuthorHuman,
	})
	if err != nil {
		return fmt.Errorf("failed to record activity on the review thread %q: %w", threadID, err)
	}

	return nil
}

// MarkReviewThreadsRead stamps a side's watermark at the moment it was actually shown, which the caller
// takes from the newest message it returned rather than from the clock: a message written
// between the read and the stamp would otherwise be marked read without being handed over.
func (s *Store) MarkReviewThreadsRead(ctx context.Context, ids []string, kind ReviewAuthorKind, readAt time.Time) error {
	if len(ids) == 0 {
		return nil
	}

	var err error
	if kind == ReviewAuthorAgent {
		err = s.queries.MarkReviewThreadsReadByAgent(ctx, models.MarkReviewThreadsReadByAgentParams{
			Ids:    ids,
			ReadAt: &readAt,
		})
	} else {
		err = s.queries.MarkReviewThreadsReadByHuman(ctx, models.MarkReviewThreadsReadByHumanParams{
			Ids:    ids,
			ReadAt: &readAt,
		})
	}

	if err != nil {
		return fmt.Errorf("failed to mark %d review thread(s) read: %w", len(ids), err)
	}

	return nil
}

// CountUnreadReviewThreads is how many open threads hold something the agent has not been told about,
// which is what a notification says and what decides whether there is anything to say.
func (s *Store) CountUnreadReviewThreads(ctx context.Context, agentID string) (int64, error) {
	count, err := s.queries.CountUnreadReviewThreads(ctx, agentID)
	if err != nil {
		return 0, fmt.Errorf("failed to count unread review threads for agent %q: %w", agentID, err)
	}

	return count, nil
}

// DeleteReviewThread removes a thread and every message in it.
func (s *Store) DeleteReviewThread(ctx context.Context, id string) error {
	if err := s.queries.DeleteReviewThread(ctx, id); err != nil {
		return fmt.Errorf("failed to delete the review thread %q: %w", id, err)
	}

	return nil
}

type AddReviewMessageParams struct {
	ThreadID string

	Kind ReviewAuthorKind
	// AuthorAgentID is the agent that wrote it, set only when an agent did. It is a link to
	// which agent rather than the record of who wrote it, since deleting an agent clears it.
	AuthorAgentID *string

	Body string
}

// AddReviewMessage writes a message into a thread.
func (s *Store) AddReviewMessage(ctx context.Context, params AddReviewMessageParams) (*ReviewMessage, error) {
	row, err := s.queries.InsertReviewMessage(ctx, models.InsertReviewMessageParams{
		ThreadID:      params.ThreadID,
		AuthorKind:    models.ReviewAuthorKind(params.Kind),
		AuthorAgentID: params.AuthorAgentID,
		Body:          params.Body,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to write a message in the review thread %q: %w", params.ThreadID, err)
	}

	message := reviewMessageFromRow(*row)

	return &message, nil
}

// SetReviewMessageBody rewrites a message.
func (s *Store) SetReviewMessageBody(ctx context.Context, id, body string) (*ReviewMessage, error) {
	row, err := s.queries.UpdateReviewMessageBody(ctx, models.UpdateReviewMessageBodyParams{
		ID:   id,
		Body: body,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to rewrite the review message %q: %w", id, err)
	}

	message := reviewMessageFromRow(*row)

	return &message, nil
}

// GetReviewMessage is one message.
func (s *Store) GetReviewMessage(ctx context.Context, id string) (*ReviewMessage, error) {
	row, err := s.queries.GetReviewMessage(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to read the review message %q: %w", id, err)
	}

	message := reviewMessageFromRow(*row)

	return &message, nil
}

// ListReviewMessages is every message in the given threads, oldest first within each.
func (s *Store) ListReviewMessages(ctx context.Context, threadIDs []string) ([]ReviewMessage, error) {
	if len(threadIDs) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ListReviewMessagesByThreads(ctx, threadIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to list the messages of %d review thread(s): %w", len(threadIDs), err)
	}

	messages := make([]ReviewMessage, 0, len(rows))
	for _, row := range rows {
		messages = append(messages, reviewMessageFromRow(*row))
	}

	return messages, nil
}

// DeleteReviewMessage removes one message, leaving the thread it was in.
func (s *Store) DeleteReviewMessage(ctx context.Context, id string) error {
	if err := s.queries.DeleteReviewMessage(ctx, id); err != nil {
		return fmt.Errorf("failed to delete the review message %q: %w", id, err)
	}

	return nil
}

func reviewThreadFromRow(row models.ReviewThread) ReviewThread {
	return ReviewThread{
		ID:                  row.ID,
		AgentID:             row.AgentID,
		FilePath:            row.FilePath,
		OldStart:            row.OldStart,
		OldEnd:              row.OldEnd,
		NewStart:            row.NewStart,
		NewEnd:              row.NewEnd,
		QuotedLines:         row.QuotedLines,
		ResolvedAt:          row.ResolvedAt,
		LastHumanActivityAt: row.LastHumanActivityAt,
		AgentReadAt:         row.AgentReadAt,
		HumanReadAt:         row.HumanReadAt,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
	}
}

func reviewMessageFromRow(row models.ReviewMessage) ReviewMessage {
	return ReviewMessage{
		ID:            row.ID,
		ThreadID:      row.ThreadID,
		AuthorKind:    ReviewAuthorKind(row.AuthorKind),
		AuthorAgentID: row.AuthorAgentID,
		Body:          row.Body,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
}
