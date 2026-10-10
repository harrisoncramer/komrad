package reviews

import (
	"context"
	"errors"
	"fmt"
	"time"

	"komrad/internal/events"
	"komrad/internal/fail"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	reviewstore "komrad/internal/store/review"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
	"komrad/lib/clock"
	"komrad/lib/xptr"
	"komrad/lib/xslice"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

// interruptSettleFor is how long the agent is given to act on the interrupt keys before the
// mail lands behind them.
const interruptSettleFor = 200 * time.Millisecond

// ErrReviewThreadNotFound is answered when a thread id names nothing.
var ErrReviewThreadNotFound = fail.NotFound("review thread not found")

// ErrReviewMessageNotFound is answered when a message id names nothing.
var ErrReviewMessageNotFound = fail.NotFound("review message not found")

// ErrReviewNotYours is answered when a caller writes to or claims another agent's review, which
// it may still read.
var ErrReviewNotYours = fail.Denied("that review belongs to another agent")

// ErrReviewHumanOnly is answered when an agent asks for something only the person reviewing
// does, such as opening, deleting, or releasing a review.
var ErrReviewHumanOnly = fail.Denied("only the person reviewing can do that")

// errReviewThreadNoFile is answered when a comment names no file.
var errReviewThreadNoFile = fail.InvalidArgument("a comment must be on a file")

// errReviewThreadHeld is answered when a reply is written in a forge thread whose first comment is
// still held and not yet accepted, which the agent could not read the reply alongside.
var errReviewThreadHeld = fail.Precondition("this thread opens with a held forge comment, so accept it before replying")

type eventRecorder interface {
	InsertManyTx(ctx context.Context, build func(tx pgx.Tx) ([]river.JobArgs, error)) error
}

type agentInterrupter interface {
	Interrupt(ctx context.Context, agentID string) error
}

// ReviewAuthor is who a call is, taken from the calling identity: the panel names no agent and
// acts as the person, and an agent names itself.
type ReviewAuthor struct {
	Kind    reviewstore.AuthorKind
	AgentID *string
}

// isHuman reports whether the call came from the person reviewing rather than from an agent.
func (a ReviewAuthor) isHuman() bool {
	return a.Kind == reviewstore.AuthorHuman
}

// ReviewThreadWithMessages is a thread and everything said in it.
type ReviewThreadWithMessages struct {
	Thread   reviewstore.Thread
	Messages []reviewstore.Message
}

// NewReviewServiceParams is everything the review service reads and tells.
type NewReviewServiceParams struct {
	Reviews     *reviewstore.ReviewStore
	Snapshots   reviewSnapshotter
	Workflows   *workflowstore.WorkflowStore
	Agents      *agentstore.AgentStore
	Tasks       *taskstore.TaskStore
	Events      eventRecorder
	Interrupter agentInterrupter
	// StateDir is where komrad keeps its own files.
	StateDir string
}

// ReviewService owns the conversations pinned to the lines of a task's diff, and holds the only
// copy.
type ReviewService struct {
	reviews     *reviewstore.ReviewStore
	snapshots   reviewSnapshotter
	workflows   *workflowstore.WorkflowStore
	agents      *agentstore.AgentStore
	tasks       *taskstore.TaskStore
	events      eventRecorder
	interrupter agentInterrupter
	stateDir    string
	roundLocks  *taskLocks
}

// NewReviewService builds the service over the review store and the things it tells.
func NewReviewService(params NewReviewServiceParams) *ReviewService {
	return &ReviewService{
		reviews:     params.Reviews,
		snapshots:   params.Snapshots,
		workflows:   params.Workflows,
		agents:      params.Agents,
		tasks:       params.Tasks,
		events:      params.Events,
		interrupter: params.Interrupter,
		stateDir:    params.StateDir,
		roundLocks:  newTaskLocks(),
	}
}

// ListReviewThreadsParams says whose diff to read the conversations of, and as whom.
type ListReviewThreadsParams struct {
	AgentID string
	Author  ReviewAuthor
}

// ListReviewThreads is every thread on the task of an agent with its messages, all of it for the
// person reviewing and only the released part for an agent.
func (s *ReviewService) ListReviewThreads(
	ctx context.Context,
	params ListReviewThreadsParams,
) ([]ReviewThreadWithMessages, error) {
	task, err := s.taskOf(ctx, params.AgentID)
	if err != nil {
		return nil, err
	}

	return s.listThreads(ctx, task.ID, params.Author)
}

// listThreads is every thread on a task's diff with what the author may read of its messages.
func (s *ReviewService) listThreads(
	ctx context.Context,
	taskID string,
	author ReviewAuthor,
) ([]ReviewThreadWithMessages, error) {
	threads, err := s.reviews.ListThreads(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if len(threads) == 0 {
		return nil, nil
	}

	ids := xslice.Map(threads, func(thread reviewstore.Thread) string {
		return thread.ID
	})

	messages, err := s.reviews.ListMessages(ctx, ids)
	if err != nil {
		return nil, err
	}

	byThread := map[string][]reviewstore.Message{}
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
		thread.ReviewAuthorAgentID != nil &&
		*thread.ReviewAuthorAgentID == *author.AgentID
}

// releasedMessages is what an agent may be shown of a conversation, decided by the review a
// message belongs to rather than by who wrote it.
func releasedMessages(messages []reviewstore.Message, author ReviewAuthor) []reviewstore.Message {
	released := make([]reviewstore.Message, 0, len(messages))
	for _, message := range messages {
		if !isReadableBy(message, author) {
			continue
		}

		message.SentAt = nil
		message.DeliveredAt = nil
		message.Delivery = reviewstore.DeliveryNone

		released = append(released, message)
	}

	return released
}

// isReadableBy reports whether one message has been released, or is the reader's own.
func isReadableBy(message reviewstore.Message, author ReviewAuthor) bool {
	switch {
	case message.Delivery == reviewstore.DeliveryHeld:
		return false
	case message.ReviewID == nil:
		return true
	case message.SentAt != nil:
		return true
	default:
		return message.AuthorAgentID != nil &&
			author.AgentID != nil &&
			*message.AuthorAgentID == *author.AgentID
	}
}

// CreateReviewThreadParams is the place a new conversation is pinned to and what it says.
type CreateReviewThreadParams struct {
	AgentID string
	// RoundID is the round the anchor's line numbers were measured in, empty for a caller that
	// leaves the round to be worked out from the snapshot it names.
	RoundID string
	// Suggestion is replacement lines the reviewer proposes for the range, empty on an ordinary
	// comment.
	Suggestion []string
	// BaseSHA is the commit the old side of these line numbers was read against, empty for the
	// round's own base.
	BaseSHA string
	// SnapshotSHA is the commit the new side of these line numbers was read against, empty for a
	// caller that read the round its own review sits on.
	SnapshotSHA string
	FilePath    string
	OldStart    int32
	OldEnd      int32
	NewStart    int32
	NewEnd      int32
	QuotedLines []string
	Body        string
	Author      ReviewAuthor
}

// CreateReviewThread pins a conversation to a place in a round and writes its first message
// under the author's open review, opening one when they have none.
func (s *ReviewService) CreateReviewThread(
	ctx context.Context,
	params CreateReviewThreadParams,
) (*ReviewThreadWithMessages, error) {
	if params.FilePath == "" {
		return nil, errReviewThreadNoFile
	}

	task, err := s.taskOf(ctx, params.AgentID)
	if err != nil {
		return nil, err
	}

	place, err := s.placeThread(ctx, *task, params)
	if err != nil {
		return nil, err
	}

	thread, err := s.reviews.CreateThread(ctx, reviewstore.CreateThreadParams{
		TaskID:        task.ID,
		ReviewID:      place.ReviewID,
		RoundID:       place.RoundID,
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
	if err != nil {
		return nil, err
	}

	messages, err := s.reviews.ListMessages(ctx, []string{thread.ID})
	if err != nil {
		return nil, err
	}

	return &ReviewThreadWithMessages{Thread: *thread, Messages: messages}, nil
}

// threadPlace is the review, round and base a new thread is filed under.
type threadPlace struct {
	ReviewID string
	RoundID  string
	BaseSHA  string
}

// placeThread finds the round a new thread's lines were measured in and the author's open review
// on it, opening or carrying one forward when needed.
func (s *ReviewService) placeThread(
	ctx context.Context,
	task taskstore.Task,
	params CreateReviewThreadParams,
) (*threadPlace, error) {
	if err := s.requireEntitled(ctx, task, params.Author); err != nil {
		return nil, err
	}

	round, err := s.roundFor(ctx, task.ID, params)
	if err != nil {
		return nil, err
	}

	open, err := s.openReviewFor(ctx, openReviewForParams{
		TaskID: task.ID,
		Author: params.Author,
		Round:  round,
	})
	if err != nil {
		return nil, err
	}

	if open.RoundID != round.ID {
		open, err = s.carryOpenReview(ctx, *open, *round, params.Author)
		if err != nil {
			return nil, err
		}
	}

	baseSHA := params.BaseSHA
	if baseSHA == "" {
		baseSHA = round.BaseSHA
	}

	return &threadPlace{
		ReviewID: open.ID,
		RoundID:  round.ID,
		BaseSHA:  baseSHA,
	}, nil
}

// carryOpenReview files a person's draft under the newest round they have commented on.
func (s *ReviewService) carryOpenReview(
	ctx context.Context,
	open reviewstore.Review,
	round reviewstore.Round,
	author ReviewAuthor,
) (*reviewstore.Review, error) {
	held, err := s.reviews.GetRound(ctx, open.RoundID)
	if err != nil {
		return nil, err
	}

	if !author.isHuman() {
		return nil, fmt.Errorf("%w: send the comments you have on round %d first",
			errReviewInFlight, held.Ordinal)
	}

	if round.Ordinal <= held.Ordinal {
		return &open, nil
	}

	return s.reviews.MoveOpenReview(ctx, reviewstore.MoveOpenReviewParams{
		ID:      open.ID,
		RoundID: round.ID,
	})
}

// AddReviewMessageResult is the message as written and anything worth telling the one who wrote it.
type AddReviewMessageResult struct {
	Message reviewstore.Message
	// Hint points a plain reply at addressed when the worktree has moved since the thread was written.
	Hint string
}

// AddReviewMessageParams is what to say in an existing thread, and as whom.
type AddReviewMessageParams struct {
	ThreadID   string
	Body       string
	Suggestion []string
	Author     ReviewAuthor
	// Addressed says the agent under review made the change the thread asks for.
	Addressed bool
}

// AddReviewMessage says something in an existing thread, filed under the review a reviewer
// still has open and under none at all for the agent answering.
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

	var reviewID *string
	if !answering {
		open, err := s.openReviewFor(ctx, openReviewForParams{
			TaskID: thread.TaskID,
			Author: params.Author,
		})
		if err != nil {
			return nil, err
		}

		reviewID = &open.ID
	}

	var roundID *string
	if params.Addressed {
		answer, err := s.openAnswerRound(ctx, *thread, xptr.Deref(params.Author.AgentID))
		if err != nil {
			return nil, err
		}

		roundID = &answer.ID
	}

	var message *reviewstore.Message
	err = s.events.InsertManyTx(ctx, func(tx pgx.Tx) ([]river.JobArgs, error) {
		var err error
		message, err = reviewstore.NewReviewStore(tx).CreateMessage(ctx, reviewstore.CreateMessageParams{
			ThreadID:      params.ThreadID,
			ReviewID:      reviewID,
			RoundID:       roundID,
			Suggestion:    params.Suggestion,
			AuthorKind:    params.Author.Kind,
			AuthorAgentID: params.Author.AgentID,
			Body:          params.Body,
		})
		if err != nil {
			return nil, err
		}
		if !answering {
			return nil, nil
		}

		event, err := events.NewEvent(answered{AgentID: *params.Author.AgentID})
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

// answerMail is the mail telling a reviewer its review was answered.
func answerMail(authorName string) string {
	return fmt.Sprintf("komrad: %s answered your review. Read the threads with list_review_threads, "+
		"open a round over its work as it now stands, and release a verdict on that round.", authorName)
}

// EditReviewMessageParams is the message to rewrite and what it should say instead.
type EditReviewMessageParams struct {
	ID         string
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
	ID     string
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
	ID       string
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

// SetReviewFileViewedParams names one file of the diff of an agent's task to mark read or unread.
type SetReviewFileViewedParams struct {
	AgentID     string
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

	task, err := s.taskOf(ctx, params.AgentID)
	if err != nil {
		return nil, err
	}

	if !params.Viewed {
		return nil, s.reviews.UnmarkFileViewed(ctx, task.ID, params.FilePath)
	}

	return s.reviews.MarkFileViewed(ctx, reviewstore.MarkFileViewedParams{
		AgentID:     params.AgentID,
		TaskID:      task.ID,
		FilePath:    params.FilePath,
		Fingerprint: params.Fingerprint,
	})
}

// ListReviewViewedFilesParams names the agent whose task's read files are wanted.
type ListReviewViewedFilesParams struct {
	AgentID string
	Author  ReviewAuthor
}

// ListReviewViewedFiles is every file of an agent's task the person reviewing has marked read.
func (s *ReviewService) ListReviewViewedFiles(
	ctx context.Context,
	params ListReviewViewedFilesParams,
) ([]reviewstore.ViewedFile, error) {
	if !params.Author.isHuman() {
		return nil, fmt.Errorf("%w: reading which files have been viewed", ErrReviewHumanOnly)
	}

	task, err := s.taskOf(ctx, params.AgentID)
	if err != nil {
		return nil, err
	}

	return s.reviews.ListViewedFiles(ctx, task.ID)
}

// interrupt stops the agent where it stands, and failing to is no reason to withhold the
// notification.
func (s *ReviewService) interrupt(ctx context.Context, agentID string) {
	if err := s.interrupter.Interrupt(ctx, agentID); err != nil {
		return
	}

	clock.Sleep(ctx, interruptSettleFor)
}

// requireOwnThread refuses an id that names no thread, and refuses an agent reaching into a
// review it is neither the subject nor the author of.
func (s *ReviewService) requireOwnThread(ctx context.Context, id string, author ReviewAuthor) error {
	_, err := s.getOwnThread(ctx, id, author)

	return err
}

// getOwnThread reads a thread and says whether the caller has any business in it, which the
// person, the task's writer and the reviewing agent that wrote it all do.
func (s *ReviewService) getOwnThread(ctx context.Context, id string, author ReviewAuthor) (*reviewstore.Thread, error) {
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
func (s *ReviewService) getOwnMessage(ctx context.Context, id string, author ReviewAuthor) (*reviewstore.Message, error) {
	message, err := s.reviews.GetMessage(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %q", ErrReviewMessageNotFound, id)
	}
	if err != nil {
		return nil, err
	}

	if author.isHuman() {
		if message.AuthorKind != reviewstore.AuthorHuman {
			return nil, fmt.Errorf("%w: %q was written by an agent", ErrReviewNotYours, id)
		}

		return message, nil
	}

	if message.AuthorKind != reviewstore.AuthorAgent ||
		message.AuthorAgentID == nil ||
		!doesOwnAgent(author, *message.AuthorAgentID) {
		return nil, fmt.Errorf("%w: %q", ErrReviewNotYours, id)
	}

	return message, nil
}

func doesOwnAgent(author ReviewAuthor, agentID string) bool {
	return author.AgentID != nil && *author.AgentID == agentID
}

// isThreadHeld reports whether a thread opens with a held forge comment that no copy, sent or
// unsent, has accepted.
func (s *ReviewService) isThreadHeld(ctx context.Context, threadID string) (bool, error) {
	messages, err := s.reviews.ListMessages(ctx, []string{threadID})
	if err != nil {
		return false, err
	}
	if len(messages) == 0 {
		return false, nil
	}

	root := messages[0]
	for _, message := range messages {
		if message.ID < root.ID {
			root = message
		}
	}
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
