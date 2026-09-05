package agents

import (
	"context"
	"errors"
	"fmt"
	"time"

	"weave/internal/store"
	mailstore "weave/internal/store/mail"
	reviewstore "weave/internal/store/review"
)

// interruptSettleFor is how long the agent is given to act on the interrupt keys before the
// mail lands behind them, so the notification is typed into a composer rather than into
// whatever was still on screen.
const interruptSettleFor = 200 * time.Millisecond

// ErrReviewThreadNotFound is answered when a thread id names nothing.
var ErrReviewThreadNotFound = errors.New("review thread not found")

// ErrReviewMessageNotFound is answered when a message id names nothing.
var ErrReviewMessageNotFound = errors.New("review message not found")

// ErrReviewNotYours is answered when a caller writes to or claims another agent's review, which
// it may still read.
var ErrReviewNotYours = errors.New("that review belongs to another agent")

// ErrReviewHumanOnly is answered when an agent asks for something only the person reviewing
// does, such as opening, deleting, or releasing a review.
var ErrReviewHumanOnly = errors.New("only the person reviewing can do that")

// reviewThreadReadWriter is the half of the store that keeps conversations: where they are
// pinned, whether the person has read the replies, and whether they are still owed an answer.
type reviewThreadReadWriter interface {
	CreateThread(ctx context.Context, params reviewstore.CreateThreadParams) (*reviewstore.Thread, error)
	ListThreads(ctx context.Context, agentID string) ([]reviewstore.Thread, error)
	GetThread(ctx context.Context, id string) (*reviewstore.Thread, error)
	MarkThreadRead(ctx context.Context, id string) (*reviewstore.Thread, error)
	SetThreadResolved(ctx context.Context, id string, resolved bool) (*reviewstore.Thread, error)
	DeleteThread(ctx context.Context, id string) error
}

// reviewMessageReadWriter is the other half: what was said in those conversations, and how far
// each of the person's comments has got towards the agent reading it.
type reviewMessageReadWriter interface {
	AddMessage(ctx context.Context, params reviewstore.AddMessageParams) (*reviewstore.Message, error)
	EditMessage(ctx context.Context, id, body string, suggestion []string) (*reviewstore.Message, error)
	GetMessage(ctx context.Context, id string) (*reviewstore.Message, error)
	ListMessages(ctx context.Context, threadIDs []string) ([]reviewstore.Message, error)
	DeleteMessage(ctx context.Context, id string) error
}

// reviewViewedFileReadWriter is the record of which files the person reviewing has read, kept
// apart from the conversations since it is about their own progress rather than about anything
// said to the agent.
type reviewViewedFileReadWriter interface {
	MarkFileViewed(ctx context.Context, params reviewstore.MarkFileViewedParams) (*reviewstore.ViewedFile, error)
	UnmarkFileViewed(ctx context.Context, roundID, filePath string) error
	ListViewedFiles(ctx context.Context, roundID string) ([]reviewstore.ViewedFile, error)
}

type reviewNoticeSender interface {
	SendNotice(ctx context.Context, params SendNoticeParams) (*mailstore.Mail, error)
}

type agentInterrupter interface {
	Interrupt(ctx context.Context, agentID string) error
}

// ReviewAuthor is who a call is, decided from the calling identity rather than from anything
// the caller asks for: the panel names no agent and acts as the person, an agent names itself
// and acts as itself.
type ReviewAuthor struct {
	Kind    reviewstore.AuthorKind
	AgentID *string
}

// IsHuman reports whether the call came from the person reviewing rather than from an agent.
func (a ReviewAuthor) IsHuman() bool { return a.Kind != reviewstore.AuthorAgent }

// ReviewThreadWithMessages is a thread and everything said in it.
type ReviewThreadWithMessages struct {
	Thread   reviewstore.Thread
	Messages []reviewstore.Message
}

type ReviewServiceParams struct {
	Threads     reviewThreadReadWriter
	Messages    reviewMessageReadWriter
	Viewed      reviewViewedFileReadWriter
	Rounds      reviewRoundReadWriter
	Reviews     reviewBatchReadWriter
	Snapshots   reviewSnapshotter
	Workflows   workflowGetter
	Agents      agentGetter
	Mailer      reviewNoticeSender
	Interrupter agentInterrupter
	// StateDir is where weave keeps its own files, which is where a released review is written.
	StateDir string
}

// ReviewService owns the conversations pinned to the lines of an agent's diff and holds the only
// copy, since mail says feedback exists without carrying any of it.
type ReviewService struct {
	threads     reviewThreadReadWriter
	messages    reviewMessageReadWriter
	viewed      reviewViewedFileReadWriter
	rounds      reviewRoundReadWriter
	reviews     reviewBatchReadWriter
	snapshots   reviewSnapshotter
	workflows   workflowGetter
	agents      agentGetter
	mailer      reviewNoticeSender
	interrupter agentInterrupter
	stateDir    string
}

