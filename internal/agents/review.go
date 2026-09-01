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
// does, such as opening, deleting, or announcing a review.
var ErrReviewHumanOnly = errors.New("only the person reviewing can do that")

// reviewThreadReadWriter is the half of the store that keeps conversations: where they are
// pinned, who has read them, and whether they are still owed an answer.
type reviewThreadReadWriter interface {
	CreateThread(ctx context.Context, params reviewstore.CreateThreadParams) (*reviewstore.Thread, error)
	ListThreads(ctx context.Context, params reviewstore.ListThreadsParams) ([]reviewstore.Thread, error)
	GetThread(ctx context.Context, id string) (*reviewstore.Thread, error)
	MarkThreadRead(ctx context.Context, id string, reader reviewstore.AuthorKind) (*reviewstore.Thread, error)
	SetThreadResolved(ctx context.Context, id string, resolved bool) (*reviewstore.Thread, error)
	CountUnreadForAgent(ctx context.Context, agentID string) (int64, error)
	DeleteThread(ctx context.Context, id string) error
}

// reviewMessageReadWriter is the other half: what was said in those conversations.
type reviewMessageReadWriter interface {
	AddMessage(ctx context.Context, params reviewstore.AddMessageParams) (*reviewstore.Message, error)
	EditMessage(ctx context.Context, id, body string) (*reviewstore.Message, error)
	GetMessage(ctx context.Context, id string) (*reviewstore.Message, error)
	ListMessages(ctx context.Context, threadIDs []string) ([]reviewstore.Message, error)
	DeleteMessage(ctx context.Context, id string) error
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
	Mailer      reviewNoticeSender
	Interrupter agentInterrupter
}

// ReviewService owns the conversations pinned to the lines of an agent's diff and holds the only
// copy, since mail says feedback exists without carrying any of it.
type ReviewService struct {
	threads     reviewThreadReadWriter
	messages    reviewMessageReadWriter
	mailer      reviewNoticeSender
	interrupter agentInterrupter
}

// NewReviewService builds the service over the review store and the things it tells.
func NewReviewService(params ReviewServiceParams) *ReviewService {
	return &ReviewService{
		threads:     params.Threads,
		messages:    params.Messages,
		mailer:      params.Mailer,
		interrupter: params.Interrupter,
	}
}

type ListReviewThreadsParams struct {
	AgentID string
	// MarkRead claims what is returned, which an agent reading in order to act wants and the
	// panel drawing a diff does not.
	MarkRead bool
	Author   ReviewAuthor
}

// ListReviewThreads is every thread on an agent's diff with its messages.
func (s *ReviewService) ListReviewThreads(
	ctx context.Context,
	params ListReviewThreadsParams,
) ([]ReviewThreadWithMessages, error) {
	// Anyone may read anyone's review, but claiming another agent's would move a watermark on
	// feedback that agent has never been shown.
	if params.MarkRead && !params.Author.IsHuman() && !doesOwnAgent(params.Author, params.AgentID) {
		return nil, fmt.Errorf("%w: %q", ErrReviewNotYours, params.AgentID)
	}

	threads, err := s.threads.ListThreads(ctx, reviewstore.ListThreadsParams{
		AgentID:  params.AgentID,
		Reader:   params.Author.Kind,
		MarkRead: params.MarkRead,
	})
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
		answered = append(answered, ReviewThreadWithMessages{Thread: thread, Messages: byThread[thread.ID]})
	}

	return answered, nil
}

type CreateReviewThreadParams struct {
	AgentID     string
	FilePath    string
	OldStart    int32
	OldEnd      int32
	NewStart    int32
	NewEnd      int32
	QuotedLines []string
	Body        string
	Author      ReviewAuthor
}

