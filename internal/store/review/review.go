package review

import (
	"context"
	"fmt"
	"time"

	"komrad/db"
	"komrad/db/models"
	"komrad/internal/store"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/google/uuid"
)

// Thread is one conversation pinned to a place in a task's diff.
type Thread struct {
	ID     uuid.UUID
	TaskID uuid.UUID

	// ReviewID is the review the thread was opened in, nil on a thread a person opened, which
	// belongs to no review.
	ReviewID *uuid.UUID
	// AuthorKind is who opened the thread.
	AuthorKind agentv1.ReviewAuthorKind
	// AuthorAgentID is the agent that opened the thread, nil when a person or a forge user did.
	AuthorAgentID *uuid.UUID
	// SnapshotID is the snapshot the thread's line numbers were measured against.
	SnapshotID uuid.UUID
	// BaseSHA is the commit the old side of the line numbers was read against.
	BaseSHA string
	// SnapshotOrdinal names that snapshot the way the panel does, zero when it was not read.
	SnapshotOrdinal int32
	// ForgeThreadID is the forge thread this one mirrors, nil for one opened in komrad.
	ForgeThreadID       *uuid.UUID
	FilePath            string
	OldStart            int32
	OldEnd              int32
	NewStart            int32
	NewEnd              int32
	QuotedLines         []string
	LastAgentActivityAt *time.Time
	HumanReadAt         *time.Time

	// ResolvedByKind is whether a person or an agent settled the thread, empty while nobody has.
	ResolvedByKind agentv1.ReviewAuthorKind
	// ResolvedByAgentID is the agent that settled it, nil for a person or an unsettled thread.
	ResolvedByAgentID *uuid.UUID
	// ResolvedAt is when the thread was settled, nil while it is still owed an
	// answer.
	ResolvedAt    *time.Time
	UnreadByHuman bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Message is one thing said in a thread.
type Message struct {
	ID            uuid.UUID
	ThreadID      uuid.UUID
	AuthorKind    agentv1.ReviewAuthorKind
	AuthorAgentID *uuid.UUID
	Body          string

	// ReviewID is the review this message went out with, nil for an answer from the agent.
	ReviewID *uuid.UUID
	// ReviewReleasedAt is when that review was released, nil until then and on an answer.
	ReviewReleasedAt *time.Time
	// SnapshotID is the snapshot an agent's reply said answers the thread, nil on any other message.
	SnapshotID *uuid.UUID
	// Suggestion is the replacement lines the reviewer is proposing for the thread's range,
	// empty on an ordinary comment.
	Suggestion []string
	// External is the forge comment the person copied this from, nil for anything written in komrad.
	External *External
	// Held is whether the agent may not read this yet: a forge message whose author could not write
	// to the repository, or anything but an agent's reply in a thread such a message opened, until
	// the person accepts it.
	Held bool
	// AcceptedFrom is the held forge message this is the person's accepted copy of, nil otherwise.
	AcceptedFrom *uuid.UUID
	// MailID is the mail that carried a person's comment to the writer, nil on every other message
	// and once that mail was deleted.
	MailID *uuid.UUID
	// ReadAt is when the writer read the mail carrying a person's comment, nil until then.
	ReadAt *time.Time
	// ReadSnapshotID is the snapshot taken when the writer read a person's comment, which an
	// addressed reply is compared against.
	ReadSnapshotID *uuid.UUID
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// External is a forge comment as it stood when the person copied it into their review.
type External struct {
	// Login is the forge login of whoever wrote it, empty when the forge no longer knew them.
	Login     string
	CommentID uuid.UUID
	// EditedAt is when the forge comment had last been edited, nil when it never had been.
	EditedAt *time.Time
}

type ReviewStore struct {
	queries *models.Queries
}

// NewReviewStore builds a review store over db, which is either the pool or a transaction.
func NewReviewStore(db store.DBTX) *ReviewStore {
	return &ReviewStore{queries: models.New(db)}
}

// OpenedThread is a thread just opened and the id of its first message.
type OpenedThread struct {
	Thread
	MessageID uuid.UUID
}

type CreateThreadParams struct {
	TaskID uuid.UUID
	// ReviewID is the review the thread is opened in, nil for a person's thread.
	ReviewID      *uuid.UUID
	SnapshotID    uuid.UUID
	BaseSHA       string
	Suggestion    []string
	FilePath      string
	OldStart      int32
	OldEnd        int32
	NewStart      int32
	NewEnd        int32
	QuotedLines   []string
	Body          string
	AuthorKind    agentv1.ReviewAuthorKind
	AuthorAgentID *uuid.UUID
	// External is the forge comment the first message is copied from, nil for one written in komrad.
	External *External
	// ForgeThreadID is the forge thread this one mirrors, nil for one opened in komrad.
	ForgeThreadID *uuid.UUID
	// Held is whether the forge author of the first message could not write to the repository.
	Held bool
}

// CreateThread pins a conversation to a place in a diff and writes its first message in one
// statement.
func (s *ReviewStore) CreateThread(ctx context.Context, params CreateThreadParams) (*OpenedThread, error) {
	storedKind, err := fromServiceToDBReviewAuthorKind(params.AuthorKind)
	if err != nil {
		return nil, err
	}

	row, err := s.queries.InsertReviewThreadWithMessage(ctx, models.InsertReviewThreadWithMessageParams{
		TaskID:         params.TaskID,
		ReviewID:       params.ReviewID,
		SnapshotID:     params.SnapshotID,
		BaseSha:        params.BaseSHA,
		Suggestion:     lines(params.Suggestion),
		FilePath:       params.FilePath,
		OldStart:       params.OldStart,
		OldEnd:         params.OldEnd,
		NewStart:       params.NewStart,
		NewEnd:         params.NewEnd,
		QuotedLines:    lines(params.QuotedLines),
		AuthorKind:     storedKind,
		AuthorAgentID:  params.AuthorAgentID,
		Body:           params.Body,
		ExternalLogin:  externalLogin(params.External),
		ForgeCommentID: externalCommentID(params.External),
		ForgeEditedAt:  externalEditedAt(params.External),
		ForgeThreadID:  params.ForgeThreadID,
		Held:           params.Held,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open a review thread on %q: %w", params.FilePath, err)
	}

	authorKind, err := fromDBToServiceReviewAuthorKind(row.AuthorKind)
	if err != nil {
		return nil, err
	}
	resolvedBy, err := fromDBToServiceNullReviewAuthorKind(row.ResolvedByKind)
	if err != nil {
		return nil, err
	}

	return &OpenedThread{
		Thread: Thread{
			ID:                  row.ID,
			TaskID:              row.TaskID,
			ReviewID:            row.ReviewID,
			AuthorKind:          authorKind,
			AuthorAgentID:       row.AuthorAgentID,
			SnapshotID:          row.SnapshotID,
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
			ResolvedByKind:      resolvedBy,
			ResolvedByAgentID:   row.ResolvedByAgentID,
			ForgeThreadID:       row.ForgeThreadID,
			CreatedAt:           row.CreatedAt,
			UpdatedAt:           row.UpdatedAt,
		},
		MessageID: row.MessageID,
	}, nil
}

// ListThreads is every thread on a task, in the order they run down the diff.
func (s *ReviewStore) ListThreads(ctx context.Context, taskID uuid.UUID) ([]Thread, error) {
	rows, err := s.queries.ListReviewThreads(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the review threads on task %q: %w", taskID, err)
	}

	threads := make([]Thread, 0, len(rows))
	for _, row := range rows {
		authorKind, err := fromDBToServiceReviewAuthorKind(row.AuthorKind)
		if err != nil {
			return nil, err
		}
		resolvedBy, err := fromDBToServiceNullReviewAuthorKind(row.ResolvedByKind)
		if err != nil {
			return nil, err
		}

		thread := Thread{
			ID:                  row.ID,
			TaskID:              row.TaskID,
			ReviewID:            row.ReviewID,
			AuthorKind:          authorKind,
			AuthorAgentID:       row.AuthorAgentID,
			SnapshotID:          row.SnapshotID,
			BaseSHA:             row.BaseSha,
			SnapshotOrdinal:     row.SnapshotOrdinal,
			ForgeThreadID:       row.ForgeThreadID,
			FilePath:            row.FilePath,
			OldStart:            row.OldStart,
			OldEnd:              row.OldEnd,
			NewStart:            row.NewStart,
			NewEnd:              row.NewEnd,
			QuotedLines:         row.QuotedLines,
			LastAgentActivityAt: row.LastAgentActivityAt,
			HumanReadAt:         row.HumanReadAt,
			ResolvedAt:          row.ResolvedAt,
			ResolvedByKind:      resolvedBy,
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
func (s *ReviewStore) GetThread(ctx context.Context, id uuid.UUID) (*Thread, error) {
	row, err := s.queries.GetReviewThread(ctx, id)
	if db.IsNoRows(err) {
		return nil, threadNotFound(id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read the review thread %q: %w", id, err)
	}

	return threadFromRow(*row)
}

// MarkThreadRead stamps the person's read column from the agent's activity column.
func (s *ReviewStore) MarkThreadRead(ctx context.Context, id uuid.UUID) (*Thread, error) {
	row, err := s.queries.MarkReviewThreadRead(ctx, id)
	if db.IsNoRows(err) {
		return nil, threadNotFound(id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to mark the review thread %q read: %w", id, err)
	}

	return threadFromRow(*row)
}

// SetThreadResolvedParams names the thread to settle or reopen, and who does it.
type SetThreadResolvedParams struct {
	ID       uuid.UUID
	Resolved bool
	// ByKind is whether a person or an agent settled it, which reopening clears along with the
	// rest.
	ByKind agentv1.ReviewAuthorKind
	// ByAgentID is the agent that settled it, nil for a person.
	ByAgentID *uuid.UUID
}

// SetThreadResolved settles a thread or reopens it, recording who did.
func (s *ReviewStore) SetThreadResolved(ctx context.Context, params SetThreadResolvedParams) (*Thread, error) {
	resolvedBy, err := fromServiceToDBNullReviewAuthorKind(params.ByKind)
	if err != nil {
		return nil, err
	}

	row, err := s.queries.SetReviewThreadResolved(ctx, models.SetReviewThreadResolvedParams{
		ID:                params.ID,
		Resolved:          params.Resolved,
		ResolvedByKind:    resolvedBy,
		ResolvedByAgentID: params.ByAgentID,
	})
	if db.IsNoRows(err) {
		return nil, threadNotFound(params.ID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to settle the review thread %q: %w", params.ID, err)
	}

	return threadFromRow(*row)
}

type CreateMessageParams struct {
	ThreadID uuid.UUID
	// ReviewID is the batch the message goes out with, nil for an answer.
	ReviewID *uuid.UUID
	// SnapshotID is the snapshot a reply says answers the thread.
	SnapshotID *uuid.UUID
	Suggestion []string
	AuthorKind agentv1.ReviewAuthorKind
	// AuthorAgentID links to the agent that wrote it, and is cleared when that agent is deleted.
	AuthorAgentID *uuid.UUID
	Body          string
	// External is the forge comment the message is copied from, nil for one written in komrad.
	External *External
	// Held is whether the forge author could not write to the repository.
	Held bool
	// AcceptedFrom is the held forge message this is the person's accepted copy of.
	AcceptedFrom *uuid.UUID
}

// CreateMessage writes a message into a thread and moves the writer's activity stamp in one
// statement.
func (s *ReviewStore) CreateMessage(ctx context.Context, params CreateMessageParams) (*Message, error) {
	storedKind, err := fromServiceToDBReviewAuthorKind(params.AuthorKind)
	if err != nil {
		return nil, err
	}

	row, err := s.queries.InsertReviewMessage(ctx, models.InsertReviewMessageParams{
		ThreadID:       params.ThreadID,
		ReviewID:       params.ReviewID,
		SnapshotID:     params.SnapshotID,
		Suggestion:     lines(params.Suggestion),
		AuthorKind:     storedKind,
		AuthorAgentID:  params.AuthorAgentID,
		Body:           params.Body,
		ExternalLogin:  externalLogin(params.External),
		ForgeCommentID: externalCommentID(params.External),
		ForgeEditedAt:  externalEditedAt(params.External),
		Held:           params.Held,
		AcceptedFrom:   params.AcceptedFrom,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to write a message in the review thread %q: %w", params.ThreadID, err)
	}

	return messageFromRow(models.ReviewMessage(*row))
}

// UpdateMessage rewrites a message and moves its author's activity stamp with it.
func (s *ReviewStore) UpdateMessage(ctx context.Context, id uuid.UUID, body string, suggestion []string) (*Message, error) {
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

	return messageFromRow(models.ReviewMessage(*row))
}

// GetMessage is one message, ErrNotFound for an id that names nothing.
func (s *ReviewStore) GetMessage(ctx context.Context, id uuid.UUID) (*Message, error) {
	row, err := s.queries.GetReviewMessage(ctx, id)
	if db.IsNoRows(err) {
		return nil, messageNotFound(id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read the review message %q: %w", id, err)
	}

	return messageFromRow(*row)
}

// SetMessageMail records the mail that carried a person's comment to the writer.
func (s *ReviewStore) SetMessageMail(ctx context.Context, id, mailID uuid.UUID) error {
	if err := s.queries.SetReviewMessageMail(ctx, models.SetReviewMessageMailParams{ID: id, MailID: &mailID}); err != nil {
		return fmt.Errorf("failed to record the mail that carried the review message %q: %w", id, err)
	}

	return nil
}

// MarkMessagesRead stamps the comments the given mail carried as read now, against the snapshot
// taken as the writer read them, nil when its task had no worktree, and returns the ones not
// already stamped.
func (s *ReviewStore) MarkMessagesRead(ctx context.Context, mailIDs []uuid.UUID, snapshotID *uuid.UUID) ([]Message, error) {
	if len(mailIDs) == 0 {
		return nil, nil
	}

	rows, err := s.queries.MarkReviewMessagesRead(ctx, models.MarkReviewMessagesReadParams{
		MailIds:        mailIDs,
		ReadSnapshotID: snapshotID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to mark the comments of %d mail read: %w", len(mailIDs), err)
	}

	return xslice.TryMap(rows, func(row *models.ReviewMessage) (Message, error) {
		message, err := messageFromRow(*row)
		if err != nil {
			return Message{}, err
		}

		return *message, nil
	})
}

// ListMessages is every message in the given threads, oldest first within each.
func (s *ReviewStore) ListMessages(ctx context.Context, threadIDs []uuid.UUID) ([]Message, error) {
	if len(threadIDs) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ListReviewMessagesOfThreads(ctx, threadIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to list the messages of %d review thread(s): %w", len(threadIDs), err)
	}

	messages, err := xslice.TryMap(rows, func(row *models.ListReviewMessagesOfThreadsRow) (Message, error) {
		message, err := messageOfThreadFromRow(*row)
		if err != nil {
			return Message{}, err
		}

		return *message, nil
	})
	if err != nil {
		return nil, err
	}

	return holdThreads(messages), nil
}

// holdThreads marks held everything but an agent's reply in a thread a held forge message opened,
// until the person accepts a copy of that message.
func holdThreads(messages []Message) []Message {
	roots := map[uuid.UUID]Message{}
	accepted := map[uuid.UUID]bool{}
	for _, message := range messages {
		if root, ok := roots[message.ThreadID]; !ok || store.CompareIDs(message.ID, root.ID) < 0 {
			roots[message.ThreadID] = message
		}
		if message.AcceptedFrom != nil {
			accepted[*message.AcceptedFrom] = true
		}
	}

	for i, message := range messages {
		root := roots[message.ThreadID]
		if !root.Held || accepted[root.ID] || message.AuthorKind == agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT {
			continue
		}
		messages[i].Held = true
	}

	return messages
}

// ViewedFile is one file of a task's diff that the person reviewing has read.
type ViewedFile struct {
	TaskID      uuid.UUID
	FilePath    string
	Fingerprint string
	ViewedAt    time.Time
}

type MarkFileViewedParams struct {
	TaskID      uuid.UUID
	FilePath    string
	Fingerprint string
}

// MarkFileViewed records that a file has been read, storing the fingerprint of its diff content
// so the mark can be checked against a later patch.
func (s *ReviewStore) MarkFileViewed(ctx context.Context, params MarkFileViewedParams) (*ViewedFile, error) {
	row, err := s.queries.MarkReviewFileViewed(ctx, models.MarkReviewFileViewedParams{
		TaskID:      params.TaskID,
		FilePath:    params.FilePath,
		Fingerprint: params.Fingerprint,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to mark %q viewed on task %q: %w", params.FilePath, params.TaskID, err)
	}

	viewed := viewedFileFromRow(*row)

	return &viewed, nil
}

// UnmarkFileViewed drops the mark on one file.
func (s *ReviewStore) UnmarkFileViewed(ctx context.Context, taskID uuid.UUID, filePath string) error {
	err := s.queries.UnmarkReviewFileViewed(ctx, models.UnmarkReviewFileViewedParams{
		TaskID:   taskID,
		FilePath: filePath,
	})
	if err != nil {
		return fmt.Errorf("failed to unmark %q viewed on task %q: %w", filePath, taskID, err)
	}

	return nil
}

// ListViewedFiles is every file of a task marked read.
func (s *ReviewStore) ListViewedFiles(ctx context.Context, taskID uuid.UUID) ([]ViewedFile, error) {
	rows, err := s.queries.ListReviewViewedFiles(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the viewed files of task %q: %w", taskID, err)
	}

	viewed := xslice.Map(rows, func(row *models.ReviewViewedFile) ViewedFile {
		return viewedFileFromRow(*row)
	})

	return viewed, nil
}

func viewedFileFromRow(row models.ReviewViewedFile) ViewedFile {
	return ViewedFile{
		TaskID:      row.TaskID,
		FilePath:    row.FilePath,
		Fingerprint: row.Fingerprint,
		ViewedAt:    row.ViewedAt,
	}
}

func threadFromRow(row models.ReviewThread) (*Thread, error) {
	authorKind, err := fromDBToServiceReviewAuthorKind(row.AuthorKind)
	if err != nil {
		return nil, err
	}
	resolvedBy, err := fromDBToServiceNullReviewAuthorKind(row.ResolvedByKind)
	if err != nil {
		return nil, err
	}

	return &Thread{
		ID:                  row.ID,
		TaskID:              row.TaskID,
		ReviewID:            row.ReviewID,
		AuthorKind:          authorKind,
		AuthorAgentID:       row.AuthorAgentID,
		SnapshotID:          row.SnapshotID,
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
		ResolvedByKind:      resolvedBy,
		ResolvedByAgentID:   row.ResolvedByAgentID,
		ForgeThreadID:       row.ForgeThreadID,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
	}, nil
}

// messageOfThreadFromRow is a listed message with the release time of the review it went out with.
func messageOfThreadFromRow(row models.ListReviewMessagesOfThreadsRow) (*Message, error) {
	message, err := messageFromRow(row.ReviewMessage)
	if err != nil {
		return nil, err
	}
	message.ReviewReleasedAt = row.ReviewReleasedAt

	return message, nil
}

func messageFromRow(row models.ReviewMessage) (*Message, error) {
	authorKind, err := fromDBToServiceReviewAuthorKind(row.AuthorKind)
	if err != nil {
		return nil, err
	}

	return &Message{
		ID:             row.ID,
		ThreadID:       row.ThreadID,
		ReviewID:       row.ReviewID,
		SnapshotID:     row.SnapshotID,
		Suggestion:     row.Suggestion,
		AuthorKind:     authorKind,
		AuthorAgentID:  row.AuthorAgentID,
		Body:           row.Body,
		External:       externalOf(row.ExternalLogin, row.ForgeCommentID, row.ForgeEditedAt),
		Held:           row.Held,
		AcceptedFrom:   row.AcceptedFrom,
		MailID:         row.MailID,
		ReadAt:         row.ReadAt,
		ReadSnapshotID: row.ReadSnapshotID,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}, nil
}

// lines turns a nil slice into the empty array a text[] column expects.
func lines(values []string) []string {
	if values == nil {
		return []string{}
	}

	return values
}

func threadNotFound(id uuid.UUID) error {
	return fmt.Errorf("%w: review thread %q", store.ErrNotFound, id)
}

func messageNotFound(id uuid.UUID) error {
	return fmt.Errorf("%w: review message %q", store.ErrNotFound, id)
}

func externalOf(login *string, commentID *uuid.UUID, editedAt *time.Time) *External {
	if commentID == nil {
		return nil
	}

	return &External{
		Login:     xptr.Deref(login),
		CommentID: *commentID,
		EditedAt:  editedAt,
	}
}

func externalLogin(external *External) *string {
	if external == nil {
		return nil
	}

	return &external.Login
}

func externalCommentID(external *External) *uuid.UUID {
	if external == nil {
		return nil
	}

	return &external.CommentID
}

func externalEditedAt(external *External) *time.Time {
	if external == nil {
		return nil
	}

	return external.EditedAt
}
