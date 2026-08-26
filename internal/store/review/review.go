// Package review stores the conversations a person pins to the lines of an agent's diff, and
// the messages in them, whether the person or the agent wrote them.
//
// What this package exposes is whole conversation operations rather than the two tables
// underneath them. Writing a message and moving the thread's activity watermark is one fact
// about the world and has to land as one, or a reader can see a message the watermark does
// not account for and call it read; so the transaction boundary lives here rather than above.
package review

import (
	"context"
	"errors"
	"fmt"
	"time"

	"weave/db"
	"weave/db/models"
	"weave/internal/store"
)

// ErrNotAuthor is answered when a caller tries to rewrite or remove somebody else's message.
var ErrNotAuthor = errors.New("that message was written by somebody else")

// ReviewAuthorKind is who wrote a message.
type ReviewAuthorKind string

const (
	// ReviewAuthorHuman is the person reviewing.
	ReviewAuthorHuman ReviewAuthorKind = ReviewAuthorKind(models.ReviewAuthorKindHuman)
	// ReviewAuthorAgent is the agent whose diff is being reviewed.
	ReviewAuthorAgent ReviewAuthorKind = ReviewAuthorKind(models.ReviewAuthorKindAgent)
)

// Author is who a write belongs to. An agent names itself; the person reviewing names nobody.
type Author struct {
	Kind ReviewAuthorKind

	AgentID *string
}

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

	LastHumanActivityAt *time.Time
	LastAgentActivityAt *time.Time
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

// IsAuthoredBy reports whether a write claiming to come from author may touch this message.
// An agent owns only what it wrote itself, so one agent cannot rewrite another's reply even
// though both are agents.
func (m ReviewMessage) IsAuthoredBy(author Author) bool {
	if m.AuthorKind != author.Kind {
		return false
	}
	if author.Kind != ReviewAuthorAgent {
		return true
	}

	return author.AgentID != nil && m.AuthorAgentID != nil && *author.AgentID == *m.AuthorAgentID
}

// ThreadWithMessages is a thread and everything said in it.
type ThreadWithMessages struct {
	Thread ReviewThread

	Messages []ReviewMessage
}

// database is the pool a repository runs against: a handle for a single statement, and a
// transaction for the operations that are more than one.
type database interface {
	GetDB() store.DBTX
	InTx(ctx context.Context, fn func(store.DBTX) error) error
}

// Repository is the review, as conversations rather than as rows. Every method here either
// happens whole or does not happen: a message and the watermark that accounts for it are
// written together, and a read that claims what it returns claims exactly that.
type Repository struct {
	db database
}

// NewRepository builds the repository over the pool, which it needs rather than a plain
// handle because it owns the transactions its operations run in.
func NewRepository(db database) *Repository { return &Repository{db: db} }

type CreateThreadParams struct {
	AgentID  string
	FilePath string

	OldStart int32
	OldEnd   int32
	NewStart int32
	NewEnd   int32

	QuotedLines []string

	Body   string
	Author Author
}