// CreateReviewThread pins a conversation to a place in a diff and writes its first message,
// which only the person reviewing may do.
func (s *ReviewService) CreateReviewThread(
	ctx context.Context,
	params CreateReviewThreadParams,
) (*ReviewThreadWithMessages, error) {
	if !params.Author.IsHuman() {
		return nil, fmt.Errorf("%w: opening a review thread", ErrReviewHumanOnly)
	}

	thread, err := s.threads.CreateThread(ctx, reviewstore.CreateThreadParams{
		AgentID:       params.AgentID,
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
	ThreadID string
	Body     string
	Author   ReviewAuthor
}

// AddReviewMessage says something in an existing thread.
func (s *ReviewService) AddReviewMessage(
	ctx context.Context,
	params AddReviewMessageParams,
) (*reviewstore.Message, error) {
	if err := s.requireOwnThread(ctx, params.ThreadID, params.Author); err != nil {
		return nil, err
	}

	return s.messages.AddMessage(ctx, reviewstore.AddMessageParams{
		ThreadID:      params.ThreadID,
		AuthorKind:    params.Author.Kind,
		AuthorAgentID: params.Author.AgentID,
		Body:          params.Body,
	})
}

type EditReviewMessageParams struct {
	ID     string
	Body   string
	Author ReviewAuthor
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

	return s.messages.EditMessage(ctx, params.ID, params.Body)
}

type MarkReviewThreadReadParams struct {
	ID     string
	Author ReviewAuthor
}

// MarkReviewThreadRead catches one side up on a single conversation, since claiming the whole
// list would say the person had read threads they never opened.
func (s *ReviewService) MarkReviewThreadRead(
	ctx context.Context,
	params MarkReviewThreadReadParams,
) (*reviewstore.Thread, error) {
	if err := s.requireOwnThread(ctx, params.ID, params.Author); err != nil {
		return nil, err
	}

	return s.threads.MarkThreadRead(ctx, params.ID, params.Author.Kind)
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

// NotifyReviewThreadsResult reports what telling an agent about its review threads did.
type NotifyReviewThreadsResult struct {
	// Unread is how many open threads hold activity the agent has not read.
	Unread int64
	// AlreadyWaiting says the agent already holds an unread notice of this kind, so none was
	// raised, and one that ran out of time before it was read does not count.
	AlreadyWaiting bool
}

type NotifyReviewThreadsParams struct {
	AgentID string
	// Interrupt stops the agent where it stands before the mail is sent, so it reads the
	// review now rather than at the end of whatever it is doing.
	Interrupt bool
	Author    ReviewAuthor
}

// NotifyReviewThreads tells an agent it has review activity waiting, as one piece of mail that
// carries a count and the name of the tool to fetch with rather than the feedback itself.
func (s *ReviewService) NotifyReviewThreads(
	ctx context.Context,
	params NotifyReviewThreadsParams,
) (*NotifyReviewThreadsResult, error) {
	if !params.Author.IsHuman() {
		return nil, fmt.Errorf("%w: announcing a review", ErrReviewHumanOnly)
	}

	unread, err := s.threads.CountUnreadForAgent(ctx, params.AgentID)
	if err != nil {
		return nil, err
	}
	if unread == 0 {
		return &NotifyReviewThreadsResult{}, nil
	}

	if params.Interrupt {
		s.interrupt(ctx, params.AgentID)
	}

	body := fmt.Sprintf(
		"weave: %d review thread(s) on your diff have activity you have not read, call list_review_threads",
		unread)

	_, err = s.mailer.SendNotice(ctx, SendNoticeParams{
		ToAgentID: params.AgentID,
		Body:      body,
		Kind:      mailstore.KindReviewActivity,
	})
	if err != nil && !errors.Is(err, mailstore.ErrNoticePending) {
		return nil, err
	}

	return &NotifyReviewThreadsResult{Unread: unread, AlreadyWaiting: err != nil}, nil
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
// review addressed to a different agent.
func (s *ReviewService) requireOwnThread(ctx context.Context, id string, author ReviewAuthor) error {
	thread, err := s.threads.GetThread(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w: %q", ErrReviewThreadNotFound, id)
	}
	if err != nil {
		return err
	}
	if !author.IsHuman() && !doesOwnAgent(author, thread.AgentID) {
		return fmt.Errorf("%w: %q", ErrReviewNotYours, id)
	}

	return nil
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