// NewReviewService builds the service over the review store and the things it tells.
func NewReviewService(params ReviewServiceParams) *ReviewService {
	return &ReviewService{
		threads:     params.Threads,
		messages:    params.Messages,
		viewed:      params.Viewed,
		rounds:      params.Rounds,
		reviews:     params.Reviews,
		snapshots:   params.Snapshots,
		workflows:   params.Workflows,
		agents:      params.Agents,
		mailer:      params.Mailer,
		interrupter: params.Interrupter,
		stateDir:    params.StateDir,
	}
}

type ListReviewThreadsParams struct {
	AgentID string
	Author  ReviewAuthor
}

// ListReviewThreads is every thread on an agent's diff with its messages, all of it for the
// person reviewing and only what they have let go of for an agent, so that a review is read
// once it is a review rather than over their shoulder as it is typed.
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

	ids := make([]string, 0, len(threads))
	for _, thread := range threads {
		ids = append(ids, thread.ID)
	}

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

// ownsReview reports whether an author is reading back a thread it opened itself, which it may
// see whole, unreleased comments included, since they are its own words.
func ownsReview(author ReviewAuthor, thread reviewstore.Thread) bool {
	return author.AgentID != nil &&
		thread.ReviewAuthorAgentID != nil &&
		*thread.ReviewAuthorAgentID == *author.AgentID
}

// releasedMessages is what an agent may be shown of a conversation, which is decided by the batch
// a message belongs to rather than by who wrote it: an answer belongs to none and is always
// shown, a comment in a released batch is shown, and one still being written is shown only to
// the author writing it.
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

// isReadableBy reports whether one message has been let go of, or is the reader's own, which is
// the whole of what decides that a review is read once it is a review.
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

type CreateReviewThreadParams struct {
	AgentID string
	// Suggestion is replacement lines the author proposes for the range, empty for an ordinary
	// comment.
	Suggestion  []string
	FilePath    string
	OldStart    int32
	OldEnd      int32
	NewStart    int32
	NewEnd      int32
	QuotedLines []string
	Body        string
	Author      ReviewAuthor
}

