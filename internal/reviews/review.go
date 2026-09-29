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
	workflowstore "komrad/internal/store/workflow"
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

// ErrReviewWholeChangeAnchored is answered when a comment on the change as a whole names lines,
// quotes them, or suggests replacing them, which only a comment on a file can.
var ErrReviewWholeChangeAnchored = fail.InvalidArgument("a comment on the change as a whole cannot name lines, quote them, or suggest a replacement")

// reviewThreadReadWriter is the half of the store that keeps conversations: where they are
// pinned, whether the person has read the replies, and whether they are still owed an answer.
type reviewThreadReadWriter interface {
	CreateThread(ctx context.Context, params reviewstore.CreateThreadParams) (*reviewstore.Thread, error)
	ListThreads(ctx context.Context, agentID string) ([]reviewstore.Thread, error)
	GetThread(ctx context.Context, id string) (*reviewstore.Thread, error)
	MarkThreadRead(ctx context.Context, id string) (*reviewstore.Thread, error)
	SetThreadResolved(ctx context.Context, params reviewstore.SetThreadResolvedParams) (*reviewstore.Thread, error)
}

// reviewMessageReadWriter is the other half: what was said in those conversations, and how far
// each of the person's comments has got towards the agent reading it.
type reviewMessageReadWriter interface {
	CreateMessage(ctx context.Context, params reviewstore.CreateMessageParams) (*reviewstore.Message, error)
	UpdateMessage(ctx context.Context, id, body string, suggestion []string) (*reviewstore.Message, error)
	GetMessage(ctx context.Context, id string) (*reviewstore.Message, error)
	ListMessages(ctx context.Context, threadIDs []string) ([]reviewstore.Message, error)
}

// reviewViewedFileReadWriter is the record of which files the person reviewing has read.
type reviewViewedFileReadWriter interface {
	MarkFileViewed(ctx context.Context, params reviewstore.MarkFileViewedParams) (*reviewstore.ViewedFile, error)
	UnmarkFileViewed(ctx context.Context, roundID, filePath string) error
	ListViewedFiles(ctx context.Context, roundID string) ([]reviewstore.ViewedFile, error)
}

// agentGetter reads one agent by id.
type agentGetter interface {
	GetByID(ctx context.Context, id string) (*agentstore.Agent, error)
}

// workflowGetter reads the workflow an agent belongs to.
type workflowGetter interface {
	Get(ctx context.Context, id string) (*workflowstore.Workflow, error)
}

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

// IsHuman reports whether the call came from the person reviewing rather than from an agent.
func (a ReviewAuthor) IsHuman() bool {
	return a.Kind != reviewstore.AuthorAgent
}

// ReviewThreadWithMessages is a thread and everything said in it.
type ReviewThreadWithMessages struct {
	Thread   reviewstore.Thread
	Messages []reviewstore.Message
}

// NewReviewServiceParams is everything the review service reads and tells.
type NewReviewServiceParams struct {
	Threads     reviewThreadReadWriter
	Messages    reviewMessageReadWriter
	Viewed      reviewViewedFileReadWriter
	Rounds      reviewRoundReadWriter
	Reviews     reviewBatchReadWriter
	Snapshots   reviewSnapshotter
	Workflows   workflowGetter
	Agents      agentGetter
	Events      eventRecorder
	Interrupter agentInterrupter
	// StateDir is where komrad keeps its own files.
	StateDir string
}

// ReviewService owns the conversations pinned to the lines of an agent's diff, and holds the
// only copy.
type ReviewService struct {
	threads     reviewThreadReadWriter
	messages    reviewMessageReadWriter
	viewed      reviewViewedFileReadWriter
	rounds      reviewRoundReadWriter
	reviews     reviewBatchReadWriter
	snapshots   reviewSnapshotter
	workflows   workflowGetter
	agents      agentGetter
	events      eventRecorder
	interrupter agentInterrupter
	stateDir    string
	roundLocks  *agentLocks
}

// NewReviewService builds the service over the review store and the things it tells.
func NewReviewService(params NewReviewServiceParams) *ReviewService {
	return &ReviewService{
		threads:     params.Threads,
		messages:    params.Messages,
		viewed:      params.Viewed,
		rounds:      params.Rounds,
		reviews:     params.Reviews,
		snapshots:   params.Snapshots,
		workflows:   params.Workflows,
		agents:      params.Agents,
		events:      params.Events,
		interrupter: params.Interrupter,
		stateDir:    params.StateDir,
		roundLocks:  newAgentLocks(),
	}
}

