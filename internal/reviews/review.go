package reviews

import (
	"context"
	"errors"
	"fmt"

	"komrad/internal/events"
	"komrad/internal/fail"
	"komrad/internal/mail"
	"komrad/internal/reviews/history"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	reviewstore "komrad/internal/store/review"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/oneofs"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

// ErrReviewThreadNotFound answers a thread id that names nothing.
var ErrReviewThreadNotFound = fail.NotFound("review thread not found")

// ErrReviewMessageNotFound answers a message id that names nothing.
var ErrReviewMessageNotFound = fail.NotFound("review message not found")

// ErrReviewNotYours answers a caller that writes to or claims another agent's review, which
// it may still read.
var ErrReviewNotYours = fail.Denied("that review belongs to another agent")

// ErrReviewHumanOnly answers an agent that asks for something only the person reviewing
// does, such as accepting a forge comment or marking a file viewed.
var ErrReviewHumanOnly = fail.Denied("only the person reviewing can do that")

// errReviewThreadHeld answers a reply written in a forge thread whose first comment is
// still held and not yet accepted, which the agent could not read the reply alongside.
var errReviewThreadHeld = fail.Precondition("this thread opens with a held forge comment, so accept it before replying")

type eventRecorder interface {
	InsertManyTx(ctx context.Context, build func(tx pgx.Tx) ([]river.JobArgs, error)) error
}

// taskFinder reads the task a caller named by its id or by its slug.
type taskFinder interface {
	FindTask(ctx context.Context, task oneofs.Task) (*taskstore.Task, error)
}

// ReviewAuthor is who a call is, taken from the calling identity: the panel names no agent and
// acts as the person, and an agent names itself.
type ReviewAuthor struct {
	Kind    agentv1.ReviewAuthorKind
	AgentID *uuid.UUID
}

// isHuman reports whether the call came from the person reviewing rather than from an agent.
func (a ReviewAuthor) isHuman() bool {
	return a.Kind == agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN
}

// ReviewThreadWithMessages is a thread and everything said in it.
type ReviewThreadWithMessages struct {
	Thread   reviewstore.Thread
	Messages []reviewstore.Message
}

// NewReviewServiceParams is everything the review service reads and tells.
type NewReviewServiceParams struct {
	Reviews    *reviewstore.ReviewStore
	Snapshots  reviewSnapshotter
	Workflows  *workflowstore.WorkflowStore
	Agents     *agentstore.AgentStore
	Tasks      *taskstore.TaskStore
	TaskFinder taskFinder
	Events     eventRecorder
	Mailer     *mail.MailService
	Git        history.Git
	Tracer     *history.Tracer
	// StateDir is where komrad keeps its own files.
	StateDir string
}

// ReviewService owns the conversations pinned to the lines of a task's diff, and holds the only
// copy.
type ReviewService struct {
	reviews       *reviewstore.ReviewStore
	snapshots     reviewSnapshotter
	workflows     *workflowstore.WorkflowStore
	agents        *agentstore.AgentStore
	tasks         *taskstore.TaskStore
	taskFinder    taskFinder
	events        eventRecorder
	mailer        *mail.MailService
	git           history.Git
	tracer        *history.Tracer
	stateDir      string
	snapshotLocks *taskLocks
}

// NewReviewService builds the service over the review store and the things it tells.
func NewReviewService(params NewReviewServiceParams) *ReviewService {
	return &ReviewService{
		reviews:       params.Reviews,
		snapshots:     params.Snapshots,
		workflows:     params.Workflows,
		agents:        params.Agents,
		tasks:         params.Tasks,
		taskFinder:    params.TaskFinder,
		events:        params.Events,
		mailer:        params.Mailer,
		git:           params.Git,
		tracer:        params.Tracer,
		stateDir:      params.StateDir,
		snapshotLocks: newTaskLocks(),
	}
}

// ListReviewThreadsParams says which task's diff to read the conversations of, and as whom.
type ListReviewThreadsParams struct {
	// Task names the task, left empty for the caller's own.
	Task   oneofs.Task
	Author ReviewAuthor
}

// ListReviewThreads is every thread on a task with its messages, all of it for the
// person reviewing and only the released part for an agent.
func (s *ReviewService) ListReviewThreads(
	ctx context.Context,
	params ListReviewThreadsParams,
) ([]ReviewThreadWithMessages, error) {
	task, err := s.getTask(ctx, params.Task, params.Author)
	if err != nil {
		return nil, err
	}

	return s.listThreads(ctx, task.ID, params.Author)
}

// listThreads is every thread on a task's diff with what the author may read of its messages.
func (s *ReviewService) listThreads(
	ctx context.Context,
	taskID uuid.UUID,
	author ReviewAuthor,
) ([]ReviewThreadWithMessages, error) {
	threads, err := s.reviews.ListThreads(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if len(threads) == 0 {
		return nil, nil
	}

	ids := xslice.Map(threads, func(thread reviewstore.Thread) uuid.UUID {
		return thread.ID
	})

	messages, err := s.reviews.ListMessages(ctx, ids)
	if err != nil {
		return nil, err
	}

	byThread := map[uuid.UUID][]reviewstore.Message{}
	for _, message := range messages {
		byThread[message.ThreadID] = append(byThread[message.ThreadID], message)
	}

	answered := make([]ReviewThreadWithMessages, 0, len(threads))
	for _, thread := range threads {
		messages := byThread[thread.ID]
		if !author.isHuman() {
			messages = releasedMessages(messages, author)
			if len(messages) == 0 {
				continue
			}
			thread.UnreadByHuman = false
		}

		answered = append(answered, ReviewThreadWithMessages{Thread: thread, Messages: messages})
	}

	return answered, nil
}

// doesOwnReview reports whether an author is reading back a thread it opened itself, which it
// may see whole.
func doesOwnReview(author ReviewAuthor, thread reviewstore.Thread) bool {
	return author.AgentID != nil &&
		thread.AuthorAgentID != nil &&
		*thread.AuthorAgentID == *author.AgentID
}

// releasedMessages is what an agent may be shown of a conversation, decided by the review a
// message belongs to rather than by who wrote it.
func releasedMessages(messages []reviewstore.Message, author ReviewAuthor) []reviewstore.Message {
	return xslice.Filter(messages, func(message reviewstore.Message) bool {
		return isReadableBy(message, author)
	})
}

// isReadableBy reports whether one message has been released, or is the reader's own.
func isReadableBy(message reviewstore.Message, author ReviewAuthor) bool {
	switch {
	case message.Held:
		return false
	case message.ReviewID == nil:
		return true
	case message.ReviewReleasedAt != nil:
		return true
	default:
		return message.AuthorAgentID != nil &&
			author.AgentID != nil &&
			*message.AuthorAgentID == *author.AgentID
	}
}

// CreateReviewThreadParams is the place a new conversation is pinned to and what it says.
type CreateReviewThreadParams struct {
	// Task names the task whose diff the thread is pinned to, left empty for the caller's own.
	Task oneofs.Task
	// SnapshotID is the snapshot the anchor's line numbers were measured in, empty for a caller that
	// leaves the snapshot to be worked out from the commit it names.
	SnapshotID uuid.UUID
	// Suggestion is replacement lines the reviewer proposes for the range, empty on an ordinary
	// comment.
	Suggestion []string
	// BaseSHA is the commit the old side of these line numbers was read against, empty for the
	// snapshot's own base.
	BaseSHA string
	// SnapshotSHA is the commit the new side of these line numbers was read against, empty for a
	// caller that read the snapshot its own review sits on.
	SnapshotSHA string
	// FilePath is the file the thread is pinned to, empty for a remark on the change as a whole.
	FilePath    string
	OldStart    int32
	OldEnd      int32
	NewStart    int32
	NewEnd      int32
	QuotedLines []string
	Body        string
	Author      ReviewAuthor
}

// CreateReviewThread pins a conversation to a place in a snapshot and writes its first message.
func (s *ReviewService) CreateReviewThread(
	ctx context.Context,
	params CreateReviewThreadParams,
) (*ReviewThreadWithMessages, error) {
	task, err := s.getTask(ctx, params.Task, params.Author)
	if err != nil {
		return nil, err
	}

	place, err := s.placeThread(ctx, *task, params)
	if err != nil {
		return nil, err
	}

	var opened *reviewstore.OpenedThread
	err = s.events.InsertManyTx(ctx, func(tx pgx.Tx) ([]river.JobArgs, error) {
		var err error
		opened, err = reviewstore.NewReviewStore(tx).CreateThread(ctx, reviewstore.CreateThreadParams{
			TaskID:        task.ID,
			ReviewID:      place.ReviewID,
			SnapshotID:    place.SnapshotID,
			BaseSHA:       place.BaseSHA,
			Suggestion:    params.Suggestion,
			FilePath:      params.FilePath,
			OldStart:      params.OldStart,
			OldEnd:        params.OldEnd,
			NewStart:      params.NewStart,
			NewEnd:        params.NewEnd,
			QuotedLines:   params.QuotedLines,
			Body:          params.Body,
			AuthorKind:    params.Author.Kind,
			AuthorAgentID: params.Author.AgentID,
		})
		if err != nil || !params.Author.isHuman() {
			return nil, err
		}

		return nil, s.mailComment(ctx, tx, mailCommentParams{
			Thread: opened.Thread,
			Message: reviewstore.Message{
				ID:         opened.MessageID,
				Body:       params.Body,
				Suggestion: params.Suggestion,
			},
		})
	})
	if err != nil {
		return nil, err
	}

	messages, err := s.reviews.ListMessages(ctx, []uuid.UUID{opened.ID})
	if err != nil {
		return nil, err
	}

	return &ReviewThreadWithMessages{Thread: opened.Thread, Messages: messages}, nil
}

// threadPlace is the review, snapshot and base a new thread is filed under.
type threadPlace struct {
	// ReviewID is the agent's open review, nil for a person's thread.
	ReviewID   *uuid.UUID
	SnapshotID uuid.UUID
	BaseSHA    string
}

// placeThread finds the snapshot a new thread's lines were measured in and, for an agent, its
// open review on it, opening one when needed.
func (s *ReviewService) placeThread(
	ctx context.Context,
	task taskstore.Task,
	params CreateReviewThreadParams,
) (*threadPlace, error) {
	if err := s.requireEntitled(ctx, task, params.Author); err != nil {
		return nil, err
	}

	snapshot, err := s.snapshotFor(ctx, task.ID, params)
	if err != nil {
		return nil, err
	}

	place := &threadPlace{SnapshotID: snapshot.ID, BaseSHA: params.BaseSHA}
	if place.BaseSHA == "" {
		place.BaseSHA = snapshot.BaseSHA
	}
	if params.Author.isHuman() {
		return place, nil
	}

	open, err := s.openReviewFor(ctx, openReviewForParams{
		TaskID:   task.ID,
		Author:   params.Author,
		Snapshot: snapshot,
	})
	if err != nil {
		return nil, err
	}
	if open.SnapshotID != snapshot.ID {
		held, err := s.reviews.GetSnapshot(ctx, open.SnapshotID)
		if err != nil {
			return nil, err
		}

		return nil, fmt.Errorf("%w: send the comments you have on snapshot %d first", errReviewInFlight, held.Ordinal)
	}

	place.ReviewID = &open.ID

	return place, nil
}

// AddReviewMessageResult is the message as written and anything worth telling the one who wrote it.
type AddReviewMessageResult struct {
	Message reviewstore.Message
	// Hint points a plain reply at addressed when the worktree has moved since the thread was written.
	Hint string
}

// AddReviewMessageParams is what to say in an existing thread, and as whom.
type AddReviewMessageParams struct {
	ThreadID   uuid.UUID
	Body       string
	Suggestion []string
	Author     ReviewAuthor
	// Addressed says the agent under review made the change the thread asks for.
	Addressed bool
}

// AddReviewMessage says something in an existing thread: under the review a reviewing agent has
// open, under none for the agent answering, and under none for the person, whose words reach
// the task's writer as mail at once.
func (s *ReviewService) AddReviewMessage(
	ctx context.Context,
	params AddReviewMessageParams,
) (*AddReviewMessageResult, error) {
	thread, err := s.getOwnThread(ctx, params.ThreadID, params.Author)
	if err != nil {
		return nil, err
	}

	answering, err := s.isTaskWriter(ctx, params.Author, thread.TaskID)
	if err != nil {
		return nil, err
	}
	if params.Addressed && !answering {
		return nil, errReviewAddressedByOwner
	}

	held, err := s.isThreadHeld(ctx, thread.ID)
	if err != nil {
		return nil, err
	}
	if held {
		return nil, errReviewThreadHeld
	}

	var reviewID *uuid.UUID
	if !answering && !params.Author.isHuman() {
		open, err := s.openReviewFor(ctx, openReviewForParams{
			TaskID: thread.TaskID,
			Author: params.Author,
		})
		if err != nil {
			return nil, err
		}

		reviewID = &open.ID
	}

	var snapshotID *uuid.UUID
	if params.Addressed {
		answer, err := s.takeAnswerSnapshot(ctx, *thread)
		if err != nil {
			return nil, err
		}

		snapshotID = &answer.ID
	}

	var message *reviewstore.Message
	err = s.events.InsertManyTx(ctx, func(tx pgx.Tx) ([]river.JobArgs, error) {
		var err error
		message, err = reviewstore.NewReviewStore(tx).CreateMessage(ctx, reviewstore.CreateMessageParams{
			ThreadID:      params.ThreadID,
			ReviewID:      reviewID,
			SnapshotID:    snapshotID,
			Suggestion:    params.Suggestion,
			AuthorKind:    params.Author.Kind,
			AuthorAgentID: params.Author.AgentID,
			Body:          params.Body,
		})
		if err != nil {
			return nil, err
		}
		if params.Author.isHuman() {
			return nil, s.mailComment(ctx, tx, mailCommentParams{Thread: *thread, Message: *message, Reply: true})
		}
		if !answering || thread.AuthorAgentID == nil {
			return nil, nil
		}

		event, err := events.NewEvent(answered{
			TaskID:     thread.TaskID,
			WriterID:   *params.Author.AgentID,
			ReviewerID: *thread.AuthorAgentID,
		})
		if err != nil {
			return nil, err
		}

		return []river.JobArgs{*event}, nil
	})
	if err != nil {
		return nil, err
	}

	result := &AddReviewMessageResult{Message: *message}
	if answering && !params.Addressed {
		result.Hint = s.findReplyHint(ctx, *thread)
	}

	return result, nil
}

// EditReviewMessageParams is the message to rewrite and what it should say instead.
type EditReviewMessageParams struct {
	ID         uuid.UUID
	Body       string
	Suggestion []string
	Author     ReviewAuthor
}

// EditReviewMessage rewrites what was said, which only its author may do and which counts as
// new activity.
func (s *ReviewService) EditReviewMessage(
	ctx context.Context,
	params EditReviewMessageParams,
) (*reviewstore.Message, error) {
	if _, err := s.getOwnMessage(ctx, params.ID, params.Author); err != nil {
		return nil, err
	}

	return s.reviews.UpdateMessage(ctx, params.ID, params.Body, params.Suggestion)
}

// MarkReviewThreadReadParams names the thread to catch the person up on.
type MarkReviewThreadReadParams struct {
	ID     uuid.UUID
	Author ReviewAuthor
}

// MarkReviewThreadRead catches the person up on a single conversation, and is theirs alone.
func (s *ReviewService) MarkReviewThreadRead(
	ctx context.Context,
	params MarkReviewThreadReadParams,
) (*reviewstore.Thread, error) {
	if !params.Author.isHuman() {
		return nil, fmt.Errorf("%w: marking a review thread read", ErrReviewHumanOnly)
	}

	if err := s.requireOwnThread(ctx, params.ID, params.Author); err != nil {
		return nil, err
	}

	return s.reviews.MarkThreadRead(ctx, params.ID)
}

// SetReviewThreadResolvedParams names the thread to settle or reopen.
type SetReviewThreadResolvedParams struct {
	ID       uuid.UUID
	Resolved bool
	Author   ReviewAuthor
}

// SetReviewThreadResolved settles a thread or reopens it, which the author of the comment and the
// person reviewing may do.
func (s *ReviewService) SetReviewThreadResolved(
	ctx context.Context,
	params SetReviewThreadResolvedParams,
) (*reviewstore.Thread, error) {
	thread, err := s.getOwnThread(ctx, params.ID, params.Author)
	if err != nil {
		return nil, err
	}

	if !params.Author.isHuman() && !doesOwnReview(params.Author, *thread) {
		return nil, ErrReviewNotYours.Saying(
			"%q is a comment about your own work, so it is not yours to settle", params.ID)
	}

	return s.reviews.SetThreadResolved(ctx, reviewstore.SetThreadResolvedParams{
		ID:        params.ID,
		Resolved:  params.Resolved,
		ByKind:    params.Author.Kind,
		ByAgentID: params.Author.AgentID,
	})
}

// SetReviewFileViewedParams names one file of a task's diff to mark read or unread.
type SetReviewFileViewedParams struct {
	// Task names the task.
	Task        oneofs.Task
	FilePath    string
	Fingerprint string
	Viewed      bool
	Author      ReviewAuthor
}

// SetReviewFileViewed marks one file read or drops the mark, which only the person reviewing
// may do.
func (s *ReviewService) SetReviewFileViewed(
	ctx context.Context,
	params SetReviewFileViewedParams,
) (*reviewstore.ViewedFile, error) {
	if !params.Author.isHuman() {
		return nil, fmt.Errorf("%w: marking a file viewed", ErrReviewHumanOnly)
	}

	task, err := s.getTask(ctx, params.Task, params.Author)
	if err != nil {
		return nil, err
	}

	if !params.Viewed {
		return nil, s.reviews.UnmarkFileViewed(ctx, task.ID, params.FilePath)
	}

	return s.reviews.MarkFileViewed(ctx, reviewstore.MarkFileViewedParams{
		TaskID:      task.ID,
		FilePath:    params.FilePath,
		Fingerprint: params.Fingerprint,
	})
}

// ListReviewViewedFilesParams names the task whose read files are wanted.
type ListReviewViewedFilesParams struct {
	// Task names the task.
	Task   oneofs.Task
	Author ReviewAuthor
}

// ListReviewViewedFiles is every file of a task the person reviewing has marked read.
func (s *ReviewService) ListReviewViewedFiles(
	ctx context.Context,
	params ListReviewViewedFilesParams,
) ([]reviewstore.ViewedFile, error) {
	if !params.Author.isHuman() {
		return nil, fmt.Errorf("%w: reading which files have been viewed", ErrReviewHumanOnly)
	}

	task, err := s.getTask(ctx, params.Task, params.Author)
	if err != nil {
		return nil, err
	}

	return s.reviews.ListViewedFiles(ctx, task.ID)
}

// requireOwnThread refuses an id that names no thread, and refuses an agent reaching into a
// review it neither writes the task of nor authored.
func (s *ReviewService) requireOwnThread(ctx context.Context, id uuid.UUID, author ReviewAuthor) error {
	_, err := s.getOwnThread(ctx, id, author)

	return err
}

// getOwnThread reads a thread and says whether the caller has any business in it, which the
// person, the task's writer and the reviewing agent that wrote it all do.
func (s *ReviewService) getOwnThread(ctx context.Context, id uuid.UUID, author ReviewAuthor) (*reviewstore.Thread, error) {
	thread, err := s.reviews.GetThread(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %q", ErrReviewThreadNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	if author.isHuman() || doesOwnReview(author, *thread) {
		return thread, nil
	}

	writer, err := s.isTaskWriter(ctx, author, thread.TaskID)
	if err != nil {
		return nil, err
	}
	if writer {
		return thread, nil
	}

	return nil, fmt.Errorf("%w: %q", ErrReviewNotYours, id)
}

// getOwnMessage reads a message, refusing a caller rewriting or removing words it did not write.
func (s *ReviewService) getOwnMessage(ctx context.Context, id uuid.UUID, author ReviewAuthor) (*reviewstore.Message, error) {
	message, err := s.reviews.GetMessage(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %q", ErrReviewMessageNotFound, id)
	}
	if err != nil {
		return nil, err
	}

	if author.isHuman() {
		if message.AuthorKind != agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN {
			return nil, fmt.Errorf("%w: %q was written by an agent", ErrReviewNotYours, id)
		}

		return message, nil
	}

	if message.AuthorKind != agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT ||
		message.AuthorAgentID == nil ||
		!doesOwnAgent(author, *message.AuthorAgentID) {
		return nil, fmt.Errorf("%w: %q", ErrReviewNotYours, id)
	}

	return message, nil
}

func doesOwnAgent(author ReviewAuthor, agentID uuid.UUID) bool {
	return author.AgentID != nil && *author.AgentID == agentID
}

// isThreadHeld reports whether a thread opens with a held forge comment that no copy has
// accepted.
func (s *ReviewService) isThreadHeld(ctx context.Context, threadID uuid.UUID) (bool, error) {
	messages, err := s.reviews.ListMessages(ctx, []uuid.UUID{threadID})
	if err != nil {
		return false, err
	}
	if len(messages) == 0 {
		return false, nil
	}

	root := rootOf(messages[0], messages)
	if !root.Held {
		return false, nil
	}

	for _, message := range messages {
		if message.AcceptedFrom != nil && *message.AcceptedFrom == root.ID {
			return false, nil
		}
	}

	return true, nil
}
