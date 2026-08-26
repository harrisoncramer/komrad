package agents

import (
	"context"
	"errors"
	"fmt"
	"time"

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

// ErrReviewNotYours is answered when a caller reaches for a review that is not its own.
// Reading another agent's review is allowed; writing to it or claiming it is not, since the
// agent the feedback is addressed to is the one it is about.
var ErrReviewNotYours = errors.New("that review belongs to another agent")

// ErrReviewHumanOnly is answered when an agent asks for something only the person reviewing
// does. An agent replies to feedback on its own work; it does not open, delete, or announce
// a review.
var ErrReviewHumanOnly = errors.New("only the person reviewing can do that")

type reviewReadWriter interface {
	CreateThread(ctx context.Context, params reviewstore.CreateThreadParams) (*reviewstore.Thread, error)
	ListThreads(ctx context.Context, params reviewstore.ListThreadsParams) ([]reviewstore.Thread, error)
	GetThread(ctx context.Context, id string) (*reviewstore.Thread, error)
	MarkThreadRead(ctx context.Context, id string, reader reviewstore.AuthorKind) (*reviewstore.Thread, error)
	CountUnreadForAgent(ctx context.Context, agentID string) (int64, error)
	DeleteThread(ctx context.Context, id string) error
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
	Kind reviewstore.AuthorKind

	AgentID *string
}

// IsHuman reports whether the call came from the person reviewing rather than from an agent.
func (a ReviewAuthor) IsHuman() bool { return a.Kind != reviewstore.AuthorAgent }

// ReviewThreadWithMessages is a thread and everything said in it.
type ReviewThreadWithMessages struct {
	Thread reviewstore.Thread

	Messages []reviewstore.Message
}

type ReviewServiceParams struct {
	Reviews     reviewReadWriter
	Mailer      reviewNoticeSender
	Interrupter agentInterrupter
}

// ReviewService owns the conversations pinned to the lines of an agent's diff. It holds the
// only copy: mail carries no feedback, it only says there is some, so a message rewritten
// after the agent has read it is simply read again as it now stands.
type ReviewService struct {
	reviews     reviewReadWriter
	mailer      reviewNoticeSender
	interrupter agentInterrupter
}

// NewReviewService builds the service over the review store and the things it tells.
func NewReviewService(params ReviewServiceParams) *ReviewService {
	return &ReviewService{
		reviews:     params.Reviews,
		mailer:      params.Mailer,
		interrupter: params.Interrupter,
	}
}

type ListReviewThreadsParams struct {
	AgentID string
	// MarkRead claims what is returned. An agent reading in order to act should claim, so the
	// same threads are not announced again; the panel drawing a diff should not.
	MarkRead bool

	Author ReviewAuthor
}

// ListReviewThreads is every thread on an agent's diff with its messages.
//
// Anyone may read anyone's review, but only the side the threads are addressed to may claim
// them. An agent claiming another agent's review would move a watermark on feedback that
// agent has never been shown.
func (s *ReviewService) ListReviewThreads(
	ctx context.Context,
	params ListReviewThreadsParams,
) ([]ReviewThreadWithMessages, error) {
	if params.MarkRead && !params.Author.IsHuman() && !doesOwnAgent(params.Author, params.AgentID) {
		return nil, fmt.Errorf("%w: %q", ErrReviewNotYours, params.AgentID)
	}

	threads, err := s.reviews.ListThreads(ctx, reviewstore.ListThreadsParams{
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
		answered = append(answered, ReviewThreadWithMessages{Thread: thread, Messages: byThread[thread.ID]})
	}

	return answered, nil
}

type CreateReviewThreadParams struct {
	AgentID  string
	FilePath string

	OldStart int32
	OldEnd   int32
	NewStart int32
	NewEnd   int32

	QuotedLines []string

	Body   string
	Author ReviewAuthor
}

// CreateReviewThread pins a conversation to a place in a diff and writes its first message.
// Only the person reviewing opens one: a thread is feedback on an agent's work, so an agent
// opening one on itself would be talking to nobody.
func (s *ReviewService) CreateReviewThread(
	ctx context.Context,
	params CreateReviewThreadParams,
) (*ReviewThreadWithMessages, error) {
	if !params.Author.IsHuman() {
		return nil, fmt.Errorf("%w: opening a review thread", ErrReviewHumanOnly)
	}

	thread, err := s.reviews.CreateThread(ctx, reviewstore.CreateThreadParams{
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

	messages, err := s.reviews.ListMessages(ctx, []string{thread.ID})
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

	return s.reviews.AddMessage(ctx, reviewstore.AddMessageParams{
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

// EditReviewMessage rewrites what was said, which only the author of that message may do. A
// rewrite counts as new activity, since the other side was shown the old text and the thread
// now says something else.
func (s *ReviewService) EditReviewMessage(
	ctx context.Context,
	params EditReviewMessageParams,
) (*reviewstore.Message, error) {
	if err := s.requireOwnMessage(ctx, params.ID, params.Author); err != nil {
		return nil, err
	}

	return s.reviews.EditMessage(ctx, params.ID, params.Body)
}

type MarkReviewThreadReadParams struct {
	ID     string
	Author ReviewAuthor
}

// MarkReviewThreadRead catches one side up on a single conversation, which is what opening a
// thread does. Claiming the whole list instead would say the person had read threads they
// never opened.
func (s *ReviewService) MarkReviewThreadRead(
	ctx context.Context,
	params MarkReviewThreadReadParams,
) (*reviewstore.Thread, error) {
	if err := s.requireOwnThread(ctx, params.ID, params.Author); err != nil {
		return nil, err
	}

	return s.reviews.MarkThreadRead(ctx, params.ID, params.Author.Kind)
}

type DeleteReviewThreadParams struct {
	ID     string
	Author ReviewAuthor
}

// DeleteReviewThread removes a thread and everything said in it. Only the person reviewing
// does, since an agent deleting the feedback on its own work would be marking its own paper.
func (s *ReviewService) DeleteReviewThread(ctx context.Context, params DeleteReviewThreadParams) error {
	if !params.Author.IsHuman() {
		return fmt.Errorf("%w: deleting a review thread", ErrReviewHumanOnly)
	}

	thread, err := s.reviews.GetThread(ctx, params.ID)
	if err != nil {
		return err
	}
	if thread == nil {
		return fmt.Errorf("%w: %q", ErrReviewThreadNotFound, params.ID)
	}

	return s.reviews.DeleteThread(ctx, params.ID)
}

type DeleteReviewMessageParams struct {
	ID     string
	Author ReviewAuthor
}

// DeleteReviewMessage removes one message, leaving the thread it was in. Only its author may.
func (s *ReviewService) DeleteReviewMessage(ctx context.Context, params DeleteReviewMessageParams) error {
	if err := s.requireOwnMessage(ctx, params.ID, params.Author); err != nil {
		return err
	}

	return s.reviews.DeleteMessage(ctx, params.ID)
}

// NotifyReviewThreadsResult is what came of pressing the doorbell.
type NotifyReviewThreadsResult struct {
	// Unread is how many open threads hold activity the agent has not read.
	Unread int64
	// AlreadyWaiting says a doorbell the agent has not claimed was already in its mailbox, so
	// none was raised. Pressing the key again is not a second thing to read.
	AlreadyWaiting bool
}

type NotifyReviewThreadsParams struct {
	AgentID string
	// Interrupt stops the agent where it stands before the mail is sent, so it reads the
	// review now rather than at the end of whatever it is doing.
	Interrupt bool

	Author ReviewAuthor
}

// NotifyReviewThreads tells an agent it has review activity waiting, as one piece of mail
// carrying a count and the name of the tool that fetches the conversation. The feedback
// itself is deliberately not sent: the threads are the record, and mail that quoted them
// would be a second copy going stale the moment either side wrote again.
//
// An agent already up to date is told nothing, and an agent still holding a doorbell it has
// not read is not handed a second one.
func (s *ReviewService) NotifyReviewThreads(
	ctx context.Context,
	params NotifyReviewThreadsParams,
) (*NotifyReviewThreadsResult, error) {
	if !params.Author.IsHuman() {
		return nil, fmt.Errorf("%w: announcing a review", ErrReviewHumanOnly)
	}

	unread, err := s.reviews.CountUnreadForAgent(ctx, params.AgentID)
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

	sent, err := s.mailer.SendNotice(ctx, SendNoticeParams{
		ToAgentID: params.AgentID,
		Body:      body,
		Kind:      mailstore.KindReviewActivity,
	})
	if err != nil {
		return nil, err
	}

	return &NotifyReviewThreadsResult{Unread: unread, AlreadyWaiting: sent == nil}, nil
}

// interrupt stops the agent where it stands. It is best effort: failing to interrupt is not a
// reason to withhold the notification, since the mail still reaches the agent at the end of
// what it is doing.
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
	thread, err := s.reviews.GetThread(ctx, id)
	if err != nil {
		return err
	}
	if thread == nil {
		return fmt.Errorf("%w: %q", ErrReviewThreadNotFound, id)
	}
	if !author.IsHuman() && !doesOwnAgent(author, thread.AgentID) {
		return fmt.Errorf("%w: %q", ErrReviewNotYours, id)
	}

	return nil
}

// requireOwnMessage refuses a caller rewriting or removing words it did not write.
func (s *ReviewService) requireOwnMessage(ctx context.Context, id string, author ReviewAuthor) error {
	message, err := s.reviews.GetMessage(ctx, id)
	if err != nil {
		return err
	}
	if message == nil {
		return fmt.Errorf("%w: %q", ErrReviewMessageNotFound, id)
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