// CreateThreadWithMessage pins a conversation to a place in a diff and writes the first thing
// said in it. The thread's activity watermark and the message's own timestamp are the same
// value, taken from the one statement they are written by, so a reader stamping the watermark
// it was shown can clear the thread exactly.
func (r *Repository) CreateThreadWithMessage(
	ctx context.Context,
	params CreateThreadParams,
) (*ThreadWithMessages, error) {
	row, err := models.New(r.db.GetDB()).InsertReviewThreadWithMessage(ctx, models.InsertReviewThreadWithMessageParams{
		AgentID:       params.AgentID,
		FilePath:      params.FilePath,
		OldStart:      params.OldStart,
		OldEnd:        params.OldEnd,
		NewStart:      params.NewStart,
		NewEnd:        params.NewEnd,
		QuotedLines:   orEmpty(params.QuotedLines),
		AuthorKind:    models.ReviewAuthorKind(params.Author.Kind),
		AuthorAgentID: params.Author.AgentID,
		Body:          params.Body,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open a review thread on %q: %w", params.FilePath, err)
	}

	return &ThreadWithMessages{
		Thread: threadFromRow(models.ReviewThread{
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
			LastAgentActivityAt: row.LastAgentActivityAt,
			AgentReadAt:         row.AgentReadAt,
			HumanReadAt:         row.HumanReadAt,
			CreatedAt:           row.CreatedAt,
			UpdatedAt:           row.UpdatedAt,
		}),
		Messages: []ReviewMessage{messageFromRow(models.ReviewMessage{
			ID:            row.MessageID,
			ThreadID:      row.ID,
			AuthorKind:    row.MessageAuthorKind,
			AuthorAgentID: row.MessageAuthorAgentID,
			Body:          row.MessageBody,
			CreatedAt:     row.MessageCreatedAt,
			UpdatedAt:     row.MessageUpdatedAt,
		})},
	}, nil
}

type AddMessageParams struct {
	ThreadID string
	Body     string
	Author   Author
}

// AddMessage says something in an existing thread, reopening it and advancing the author's
// side of the activity watermark in the same statement. It answers nil for a thread that no
// longer exists rather than failing on the foreign key.
func (r *Repository) AddMessage(ctx context.Context, params AddMessageParams) (*ReviewMessage, error) {
	if !store.IsID(params.ThreadID) {
		return nil, nil
	}

	var written *ReviewMessage
	err := r.db.InTx(ctx, func(handle store.DBTX) error {
		queries := models.New(handle)

		if _, err := queries.GetReviewThread(ctx, params.ThreadID); err != nil {
			if db.IsNoRows(err) {
				return nil
			}

			return fmt.Errorf("failed to read the review thread %q: %w", params.ThreadID, err)
		}

		row, err := queries.InsertReviewMessage(ctx, models.InsertReviewMessageParams{
			ThreadID:      params.ThreadID,
			AuthorKind:    models.ReviewAuthorKind(params.Author.Kind),
			AuthorAgentID: params.Author.AgentID,
			Body:          params.Body,
		})
		if err != nil {
			return fmt.Errorf("failed to write a message in the review thread %q: %w", params.ThreadID, err)
		}

		message := messageFromRow(models.ReviewMessage(*row))
		written = &message

		return nil
	})
	if err != nil {
		return nil, err
	}

	return written, nil
}

type EditMessageParams struct {
	ID     string
	Body   string
	Author Author
}

// EditMessage rewrites what was said, which counts as new activity: the other side was told
// the old text and the thread now says something else. The check that the caller wrote the
// message and the rewrite are one transaction, so a message cannot be edited out from under
// the check.
func (r *Repository) EditMessage(ctx context.Context, params EditMessageParams) (*ReviewMessage, error) {
	if !store.IsID(params.ID) {
		return nil, nil
	}

	var written *ReviewMessage
	err := r.db.InTx(ctx, func(handle store.DBTX) error {
		queries := models.New(handle)

		existing, err := queries.GetReviewMessage(ctx, params.ID)
		if db.IsNoRows(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to read the review message %q: %w", params.ID, err)
		}
		if !messageFromRow(*existing).IsAuthoredBy(params.Author) {
			return fmt.Errorf("%w: %q", ErrNotAuthor, params.ID)
		}

		row, err := queries.UpdateReviewMessageBody(ctx, models.UpdateReviewMessageBodyParams{
			ID:   params.ID,
			Body: params.Body,
		})
		if err != nil {
			return fmt.Errorf("failed to rewrite the review message %q: %w", params.ID, err)
		}

		message := messageFromRow(models.ReviewMessage(*row))
		written = &message

		return nil
	})
	if err != nil {
		return nil, err
	}

	return written, nil
}

// DeleteMessage removes one message, leaving the thread it was in, and pulls the activity
// watermarks back to what is left so a thread does not stay unread with nothing in it to
// read. It answers false when no message holds that id.
func (r *Repository) DeleteMessage(ctx context.Context, id string, author Author) (bool, error) {
	if !store.IsID(id) {
		return false, nil
	}

	deleted := false
	err := r.db.InTx(ctx, func(handle store.DBTX) error {
		queries := models.New(handle)

		existing, err := queries.GetReviewMessage(ctx, id)
		if db.IsNoRows(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to read the review message %q: %w", id, err)
		}
		if !messageFromRow(*existing).IsAuthoredBy(author) {
			return fmt.Errorf("%w: %q", ErrNotAuthor, id)
		}

		if err := queries.DeleteReviewMessage(ctx, id); err != nil {
			return fmt.Errorf("failed to delete the review message %q: %w", id, err)
		}
		deleted = true

		return nil
	})
	if err != nil {
		return false, err
	}

	return deleted, nil
}

type ListThreadsParams struct {
	AgentID string
	// MarkRead claims what is returned for the agent whose diff this is, which is what stops
	// the same activity being announced to it again. The person reviewing claims a thread by
	// opening it, one at a time, rather than by listing.
	MarkRead bool
}

// ListAndMarkRead is every thread on an agent's diff with its messages.
//
// Claiming stamps each thread's watermark from the activity value the claim itself read,
// inside the same statement, so the agent is never told it has read something that was
// written while it was reading. It runs in one transaction with the messages it returns for
// the same reason.
func (r *Repository) ListAndMarkRead(ctx context.Context, params ListThreadsParams) ([]ThreadWithMessages, error) {
	if !store.IsID(params.AgentID) {
		return nil, nil
	}

	var listed []ThreadWithMessages
	err := r.db.InTx(ctx, func(handle store.DBTX) error {
		queries := models.New(handle)

		threads, err := readThreads(ctx, queries, params)
		if err != nil {
			return err
		}
		if len(threads) == 0 {
			return nil
		}

		ids := make([]string, 0, len(threads))
		for _, thread := range threads {
			ids = append(ids, thread.ID)
		}

		rows, err := queries.ListReviewMessagesByThreads(ctx, ids)
		if err != nil {
			return fmt.Errorf("failed to list the messages of %d review thread(s): %w", len(ids), err)
		}

		byThread := map[string][]ReviewMessage{}
		for _, row := range rows {
			byThread[row.ThreadID] = append(byThread[row.ThreadID], messageFromRow(*row))
		}

		listed = make([]ThreadWithMessages, 0, len(threads))
		for _, thread := range threads {
			listed = append(listed, ThreadWithMessages{Thread: thread, Messages: byThread[thread.ID]})
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return listed, nil
}

// MarkThreadReadByHuman is the person reviewing acknowledging one conversation, which is what
// opening it on the thread screen means. It stamps their watermark from the agent's activity
// value, so a reply written afterwards still shows as something they have not seen.
func (r *Repository) MarkThreadReadByHuman(ctx context.Context, id string) (*ReviewThread, error) {
	if !store.IsID(id) {
		return nil, nil
	}

	row, err := models.New(r.db.GetDB()).MarkReviewThreadReadByHuman(ctx, id)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to mark the review thread %q read: %w", id, err)
	}

	thread := threadFromRow(*row)

	return &thread, nil
}

// GetThread is one thread, or nil when nothing holds that id.
func (r *Repository) GetThread(ctx context.Context, id string) (*ReviewThread, error) {
	if !store.IsID(id) {
		return nil, nil
	}

	row, err := models.New(r.db.GetDB()).GetReviewThread(ctx, id)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read the review thread %q: %w", id, err)
	}

	thread := threadFromRow(row.ReviewThread)

	return &thread, nil
}

// GetMessage is one message, or nil when nothing holds that id.
func (r *Repository) GetMessage(ctx context.Context, id string) (*ReviewMessage, error) {
	if !store.IsID(id) {
		return nil, nil
	}

	row, err := models.New(r.db.GetDB()).GetReviewMessage(ctx, id)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read the review message %q: %w", id, err)
	}

	message := messageFromRow(*row)

	return &message, nil
}

// SetResolved settles a thread or reopens it, and answers nil for an id that holds none.
func (r *Repository) SetResolved(ctx context.Context, id string, resolved bool) (*ReviewThread, error) {
	if !store.IsID(id) {
		return nil, nil
	}

	var resolvedAt *time.Time
	if resolved {
		now := time.Now()
		resolvedAt = &now
	}

	row, err := models.New(r.db.GetDB()).SetReviewThreadResolved(ctx, models.SetReviewThreadResolvedParams{
		ID:         id,
		ResolvedAt: resolvedAt,
	})
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to settle the review thread %q: %w", id, err)
	}

	thread := threadFromRow(*row)

	return &thread, nil
}