// CreateReviewThread pins a conversation to a place in a round and writes its first message under
// the author's open batch, opening one when they have none, so that writing a comment is what
// starts a review.
func (s *ReviewService) CreateReviewThread(
	ctx context.Context,
	params CreateReviewThreadParams,
) (*ReviewThreadWithMessages, error) {
	open, err := s.openReviewFor(ctx, params.AgentID, params.Author)
	if err != nil {
		return nil, err
	}

	thread, err := s.threads.CreateThread(ctx, reviewstore.CreateThreadParams{
		AgentID:       params.AgentID,
		ReviewID:      open.ID,
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

type AddReviewMessageParams struct {
	ThreadID   string
	Body       string
	Suggestion []string
	Author     ReviewAuthor
}

// AddReviewMessage says something in an existing thread, filed under the batch a reviewer still
// has open and under none at all for the agent answering, whose answer reaches the reviewer as
// soon as it is written.
func (s *ReviewService) AddReviewMessage(
	ctx context.Context,
	params AddReviewMessageParams,
) (*reviewstore.Message, error) {
	thread, err := s.ownThread(ctx, params.ThreadID, params.Author)
	if err != nil {
		return nil, err
	}

	var reviewID *string
	if params.Author.IsHuman() || !doesOwnAgent(params.Author, thread.AgentID) {
		open, err := s.openReviewFor(ctx, thread.AgentID, params.Author)
		if err != nil {
			return nil, err
		}

		reviewID = &open.ID
	}

	return s.messages.AddMessage(ctx, reviewstore.AddMessageParams{
		ThreadID:      params.ThreadID,
		ReviewID:      reviewID,
		Suggestion:    params.Suggestion,
		AuthorKind:    params.Author.Kind,
		AuthorAgentID: params.Author.AgentID,
		Body:          params.Body,
	})
}

type EditReviewMessageParams struct {
	ID         string
	Body       string
	Suggestion []string
	Author     ReviewAuthor
}

// EditReviewMessage rewrites what was said, which only its author may do and which counts as new
// activity since the other side was shown the old text.
func (s *ReviewService) EditReviewMessage(
	ctx context.Context,
	params EditReviewMessageParams,
) (*reviewstore.Message, error) {
	if err := s.requireOwnMessage(ctx, params.ID, params.Author); err != nil {
		return nil, err
	}

	return s.messages.EditMessage(ctx, params.ID, params.Body, params.Suggestion)
}

type MarkReviewThreadReadParams struct {
	ID     string
	Author ReviewAuthor
}

// MarkReviewThreadRead catches the person up on a single conversation, and is theirs alone,
// since what the agent has been told is recorded per comment rather than per thread.
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

type SetReviewThreadResolvedParams struct {
	ID       string
	Resolved bool
	Author   ReviewAuthor
}

// SetReviewThreadResolved settles a thread or reopens it, which only the person reviewing may
// do.
func (s *ReviewService) SetReviewThreadResolved(
	ctx context.Context,
	params SetReviewThreadResolvedParams,
) (*reviewstore.Thread, error) {
	if !params.Author.IsHuman() {
		return nil, fmt.Errorf("%w: settling a review thread", ErrReviewHumanOnly)
	}

	if err := s.requireOwnThread(ctx, params.ID, params.Author); err != nil {
		return nil, err
	}

	return s.threads.SetThreadResolved(ctx, params.ID, params.Resolved)
}

type SetReviewFileViewedParams struct {
	RoundID  string
	FilePath string
	Viewed   bool
	Author   ReviewAuthor
}

// SetReviewFileViewed marks one file of a round read or drops the mark, which only the person
// reviewing may do, since it is a record of what they have looked at.
func (s *ReviewService) SetReviewFileViewed(
	ctx context.Context,
	params SetReviewFileViewedParams,
) (*reviewstore.ViewedFile, error) {
	if !params.Author.IsHuman() {
		return nil, fmt.Errorf("%w: marking a file viewed", ErrReviewHumanOnly)
	}

	if !params.Viewed {
		return nil, s.viewed.UnmarkFileViewed(ctx, params.RoundID, params.FilePath)
	}

	return s.viewed.MarkFileViewed(ctx, reviewstore.MarkFileViewedParams{
		RoundID:  params.RoundID,
		FilePath: params.FilePath,
	})
}

type ListReviewViewedFilesParams struct {
	RoundID string
	Author  ReviewAuthor
}

// ListReviewViewedFiles is every file of a round the person reviewing has marked read,
// which is theirs alone, so an agent is never told which parts of its work have been looked at.
func (s *ReviewService) ListReviewViewedFiles(
	ctx context.Context,
	params ListReviewViewedFilesParams,
) ([]reviewstore.ViewedFile, error) {
	if !params.Author.IsHuman() {
		return nil, fmt.Errorf("%w: reading which files have been viewed", ErrReviewHumanOnly)
	}

	return s.viewed.ListViewedFiles(ctx, params.RoundID)
}

type DeleteReviewThreadParams struct {
	ID     string
	Author ReviewAuthor
}

// DeleteReviewThread removes a thread and everything said in it, which only the person reviewing
// may do.
func (s *ReviewService) DeleteReviewThread(ctx context.Context, params DeleteReviewThreadParams) error {
	if !params.Author.IsHuman() {
		return fmt.Errorf("%w: deleting a review thread", ErrReviewHumanOnly)
	}

	if err := s.requireOwnThread(ctx, params.ID, params.Author); err != nil {
		return err
	}

	return s.threads.DeleteThread(ctx, params.ID)
}

type DeleteReviewMessageParams struct {
	ID     string
	Author ReviewAuthor
}

// DeleteReviewMessage removes one message, leaving the thread it was in, which only its author
// may do.
func (s *ReviewService) DeleteReviewMessage(ctx context.Context, params DeleteReviewMessageParams) error {
	if err := s.requireOwnMessage(ctx, params.ID, params.Author); err != nil {
		return err
	}

	return s.messages.DeleteMessage(ctx, params.ID)
}

// interrupt stops the agent where it stands, and failing to is no reason to withhold the
// notification since the mail still reaches the agent later.
func (s *ReviewService) interrupt(ctx context.Context, agentID string) {
	if s.interrupter == nil {
		return
	}
	if err := s.interrupter.Interrupt(ctx, agentID); err != nil {
		return
	}

	time.Sleep(interruptSettleFor)
}

// requireOwnThread refuses an id that names no thread, and refuses an agent reaching into a
// review it is neither the subject nor the author of.
func (s *ReviewService) requireOwnThread(ctx context.Context, id string, author ReviewAuthor) error {
	_, err := s.ownThread(ctx, id, author)

	return err
}

// ownThread reads a thread and says whether the caller has any business in it: the person
// always has, the agent under review has because the thread is about its work, and a reviewing
// agent has because it wrote the thread.
func (s *ReviewService) ownThread(ctx context.Context, id string, author ReviewAuthor) (*reviewstore.Thread, error) {
	thread, err := s.threads.GetThread(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %q", ErrReviewThreadNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	if author.IsHuman() || doesOwnAgent(author, thread.AgentID) || ownsReview(author, *thread) {
		return thread, nil
	}

	return nil, fmt.Errorf("%w: %q", ErrReviewNotYours, id)
}

// requireOwnMessage refuses a caller rewriting or removing words it did not write.
func (s *ReviewService) requireOwnMessage(ctx context.Context, id string, author ReviewAuthor) error {
	message, err := s.messages.GetMessage(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w: %q", ErrReviewMessageNotFound, id)
	}
	if err != nil {
		return err
	}

	if author.IsHuman() {
		if message.AuthorKind != reviewstore.AuthorHuman {
			return fmt.Errorf("%w: %q was written by an agent", ErrReviewNotYours, id)
		}

		return nil
	}

	if message.AuthorKind != reviewstore.AuthorAgent ||
		message.AuthorAgentID == nil ||
		!doesOwnAgent(author, *message.AuthorAgentID) {
		return fmt.Errorf("%w: %q", ErrReviewNotYours, id)
	}

	return nil
}

func doesOwnAgent(author ReviewAuthor, agentID string) bool {
	return author.AgentID != nil && *author.AgentID == agentID
}
