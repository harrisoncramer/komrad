// Package review stores the conversations a person pins to the lines of an agent's diff, and
// the messages in them, whether the person or the agent wrote them.
package review

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
	ID                  string
	AgentID             string
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

// DeliveryState is how far one of the person's comments has got towards the agent reading it,
// read off the comment's own sent stamp and the newest doorbell raised against it rather than
// stored, since the stamp outlives mail the agent deletes or lets expire.
type DeliveryState string

const (
	// DeliveryNone is what delivery says about a message the agent wrote itself, which is
	// nothing, since nobody is waiting to be told about it.
	DeliveryNone DeliveryState = ""
	// DeliveryUnsent is a comment written down and not yet let go of.
	DeliveryUnsent DeliveryState = "unsent"
	// DeliverySent is a released comment whose doorbell is still waiting to be claimed.
	DeliverySent DeliveryState = "sent"
	// DeliveryDelivered is a released comment whose doorbell the agent has claimed.
	DeliveryDelivered DeliveryState = "delivered"
	// DeliveryExpired is a released comment whose doorbell ran out of time unclaimed or is
	// gone, so ringing again is what reaches the agent.
	DeliveryExpired DeliveryState = "expired"
)

// Message is one thing said in a thread, carrying how far it has got towards being read.
type Message struct {
	ID            string
	ThreadID      string
	AuthorKind    AuthorKind
	AuthorAgentID *string
	Body          string

	// SentAt is when the person released this comment to the agent, nil while it is only
	// written down.
	SentAt *time.Time
	// DeliveredAt is when the agent claimed the doorbell raised for it, nil until it has.
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

	thread := threadFromRow(*row)

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
	ThreadID   string
	AuthorKind AuthorKind
	// AuthorAgentID links to the agent that wrote it, which deleting that agent clears, so it
	// is not a record of who wrote it.
	AuthorAgentID *string
	Body          string
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

	message := Message{
		ID:            row.ID,
		ThreadID:      row.ThreadID,
		AuthorKind:    AuthorKind(row.AuthorKind),
		AuthorAgentID: row.AuthorAgentID,
		Body:          row.Body,
		SentAt:        row.SentAt,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
	message.Delivery = deliveryOf(message, false)

	return &message, nil
}

// EditMessage rewrites a message and moves its author's activity stamp with it, so the other
// side sees a rewrite as something new rather than as the text it was already shown.
func (s *Store) EditMessage(ctx context.Context, id, body string) (*Message, error) {
	if !store.IsID(id) {
		return nil, messageNotFound(id)
	}

	row, err := s.queries.UpdateReviewMessageBody(ctx, models.UpdateReviewMessageBodyParams{
		ID:   id,
		Body: body,
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
		AuthorKind:    AuthorKind(row.AuthorKind),
		AuthorAgentID: row.AuthorAgentID,
		Body:          row.Body,
		SentAt:        row.SentAt,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
	message.Delivery = deliveryOf(message, false)

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
			AuthorKind:    AuthorKind(row.AuthorKind),
			AuthorAgentID: row.AuthorAgentID,
			Body:          row.Body,
			SentAt:        row.SentAt,
			DeliveredAt:   row.DeliveredAt,
			CreatedAt:     row.CreatedAt,
			UpdatedAt:     row.UpdatedAt,
		}
		message.Delivery = deliveryOf(message, row.DoorbellExpired != nil && *row.DoorbellExpired)

		messages = append(messages, message)
	}

	return messages, nil
}

// CountUnsentMessages is how many comments on an agent's review the person has written and
// not sent, which the caller reads before raising a doorbell so a review with nothing left in
// it rings nobody.
func (s *Store) CountUnsentMessages(ctx context.Context, agentID string) (int, error) {
	if !store.IsID(agentID) {
		return 0, nil
	}

	count, err := s.queries.CountUnsentReviewMessages(ctx, agentID)
	if err != nil {
		return 0, fmt.Errorf("failed to count the unsent review messages on agent %q: %w", agentID, err)
	}

	return int(count), nil
}

// ReleaseUnsentMessages lets go of every comment on an agent's review the person has written
// and not sent, stamping each as sent and pointing it at the doorbell that says so, which the
// caller has already raised so that nothing is stamped for a review that never rang.
func (s *Store) ReleaseUnsentMessages(ctx context.Context, agentID, doorbellMailID string) ([]Message, error) {
	if !store.IsID(agentID) {
		return nil, nil
	}

	rows, err := s.queries.ReleaseUnsentReviewMessages(ctx, models.ReleaseUnsentReviewMessagesParams{
		AgentID:        agentID,
		DoorbellMailID: &doorbellMailID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to release the unsent review messages on agent %q: %w", agentID, err)
	}

	return sortedMessages(rows), nil
}

// PointMessagesAtDoorbell moves a batch of released comments onto a freshly raised doorbell,
// which is what ringing again does once the one they were released under is beyond reading.
func (s *Store) PointMessagesAtDoorbell(ctx context.Context, ids []string, doorbellMailID string) ([]Message, error) {
	storable := xslice.Filter(ids, store.IsID)
	if len(storable) == 0 {
		return nil, nil
	}

	rows, err := s.queries.PointReviewMessagesAtDoorbell(ctx, models.PointReviewMessagesAtDoorbellParams{
		Ids:            storable,
		DoorbellMailID: &doorbellMailID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to point %d review message(s) at the doorbell %q: %w", len(storable), doorbellMailID, err)
	}

	return sortedMessages(rows), nil
}

// ListExpiredMessages is every released comment on an agent's review whose doorbell ran out of
// time unclaimed or is no longer there, which is what ringing again reaches.
func (s *Store) ListExpiredMessages(ctx context.Context, agentID string) ([]Message, error) {
	if !store.IsID(agentID) {
		return nil, nil
	}

	rows, err := s.queries.ListExpiredReviewMessages(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the expired review messages on agent %q: %w", agentID, err)
	}

	return sortedMessages(rows), nil
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

// ViewedFile is one file of an agent's diff the person reviewing has read, holding the shape
// that file had at the time so the mark can be dropped once the file no longer looks like it.
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

// MarkFileViewed records that a file has been read, replacing the shape held for it when it
// was read before, since marking it again means the file as it now stands.
func (s *Store) MarkFileViewed(ctx context.Context, params MarkFileViewedParams) (*ViewedFile, error) {
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

// UnmarkFileViewed drops the mark on one file, which is what the toggle does on its way back.
func (s *Store) UnmarkFileViewed(ctx context.Context, agentID, filePath string) error {
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

// ListViewedFiles is every file marked read on an agent, whether or not the diff still looks
// the way it did when it was marked, which is the caller's comparison to make.
func (s *Store) ListViewedFiles(ctx context.Context, agentID string) ([]ViewedFile, error) {
	if !store.IsID(agentID) {
		return nil, nil
	}

	rows, err := s.queries.ListReviewViewedFiles(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the viewed files of agent %q: %w", agentID, err)
	}

	viewed := make([]ViewedFile, 0, len(rows))
	for _, row := range rows {
		viewed = append(viewed, viewedFileFromRow(*row))
	}

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

func sortedMessages(rows []*models.ReviewMessage) []Message {
	messages := xslice.Map(rows, func(row *models.ReviewMessage) Message {
		return messageFromRow(*row)
	})

	slices.SortFunc(messages, func(a, b Message) int {
		return strings.Compare(a.ID, b.ID)
	})

	return messages
}

func messageFromRow(row models.ReviewMessage) Message {
	message := Message{
		ID:            row.ID,
		ThreadID:      row.ThreadID,
		AuthorKind:    AuthorKind(row.AuthorKind),
		AuthorAgentID: row.AuthorAgentID,
		Body:          row.Body,
		SentAt:        row.SentAt,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
	message.Delivery = deliveryOf(message, false)

	return message
}

// deliveryOf reads a comment's state off its own stamps, so a row read without the doorbell
// joined to it reads as sent, which is the last thing the comment itself is a record of.
func deliveryOf(message Message, doorbellExpired bool) DeliveryState {
	switch {
	case message.AuthorKind != AuthorHuman:
		return DeliveryNone
	case message.SentAt == nil:
		return DeliveryUnsent
	case message.DeliveredAt != nil:
		return DeliveryDelivered
	case doorbellExpired:
		return DeliveryExpired
	default:
		return DeliverySent
	}
}

func threadNotFound(id string) error {
	return fmt.Errorf("%w: review thread %q", store.ErrNotFound, id)
}

func messageNotFound(id string) error {
	return fmt.Errorf("%w: review message %q", store.ErrNotFound, id)
}