// ListReviewThreadsParams says whose diff to read the conversations of, and as whom.
type ListReviewThreadsParams struct {
	AgentID string
	Author  ReviewAuthor
}

// ListReviewThreads is every thread on an agent's diff with its messages, all of it for the
// person reviewing and only the released part for an agent.
func (s *ReviewService) ListReviewThreads(
	ctx context.Context,
	params ListReviewThreadsParams,
) ([]ReviewThreadWithMessages, error) {
	threads, err := s.threads.ListThreads(ctx, params.AgentID)
	if err != nil {
		return nil, err
	}
	if len(threads) == 0 {
		return nil, nil
	}

	ids := xslice.Map(threads, func(thread reviewstore.Thread) string {
		return thread.ID
	})

	messages, err := s.messages.ListMessages(ctx, ids)
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
		if !params.Author.IsHuman() {
			messages = releasedMessages(messages, params.Author)
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
	if params.FilePath == "" && isAnchoredToLines(params) {
		return nil, ErrReviewWholeChangeAnchored
	}

	if err := s.requireEntitled(ctx, params.AgentID, params.Author); err != nil {
		return nil, err
	}

	round, err := s.roundFor(ctx, params)
	if err != nil {
		return nil, err
	}

	open, err := s.openReviewFor(ctx, openReviewForParams{
		AgentID: params.AgentID,
		Author:  params.Author,
		Round:   round,
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

	thread, err := s.threads.CreateThread(ctx, reviewstore.CreateThreadParams{
		AgentID:       params.AgentID,
		ReviewID:      open.ID,
		RoundID:       round.ID,
		BaseSHA:       baseSHA,
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

	messages, err := s.messages.ListMessages(ctx, []string{thread.ID})
	if err != nil {
		return nil, err
	}

	return &ReviewThreadWithMessages{Thread: *thread, Messages: messages}, nil
}

// carryOpenReview files a person's draft under the newest round they have commented on.
func (s *ReviewService) carryOpenReview(
	ctx context.Context,
	open reviewstore.Review,
	round reviewstore.Round,
	author ReviewAuthor,
) (*reviewstore.Review, error) {
	held, err := s.rounds.GetRound(ctx, open.RoundID)
	if err != nil {
		return nil, err
	}

	if !author.IsHuman() {
		return nil, fmt.Errorf("%w: send the comments you have on round %d first",
			ErrReviewInFlight, held.Ordinal)
	}

	if round.Ordinal <= held.Ordinal {
		return &open, nil
	}

	return s.reviews.MoveOpenReview(ctx, reviewstore.MoveOpenReviewParams{
		ID:      open.ID,
		RoundID: round.ID,
	})
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

// AddReviewMessageResult is the message as written and anything worth telling the one who wrote it.
type AddReviewMessageResult struct {
	Message reviewstore.Message
	// Hint points a plain reply at addressed when the worktree has moved since the thread was written.
	Hint string
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

	if thread.FilePath == "" && len(params.Suggestion) > 0 {
		return nil, ErrReviewWholeChangeAnchored
	}

	answering := !params.Author.IsHuman() && doesOwnAgent(params.Author, thread.AgentID)
	if params.Addressed && !answering {
		return nil, ErrReviewAddressedByOwner
	}

	var reviewID *string
	if !answering {
		open, err := s.openReviewFor(ctx, openReviewForParams{
			AgentID: thread.AgentID,
			Author:  params.Author,
		})
		if err != nil {
			return nil, err
		}

		reviewID = &open.ID
	}

	var roundID *string
	if params.Addressed {
		answer, err := s.openAnswerRound(ctx, *thread, params.Author)
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

		event, err := events.NewEvent(Answered{AgentID: thread.AgentID})
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
	message, err := s.getOwnMessage(ctx, params.ID, params.Author)
	if err != nil {
		return nil, err
	}

	if len(params.Suggestion) > 0 {
		thread, err := s.threads.GetThread(ctx, message.ThreadID)
		if err != nil {
			return nil, err
		}

		if thread.FilePath == "" {
			return nil, ErrReviewWholeChangeAnchored
		}
	}

	return s.messages.UpdateMessage(ctx, params.ID, params.Body, params.Suggestion)
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
	if !params.Author.IsHuman() {
		return nil, fmt.Errorf("%w: marking a review thread read", ErrReviewHumanOnly)
	}

	if err := s.requireOwnThread(ctx, params.ID, params.Author); err != nil {
		return nil, err
	}

	return s.threads.MarkThreadRead(ctx, params.ID)
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

	if !params.Author.IsHuman() && !doesOwnReview(params.Author, *thread) {
		return nil, ErrReviewNotYours.Saying(
			"%q is a comment about your own work, so it is not yours to settle", params.ID)
	}

	return s.threads.SetThreadResolved(ctx, reviewstore.SetThreadResolvedParams{
		ID:        params.ID,
		Resolved:  params.Resolved,
		ByKind:    params.Author.Kind,
		ByAgentID: params.Author.AgentID,
	})
}

// SetReviewFileViewedParams names one file of an agent's diff to mark read or unread.
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
	if !params.Author.IsHuman() {
		return nil, fmt.Errorf("%w: marking a file viewed", ErrReviewHumanOnly)
	}

	if !params.Viewed {
		return nil, s.viewed.UnmarkFileViewed(ctx, params.AgentID, params.FilePath)
	}

	return s.viewed.MarkFileViewed(ctx, reviewstore.MarkFileViewedParams{
		AgentID:     params.AgentID,
		FilePath:    params.FilePath,
		Fingerprint: params.Fingerprint,
	})
}

// ListReviewViewedFilesParams names the agent whose read files are wanted.
type ListReviewViewedFilesParams struct {
	AgentID string
	Author  ReviewAuthor
}

// ListReviewViewedFiles is every file of an agent the person reviewing has marked read.
func (s *ReviewService) ListReviewViewedFiles(
	ctx context.Context,
	params ListReviewViewedFilesParams,
) ([]reviewstore.ViewedFile, error) {
	if !params.Author.IsHuman() {
		return nil, fmt.Errorf("%w: reading which files have been viewed", ErrReviewHumanOnly)
	}

	return s.viewed.ListViewedFiles(ctx, params.AgentID)
}

// interrupt stops the agent where it stands, and failing to is no reason to withhold the
// notification.
func (s *ReviewService) interrupt(ctx context.Context, agentID string) {
	if err := s.interrupter.Interrupt(ctx, agentID); err != nil {
		return
	}

	time.Sleep(interruptSettleFor)
}

// requireOwnThread refuses an id that names no thread, and refuses an agent reaching into a
// review it is neither the subject nor the author of.
func (s *ReviewService) requireOwnThread(ctx context.Context, id string, author ReviewAuthor) error {
	_, err := s.getOwnThread(ctx, id, author)

	return err
}

// getOwnThread reads a thread and says whether the caller has any business in it, which the
// person, the agent under review and the reviewing agent that wrote it all do.
func (s *ReviewService) getOwnThread(ctx context.Context, id string, author ReviewAuthor) (*reviewstore.Thread, error) {
	thread, err := s.threads.GetThread(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %q", ErrReviewThreadNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	if author.IsHuman() || doesOwnAgent(author, thread.AgentID) || doesOwnReview(author, *thread) {
		return thread, nil
	}

	return nil, fmt.Errorf("%w: %q", ErrReviewNotYours, id)
}

// getOwnMessage reads a message, refusing a caller rewriting or removing words it did not write.
func (s *ReviewService) getOwnMessage(ctx context.Context, id string, author ReviewAuthor) (*reviewstore.Message, error) {
	message, err := s.messages.GetMessage(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %q", ErrReviewMessageNotFound, id)
	}
	if err != nil {
		return nil, err
	}

	if author.IsHuman() {
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

// isAnchoredToLines reports whether a new thread names, quotes or rewrites any line.
func isAnchoredToLines(params CreateReviewThreadParams) bool {
	return params.OldStart != 0 || params.OldEnd != 0 || params.NewStart != 0 || params.NewEnd != 0 ||
		len(params.QuotedLines) > 0 || len(params.Suggestion) > 0
}