// DeleteThread removes a thread and every message in it.
func (r *Repository) DeleteThread(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := models.New(r.db.GetDB()).DeleteReviewThread(ctx, id); err != nil {
		return fmt.Errorf("failed to delete the review thread %q: %w", id, err)
	}

	return nil
}

// CountUnread is how many open threads hold something the agent has not been told about,
// which is what decides whether there is anything to tell it.
func (r *Repository) CountUnread(ctx context.Context, agentID string) (int64, error) {
	if !store.IsID(agentID) {
		return 0, nil
	}

	count, err := models.New(r.db.GetDB()).CountUnreadReviewThreads(ctx, agentID)
	if err != nil {
		return 0, fmt.Errorf("failed to count unread review threads for agent %q: %w", agentID, err)
	}

	return count, nil
}

// ListAgentsWithUnread is every agent whose diff holds review activity it has not read. It is
// what the doorbell is reconciled against, since an agent that never claimed its mail has to
// be told again rather than left waiting on a notice that expired.
func (r *Repository) ListAgentsWithUnread(ctx context.Context) ([]string, error) {
	ids, err := models.New(r.db.GetDB()).ListAgentsWithUnreadReviewThreads(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the agents with unread review activity: %w", err)
	}

	return ids, nil
}

func readThreads(ctx context.Context, queries *models.Queries, params ListThreadsParams) ([]ReviewThread, error) {
	if params.MarkRead {
		rows, err := queries.ClaimReviewThreadsForAgent(ctx, params.AgentID)
		if err != nil {
			return nil, fmt.Errorf("failed to claim the review threads on agent %q: %w", params.AgentID, err)
		}

		threads := make([]ReviewThread, 0, len(rows))
		for _, row := range rows {
			threads = append(threads, threadFromRow(row.ReviewThread))
		}

		return threads, nil
	}

	rows, err := queries.ListReviewThreads(ctx, params.AgentID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the review threads on agent %q: %w", params.AgentID, err)
	}

	threads := make([]ReviewThread, 0, len(rows))
	for _, row := range rows {
		threads = append(threads, threadFromRow(row.ReviewThread))
	}

	return threads, nil
}

func threadFromRow(row models.ReviewThread) ReviewThread {
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
		LastAgentActivityAt: row.LastAgentActivityAt,
		AgentReadAt:         row.AgentReadAt,
		HumanReadAt:         row.HumanReadAt,
		UnreadByAgent:       row.LastHumanActivityAt != nil && isAfter(row.LastHumanActivityAt, row.AgentReadAt),
		UnreadByHuman:       row.LastAgentActivityAt != nil && isAfter(row.LastAgentActivityAt, row.HumanReadAt),
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
	}
}

func messageFromRow(row models.ReviewMessage) ReviewMessage {
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

// isAfter is the unread comparison the queries make, for the rows that come back from a write
// rather than from one of them: activity nobody has read yet counts as unread.
func isAfter(activity, read *time.Time) bool {
	return read == nil || activity.After(*read)
}

func orEmpty(lines []string) []string {
	if lines == nil {
		return []string{}
	}

	return lines
}
