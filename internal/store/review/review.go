// Package review stores the conversations pinned to the lines of an agent's diff and the
// messages in them.
package review

import (
	"context"
	"fmt"
	"time"

	"komrade/db"
	"komrade/db/models"
	"komrade/internal/store"
	"komrade/lib/xslice"
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
type Thread struct {
	ID      string
	AgentID string

	// ReviewID is the review the thread was opened as part of.
	ReviewID string
	// RoundID is the round the thread's line numbers were measured against.
	RoundID string
	// BaseSHA is the commit the old side of the line numbers was read against.
	BaseSHA string
	// RoundOrdinal names that round the way the panel does, zero when it was not read.
	RoundOrdinal int32
	// ReviewReleasedAt is when the review this thread came out with was released, nil while the
	// thread is still private to its author.
	ReviewReleasedAt *time.Time
	// ReviewAuthorAgentID is the agent that gave the review this thread was opened in, nil when
	// the person reviewing did.
	ReviewAuthorAgentID *string
	FilePath            string
	OldStart            int32
	OldEnd              int32
	NewStart            int32
	NewEnd              int32
	QuotedLines         []string
	LastAgentActivityAt *time.Time
	HumanReadAt         *time.Time

	// ResolvedByKind is whether a person or an agent settled the thread, empty while nobody has.
	ResolvedByKind AuthorKind
	// ResolvedByAgentID is the agent that settled it, nil for a person or an unsettled thread.
	ResolvedByAgentID *string
	// ResolvedAt is when the thread was settled, nil while it is still owed an
	// answer.
	ResolvedAt    *time.Time
	UnreadByHuman bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// DeliveryState is how far one of the reviewer's comments has got towards the agent reading it.
type DeliveryState string

const (
	// DeliveryNone is the empty state carried by a message the agent wrote itself.
	DeliveryNone DeliveryState = ""
	// DeliveryUnsent is a comment in a review nobody has released yet.
	DeliveryUnsent DeliveryState = "unsent"
	// DeliverySent is a comment in a released review whose mail is still waiting to be claimed.
	DeliverySent DeliveryState = "sent"
	// DeliveryDelivered is a comment in a released review whose mail the agent has claimed.
	DeliveryDelivered DeliveryState = "delivered"
)

// Message is one thing said in a thread, carrying how far it has got towards being read.
type Message struct {
	ID            string
	ThreadID      string
	AuthorKind    AuthorKind
	AuthorAgentID *string
	Body          string

	// ReviewID is the review this message went out with, nil for an answer from the agent.
	ReviewID *string
	// Suggestion is the replacement lines the reviewer is proposing for the thread's range,
	// empty on an ordinary comment.
	Suggestion []string
	// SentAt is when the review carrying this comment was released, nil until then.
	SentAt *time.Time
	// DeliveredAt is when the agent claimed the mail raised for that review, nil until it has.
	DeliveredAt *time.Time
	Delivery    DeliveryState
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ReviewStore struct {
	queries *models.Queries
}

// NewReviewStore builds a review store over db, which is either the pool or a transaction.
func NewReviewStore(db store.DBTX) *ReviewStore {
	return &ReviewStore{queries: models.New(db)}
}

type CreateThreadParams struct {
	AgentID       string
	ReviewID      string
	RoundID       string
	BaseSHA       string
	Suggestion    []string
	FilePath      string
	OldStart      int32
	OldEnd        int32
	NewStart      int32
	NewEnd        int32
	QuotedLines   []string
	Body          string
	AuthorKind    AuthorKind
	AuthorAgentID *string
}

// CreateThread pins a conversation to a place in a diff and writes its first message in one
// statement.
func (s *ReviewStore) CreateThread(ctx context.Context, params CreateThreadParams) (*Thread, error) {
	row, err := s.queries.InsertReviewThreadWithMessage(ctx, models.InsertReviewThreadWithMessageParams{
		AgentID:       params.AgentID,
		ReviewID:      params.ReviewID,
		RoundID:       params.RoundID,
		BaseSha:       params.BaseSHA,
		Suggestion:    lines(params.Suggestion),
		FilePath:      params.FilePath,
		OldStart:      params.OldStart,
		OldEnd:        params.OldEnd,
		NewStart:      params.NewStart,
		NewEnd:        params.NewEnd,
		QuotedLines:   lines(params.QuotedLines),
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
		ReviewID:            row.ReviewID,
		RoundID:             row.RoundID,
		BaseSHA:             row.BaseSha,
		FilePath:            row.FilePath,
		OldStart:            row.OldStart,
		OldEnd:              row.OldEnd,
		NewStart:            row.NewStart,
		NewEnd:              row.NewEnd,
		QuotedLines:         row.QuotedLines,
		LastAgentActivityAt: row.LastAgentActivityAt,
		HumanReadAt:         row.HumanReadAt,
		ResolvedAt:          row.ResolvedAt,
		ResolvedByKind:      AuthorKind(row.ResolvedByKind.ReviewAuthorKind),
		ResolvedByAgentID:   row.ResolvedByAgentID,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
	}, nil
}

// ListThreads is every thread on an agent, in the order they run down the diff.
func (s *ReviewStore) ListThreads(ctx context.Context, agentID string) ([]Thread, error) {
	if !store.IsID(agentID) {
		return nil, nil
	}

	rows, err := s.queries.ListReviewThreads(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the review threads on agent %q: %w", agentID, err)
	}

	threads := make([]Thread, 0, len(rows))
	for _, row := range rows {
		thread := Thread{
			ID:                  row.ID,
			AgentID:             row.AgentID,
			ReviewID:            row.ReviewID,
			RoundID:             row.RoundID,
			BaseSHA:             row.BaseSha,
			RoundOrdinal:        row.RoundOrdinal,
			ReviewReleasedAt:    row.ReviewReleasedAt,
			ReviewAuthorAgentID: row.ReviewAuthorAgentID,
			FilePath:            row.FilePath,
			OldStart:            row.OldStart,
			OldEnd:              row.OldEnd,
			NewStart:            row.NewStart,
			NewEnd:              row.NewEnd,
			QuotedLines:         row.QuotedLines,
			LastAgentActivityAt: row.LastAgentActivityAt,
			HumanReadAt:         row.HumanReadAt,
			ResolvedAt:          row.ResolvedAt,
			ResolvedByKind:      AuthorKind(row.ResolvedByKind.ReviewAuthorKind),
			ResolvedByAgentID:   row.ResolvedByAgentID,
			CreatedAt:           row.CreatedAt,
			UpdatedAt:           row.UpdatedAt,
		}
		if row.UnreadByHuman != nil {
			thread.UnreadByHuman = *row.UnreadByHuman
		}

		threads = append(threads, thread)
	}

	return threads, nil
}

// GetThread is one thread without the read stamps resolved into unread flags, ErrNotFound
// for an id that names nothing.
func (s *ReviewStore) GetThread(ctx context.Context, id string) (*Thread, error) {
	if !store.IsID(id) {
		return nil, threadNotFound(id)
	}

	row, err := s.queries.GetReviewThread(ctx, id)
	if db.IsNoRows(err) {
		return nil, threadNotFound(id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read the review thread %q: %w", id, err)
	}

	thread := threadFromRow(models.ReviewThread{
		ID:                  row.ID,
		AgentID:             row.AgentID,
		ReviewID:            row.ReviewID,
		RoundID:             row.RoundID,
		BaseSha:             row.BaseSha,
		FilePath:            row.FilePath,
		OldStart:            row.OldStart,
		OldEnd:              row.OldEnd,
		NewStart:            row.NewStart,
		NewEnd:              row.NewEnd,
		QuotedLines:         row.QuotedLines,
		LastAgentActivityAt: row.LastAgentActivityAt,
		HumanReadAt:         row.HumanReadAt,
		ResolvedAt:          row.ResolvedAt,
		ResolvedByKind:      row.ResolvedByKind,
		ResolvedByAgentID:   row.ResolvedByAgentID,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
	})
	thread.ReviewReleasedAt = row.ReviewReleasedAt
	thread.ReviewAuthorAgentID = row.ReviewAuthorAgentID

	return &thread, nil
}

// MarkThreadRead stamps the person's read column from the agent's activity column.
func (s *ReviewStore) MarkThreadRead(ctx context.Context, id string) (*Thread, error) {
	if !store.IsID(id) {
		return nil, threadNotFound(id)
	}

	row, err := s.queries.MarkReviewThreadRead(ctx, id)
	if db.IsNoRows(err) {
		return nil, threadNotFound(id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to mark the review thread %q read: %w", id, err)
	}

	thread := threadFromRow(*row)

	return &thread, nil
}

// SetThreadResolvedParams is the thread being settled or reopened, and who is doing it.
type SetThreadResolvedParams struct {
	ID       string
	Resolved bool
	// ByKind is whether a person or an agent settled it, which reopening clears along with the
	// rest.
	ByKind AuthorKind
	// ByAgentID is the agent that settled it, nil for a person.
	ByAgentID *string
}

// SetThreadResolved settles a thread or reopens it, recording who did.
func (s *ReviewStore) SetThreadResolved(ctx context.Context, params SetThreadResolvedParams) (*Thread, error) {
	if !store.IsID(params.ID) {
		return nil, threadNotFound(params.ID)
	}

	row, err := s.queries.SetReviewThreadResolved(ctx, models.SetReviewThreadResolvedParams{
		ID:                params.ID,
		Resolved:          params.Resolved,
		ResolvedByKind:    models.NullReviewAuthorKind{ReviewAuthorKind: models.ReviewAuthorKind(params.ByKind), Valid: params.ByKind != ""},
		ResolvedByAgentID: params.ByAgentID,
	})
	if db.IsNoRows(err) {
		return nil, threadNotFound(params.ID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to settle the review thread %q: %w", params.ID, err)
	}

	thread := threadFromRow(*row)

	return &thread, nil
}

type CreateMessageParams struct {
	ThreadID string
	// ReviewID is the batch the message goes out with, nil for an answer.
	ReviewID   *string
	Suggestion []string
	AuthorKind AuthorKind
	// AuthorAgentID links to the agent that wrote it, and is cleared when that agent is deleted.
	AuthorAgentID *string
	Body          string
}

// CreateMessage writes a message into a thread and moves the writer's activity stamp in one
// statement.
func (s *ReviewStore) CreateMessage(ctx context.Context, params CreateMessageParams) (*Message, error) {
	row, err := s.queries.InsertReviewMessage(ctx, models.InsertReviewMessageParams{
		ThreadID:      params.ThreadID,
		ReviewID:      params.ReviewID,
		Suggestion:    lines(params.Suggestion),
		AuthorKind:    models.ReviewAuthorKind(params.AuthorKind),
		AuthorAgentID: params.AuthorAgentID,
		Body:          params.Body,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to write a message in the review thread %q: %w", params.ThreadID, err)
	}

	message := Message{
		ID:            row.ID,
		ThreadID:      row.ThreadID,
		ReviewID:      row.ReviewID,
		Suggestion:    row.Suggestion,
		AuthorKind:    AuthorKind(row.AuthorKind),
		AuthorAgentID: row.AuthorAgentID,
		Body:          row.Body,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
	message.Delivery = deliveryOf(message)

	return &message, nil
}

// UpdateMessage rewrites a message and moves its author's activity stamp with it.
func (s *ReviewStore) UpdateMessage(ctx context.Context, id, body string, suggestion []string) (*Message, error) {
	if !store.IsID(id) {
		return nil, messageNotFound(id)
	}

	row, err := s.queries.UpdateReviewMessageBody(ctx, models.UpdateReviewMessageBodyParams{
		ID:         id,
		Body:       body,
		Suggestion: lines(suggestion),
	})
	if db.IsNoRows(err) {
		return nil, messageNotFound(id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to rewrite the review message %q: %w", id, err)
	}

	message := Message{
		ID:            row.ID,
		ThreadID:      row.ThreadID,
		ReviewID:      row.ReviewID,
		Suggestion:    row.Suggestion,
		AuthorKind:    AuthorKind(row.AuthorKind),
		AuthorAgentID: row.AuthorAgentID,
		Body:          row.Body,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
	message.Delivery = deliveryOf(message)

	return &message, nil
}

// GetMessage is one message, ErrNotFound for an id that names nothing.
func (s *ReviewStore) GetMessage(ctx context.Context, id string) (*Message, error) {
	if !store.IsID(id) {
		return nil, messageNotFound(id)
	}

	row, err := s.queries.GetReviewMessage(ctx, id)
	if db.IsNoRows(err) {
		return nil, messageNotFound(id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read the review message %q: %w", id, err)
	}

	message := messageFromRow(*row)

	return &message, nil
}

// ListMessages is every message in the given threads, oldest first within each.
func (s *ReviewStore) ListMessages(ctx context.Context, threadIDs []string) ([]Message, error) {
	if len(threadIDs) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ListReviewMessagesByThreads(ctx, threadIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to list the messages of %d review thread(s): %w", len(threadIDs), err)
	}

	messages := make([]Message, 0, len(rows))
	for _, row := range rows {
		message := Message{
			ID:            row.ID,
			ThreadID:      row.ThreadID,
			ReviewID:      row.ReviewID,
			Suggestion:    row.Suggestion,
			AuthorKind:    AuthorKind(row.AuthorKind),
			AuthorAgentID: row.AuthorAgentID,
			Body:          row.Body,
			SentAt:        row.ReviewReleasedAt,
			DeliveredAt:   row.DeliveredAt,
			CreatedAt:     row.CreatedAt,
			UpdatedAt:     row.UpdatedAt,
		}
		message.Delivery = deliveryOf(message)

		messages = append(messages, message)
	}

	return messages, nil
}

// ViewedFile is one file of an agent's diff that the person reviewing has read.
type ViewedFile struct {
	AgentID     string
	FilePath    string
	Fingerprint string
	ViewedAt    time.Time
}

type MarkFileViewedParams struct {
	AgentID     string
	FilePath    string
	Fingerprint string
}

// MarkFileViewed records that a file has been read, storing the fingerprint of its diff content
// so the mark can be checked against a later patch.
func (s *ReviewStore) MarkFileViewed(ctx context.Context, params MarkFileViewedParams) (*ViewedFile, error) {
	row, err := s.queries.MarkReviewFileViewed(ctx, models.MarkReviewFileViewedParams{
		AgentID:     params.AgentID,
		FilePath:    params.FilePath,
		Fingerprint: params.Fingerprint,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to mark %q viewed on agent %q: %w", params.FilePath, params.AgentID, err)
	}

	viewed := viewedFileFromRow(*row)

	return &viewed, nil
}

// UnmarkFileViewed drops the mark on one file.
func (s *ReviewStore) UnmarkFileViewed(ctx context.Context, agentID, filePath string) error {
	if !store.IsID(agentID) {
		return nil
	}

	err := s.queries.UnmarkReviewFileViewed(ctx, models.UnmarkReviewFileViewedParams{
		AgentID:  agentID,
		FilePath: filePath,
	})
	if err != nil {
		return fmt.Errorf("failed to unmark %q viewed on agent %q: %w", filePath, agentID, err)
	}

	return nil
}

// ListViewedFiles is every file of an agent marked read.
func (s *ReviewStore) ListViewedFiles(ctx context.Context, agentID string) ([]ViewedFile, error) {
	if !store.IsID(agentID) {
		return nil, nil
	}

	rows, err := s.queries.ListReviewViewedFiles(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the viewed files of agent %q: %w", agentID, err)
	}

	viewed := xslice.Map(rows, func(row *models.ReviewViewedFile) ViewedFile {
		return viewedFileFromRow(*row)
	})

	return viewed, nil
}

func viewedFileFromRow(row models.ReviewViewedFile) ViewedFile {
	return ViewedFile{
		AgentID:     row.AgentID,
		FilePath:    row.FilePath,
		Fingerprint: row.Fingerprint,
		ViewedAt:    row.ViewedAt,
	}
}

func threadFromRow(row models.ReviewThread) Thread {
	return Thread{
		ID:                  row.ID,
		AgentID:             row.AgentID,
		ReviewID:            row.ReviewID,
		RoundID:             row.RoundID,
		BaseSHA:             row.BaseSha,
		FilePath:            row.FilePath,
		OldStart:            row.OldStart,
		OldEnd:              row.OldEnd,
		NewStart:            row.NewStart,
		NewEnd:              row.NewEnd,
		QuotedLines:         row.QuotedLines,
		LastAgentActivityAt: row.LastAgentActivityAt,
		HumanReadAt:         row.HumanReadAt,
		ResolvedAt:          row.ResolvedAt,
		ResolvedByKind:      AuthorKind(row.ResolvedByKind.ReviewAuthorKind),
		ResolvedByAgentID:   row.ResolvedByAgentID,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
	}
}

func messageFromRow(row models.ReviewMessage) Message {
	message := undeliveredMessageFromRow(row)
	message.Delivery = deliveryOf(message)

	return message
}

// undeliveredMessageFromRow is a message without the state of the review it went out with.
func undeliveredMessageFromRow(row models.ReviewMessage) Message {
	return Message{
		ID:            row.ID,
		ThreadID:      row.ThreadID,
		ReviewID:      row.ReviewID,
		Suggestion:    row.Suggestion,
		AuthorKind:    AuthorKind(row.AuthorKind),
		AuthorAgentID: row.AuthorAgentID,
		Body:          row.Body,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
}

// deliveryOf reads a comment's state off the review it belongs to.
func deliveryOf(message Message) DeliveryState {
	switch {
	case message.ReviewID == nil:
		return DeliveryNone
	case message.SentAt == nil:
		return DeliveryUnsent
	case message.DeliveredAt != nil:
		return DeliveryDelivered
	default:
		return DeliverySent
	}
}

// lines turns a nil slice into the empty array a text[] column expects.
func lines(values []string) []string {
	if values == nil {
		return []string{}
	}

	return values
}

func threadNotFound(id string) error {
	return fmt.Errorf("%w: review thread %q", store.ErrNotFound, id)
}

func messageNotFound(id string) error {
	return fmt.Errorf("%w: review message %q", store.ErrNotFound, id)
}
