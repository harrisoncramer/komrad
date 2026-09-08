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

// Thread is one conversation pinned to a place in an agent's diff, with the person's read
// stamp taken from the activity of the row that was shown rather than from the clock, so a
// reply written in between stays unread.
type Thread struct {
	ID      string
	AgentID string

	// ReviewID is the batch the thread was opened as part of, and through it the round the
	// thread is anchored to, neither of which ever moves.
	ReviewID string
	// RoundID is the round the thread's line numbers were measured in.
	RoundID string
	// Harvested says weave wrote this thread from an edit of the round's checkout, which is
	// what makes it weave's to rewrite or delete on a later harvest.
	Harvested bool
	// RoundOrdinal names that round the way the panel does, zero when it was not read.
	RoundOrdinal int32
	// ReviewReleasedAt is when the batch this thread came out with was let go of, nil while the
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

	// ResolvedAt is when the person reviewing settled the thread, nil while it is still owed an
	// answer and untouched by the code moving underneath.
	ResolvedAt    *time.Time
	UnreadByHuman bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// DeliveryState is how far one of the author's comments has got towards the agent reading it,
// read off the batch it went out with rather than off the comment.
type DeliveryState string

const (
	// DeliveryNone is what delivery says about a message the agent wrote itself, which is
	// nothing.
	DeliveryNone DeliveryState = ""
	// DeliveryUnsent is a comment in a review nobody has let go of yet.
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

	// ReviewID is the batch this message went out with, nil for an answer, which belongs to no
	// batch and reaches the reviewer as soon as it is written.
	ReviewID *string
	// Suggestion is the replacement lines the author is proposing for the thread's range,
	// empty for an ordinary comment.
	Suggestion []string
	// SentAt is when the batch carrying this comment was released, nil while it is only
	// written down.
	SentAt *time.Time
	// DeliveredAt is when the agent claimed the mail raised for that batch, nil until it has.
	DeliveredAt *time.Time
	Delivery    DeliveryState
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Store struct {
	queries *models.Queries
}

// New builds a review store over db, which is either the pool or a transaction.
func New(db store.DBTX) *Store {
	return &Store{queries: models.New(db)}
}

type CreateThreadParams struct {
	AgentID       string
	ReviewID      string
	RoundID       string
	Harvested     bool
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
// statement, stamping activity at the same NOW() so no thread exists with nothing said in it.
func (s *Store) CreateThread(ctx context.Context, params CreateThreadParams) (*Thread, error) {
	row, err := s.queries.InsertReviewThreadWithMessage(ctx, models.InsertReviewThreadWithMessageParams{
		AgentID:       params.AgentID,
		ReviewID:      params.ReviewID,
		RoundID:       params.RoundID,
		Harvested:     params.Harvested,
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
		Harvested:           row.Harvested,
		FilePath:            row.FilePath,
		OldStart:            row.OldStart,
		OldEnd:              row.OldEnd,
		NewStart:            row.NewStart,
		NewEnd:              row.NewEnd,
		QuotedLines:         row.QuotedLines,
		LastAgentActivityAt: row.LastAgentActivityAt,
		HumanReadAt:         row.HumanReadAt,
		ResolvedAt:          row.ResolvedAt,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
	}, nil
}

// ListThreads is every thread on an agent, in the order they run down the diff.
func (s *Store) ListThreads(ctx context.Context, agentID string) ([]Thread, error) {
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
			Harvested:           row.Harvested,
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
func (s *Store) GetThread(ctx context.Context, id string) (*Thread, error) {
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
		Harvested:           row.Harvested,
		FilePath:            row.FilePath,
		OldStart:            row.OldStart,
		OldEnd:              row.OldEnd,
		NewStart:            row.NewStart,
		NewEnd:              row.NewEnd,
		QuotedLines:         row.QuotedLines,
		LastAgentActivityAt: row.LastAgentActivityAt,
		HumanReadAt:         row.HumanReadAt,
		ResolvedAt:          row.ResolvedAt,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
	})
	thread.ReviewReleasedAt = row.ReviewReleasedAt
	thread.ReviewAuthorAgentID = row.ReviewAuthorAgentID

	return &thread, nil
}

// MarkThreadRead stamps the person's read column from the agent's activity column, which is
// how opening a single conversation catches up on it without claiming the rest.
func (s *Store) MarkThreadRead(ctx context.Context, id string) (*Thread, error) {
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

// SetThreadResolved settles a thread or reopens it.
func (s *Store) SetThreadResolved(ctx context.Context, id string, resolved bool) (*Thread, error) {
	if !store.IsID(id) {
		return nil, threadNotFound(id)
	}

	row, err := s.queries.SetReviewThreadResolved(ctx, models.SetReviewThreadResolvedParams{
		ID:       id,
		Resolved: resolved,
	})
	if db.IsNoRows(err) {
		return nil, threadNotFound(id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to settle the review thread %q: %w", id, err)
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
	// AuthorAgentID links to the agent that wrote it, which deleting that agent clears, so it
	// is not a record of who wrote it.
	AuthorAgentID *string
	Body          string
}

// CreateMessage writes a message into a thread and moves the writer's activity stamp, both in
// one statement.
func (s *Store) CreateMessage(ctx context.Context, params CreateMessageParams) (*Message, error) {
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

// UpdateMessage rewrites a message and moves its author's activity stamp with it, so the other
// side sees a rewrite as something new rather than as the text it was already shown.
func (s *Store) UpdateMessage(ctx context.Context, id, body string, suggestion []string) (*Message, error) {
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
func (s *Store) GetMessage(ctx context.Context, id string) (*Message, error) {
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

// ListMessages is every message in the given threads, oldest first within each, each carrying
// how far it has got towards being read.
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

// ViewedFile is one file of a round the person reviewing has read, and a later round is a row of
// its own, which is what makes an agent rewriting the file undo the mark.
type ViewedFile struct {
	RoundID  string
	FilePath string
	ViewedAt time.Time
}

type MarkFileViewedParams struct {
	RoundID  string
	FilePath string
}

// MarkFileViewed records that a file of a round has been read.
func (s *Store) MarkFileViewed(ctx context.Context, params MarkFileViewedParams) (*ViewedFile, error) {
	row, err := s.queries.MarkReviewFileViewed(ctx, models.MarkReviewFileViewedParams{
		RoundID:  params.RoundID,
		FilePath: params.FilePath,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to mark %q viewed on round %q: %w", params.FilePath, params.RoundID, err)
	}

	viewed := viewedFileFromRow(*row)

	return &viewed, nil
}

// UnmarkFileViewed drops the mark on one file, which is what the toggle does on its way back.
func (s *Store) UnmarkFileViewed(ctx context.Context, roundID, filePath string) error {
	if !store.IsID(roundID) {
		return nil
	}

	err := s.queries.UnmarkReviewFileViewed(ctx, models.UnmarkReviewFileViewedParams{
		RoundID:  roundID,
		FilePath: filePath,
	})
	if err != nil {
		return fmt.Errorf("failed to unmark %q viewed on round %q: %w", filePath, roundID, err)
	}

	return nil
}

// ListViewedFiles is every file of a round marked read.
func (s *Store) ListViewedFiles(ctx context.Context, roundID string) ([]ViewedFile, error) {
	if !store.IsID(roundID) {
		return nil, nil
	}

	rows, err := s.queries.ListReviewViewedFiles(ctx, roundID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the viewed files of round %q: %w", roundID, err)
	}

	viewed := make([]ViewedFile, 0, len(rows))
	for _, row := range rows {
		viewed = append(viewed, viewedFileFromRow(*row))
	}

	return viewed, nil
}

func viewedFileFromRow(row models.ReviewViewedFile) ViewedFile {
	return ViewedFile{
		RoundID:  row.RoundID,
		FilePath: row.FilePath,
		ViewedAt: row.ViewedAt,
	}
}

func threadFromRow(row models.ReviewThread) Thread {
	return Thread{
		ID:                  row.ID,
		AgentID:             row.AgentID,
		ReviewID:            row.ReviewID,
		RoundID:             row.RoundID,
		Harvested:           row.Harvested,
		FilePath:            row.FilePath,
		OldStart:            row.OldStart,
		OldEnd:              row.OldEnd,
		NewStart:            row.NewStart,
		NewEnd:              row.NewEnd,
		QuotedLines:         row.QuotedLines,
		LastAgentActivityAt: row.LastAgentActivityAt,
		HumanReadAt:         row.HumanReadAt,
		ResolvedAt:          row.ResolvedAt,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
	}
}

func messageFromRow(row models.ReviewMessage) Message {
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

	return message
}

// deliveryOf reads a comment's state off the batch it belongs to, a review being released,
// delivered and answered whole.
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

// lines is what a text[] column is written with, a nil slice writing SQL NULL rather than the
// empty array the column is declared with.
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

// HarvestedThread is a thread weave wrote from an edit of the round's checkout, carrying how
// many messages it holds so a caller can tell one nobody has answered from one somebody has.
type HarvestedThread struct {
	Thread
	MessageCount int64
}

// ListHarvested is every unreleased harvested thread on one round, which is the set a later
// harvest of that round reconciles against.
func (s *Store) ListHarvested(ctx context.Context, agentID, roundID string) ([]HarvestedThread, error) {
	if !store.IsID(agentID) || !store.IsID(roundID) {
		return nil, nil
	}

	rows, err := s.queries.ListHarvestedReviewThreads(ctx, models.ListHarvestedReviewThreadsParams{
		AgentID: agentID,
		RoundID: roundID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list the harvested threads of round %q: %w", roundID, err)
	}

	threads := make([]HarvestedThread, 0, len(rows))
	for _, row := range rows {
		threads = append(threads, HarvestedThread{
			Thread: threadFromRow(models.ReviewThread{
				ID:                  row.ID,
				AgentID:             row.AgentID,
				ReviewID:            row.ReviewID,
				RoundID:             row.RoundID,
				Harvested:           row.Harvested,
				FilePath:            row.FilePath,
				OldStart:            row.OldStart,
				OldEnd:              row.OldEnd,
				NewStart:            row.NewStart,
				NewEnd:              row.NewEnd,
				QuotedLines:         row.QuotedLines,
				LastAgentActivityAt: row.LastAgentActivityAt,
				HumanReadAt:         row.HumanReadAt,
				ResolvedAt:          row.ResolvedAt,
				CreatedAt:           row.CreatedAt,
				UpdatedAt:           row.UpdatedAt,
			}),
			MessageCount: row.MessageCount,
		})
	}

	return threads, nil
}

type MoveThreadParams struct {
	ID          string
	OldStart    int32
	OldEnd      int32
	NewStart    int32
	NewEnd      int32
	QuotedLines []string
}

// MoveThread repins a thread to a new range of the same round, which is what a harvested thread
// needs when the reviewer widens or narrows the run it was written from.
func (s *Store) MoveThread(ctx context.Context, params MoveThreadParams) (*Thread, error) {
	if !store.IsID(params.ID) {
		return nil, threadNotFound(params.ID)
	}

	row, err := s.queries.MoveReviewThread(ctx, models.MoveReviewThreadParams{
		ID:          params.ID,
		OldStart:    params.OldStart,
		OldEnd:      params.OldEnd,
		NewStart:    params.NewStart,
		NewEnd:      params.NewEnd,
		QuotedLines: lines(params.QuotedLines),
	})
	if db.IsNoRows(err) {
		return nil, threadNotFound(params.ID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to move the review thread %q: %w", params.ID, err)
	}

	thread := threadFromRow(*row)

	return &thread, nil
}

// DeleteThread removes a thread and the messages under it, which only ever happens to one weave
// harvested and nobody has answered.
func (s *Store) DeleteThread(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return threadNotFound(id)
	}

	_, err := s.queries.DeleteReviewThread(ctx, id)
	if db.IsNoRows(err) {
		return threadNotFound(id)
	}
	if err != nil {
		return fmt.Errorf("failed to delete the review thread %q: %w", id, err)
	}

	return nil
}
