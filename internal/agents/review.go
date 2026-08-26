package agents

import (
	"context"
	"errors"
	"fmt"
	"time"

	reviewstore "weave/internal/store/review"
)

// ErrReviewThreadNotFound is answered when a thread id names nothing.
var ErrReviewThreadNotFound = errors.New("review thread not found")

// ErrReviewMessageNotFound is answered when a message id names nothing.
var ErrReviewMessageNotFound = errors.New("review message not found")

// interruptSettleFor is how long the agent is given to act on the interrupt keys before the
// mail lands behind them, so the notification is typed into a composer rather than into
// whatever was still on screen.
const interruptSettleFor = 200 * time.Millisecond

type reviewReadWriter interface {
	CreateReviewThread(ctx context.Context, params reviewstore.CreateReviewThreadParams) (*reviewstore.ReviewThread, error)
	ListReviewThreads(ctx context.Context, agentID string) ([]reviewstore.ReviewThread, error)
	GetReviewThread(ctx context.Context, id string) (*reviewstore.ReviewThread, error)
	SetReviewThreadResolved(ctx context.Context, id string, resolved bool) (*reviewstore.ReviewThread, error)
	RecordReviewThreadActivity(ctx context.Context, threadID string, kind reviewstore.ReviewAuthorKind) error
	MarkReviewThreadsRead(ctx context.Context, ids []string, kind reviewstore.ReviewAuthorKind, readAt time.Time) error
	CountUnreadReviewThreads(ctx context.Context, agentID string) (int64, error)
	DeleteReviewThread(ctx context.Context, id string) error
	AddReviewMessage(ctx context.Context, params reviewstore.AddReviewMessageParams) (*reviewstore.ReviewMessage, error)
	SetReviewMessageBody(ctx context.Context, id, body string) (*reviewstore.ReviewMessage, error)
	GetReviewMessage(ctx context.Context, id string) (*reviewstore.ReviewMessage, error)
	ListReviewMessages(ctx context.Context, threadIDs []string) ([]reviewstore.ReviewMessage, error)
	DeleteReviewMessage(ctx context.Context, id string) error
}

type ReviewServiceParams struct {
	Reviews   reviewReadWriter
	Mailer    systemMailSender
	Agents    agentGetter
	Harnesses harnessGetter
	Panes     paneTyper
}

// ReviewService owns the conversations pinned to the lines of an agent's diff. It holds the
// only copy: mail carries no feedback, it only says there is some, so a message rewritten
// after the agent has read it is simply read again as it now stands.
type ReviewService struct {
	reviews   reviewReadWriter
	mailer    systemMailSender
	agents    agentGetter
	harnesses harnessGetter
	panes     paneTyper
}

// NewReviewService builds the service over the review store and the things it tells.
func NewReviewService(params ReviewServiceParams) *ReviewService {
	return &ReviewService{
		reviews:   params.Reviews,
		mailer:    params.Mailer,
		agents:    params.Agents,
		harnesses: params.Harnesses,
		panes:     params.Panes,
	}
}

// ReviewAuthor is who a write is attributed to, decided from the calling identity rather than
// from anything the caller asks for: the panel names no agent and writes as the person, an
// agent names itself and writes as itself.
type ReviewAuthor struct {
	Kind reviewstore.ReviewAuthorKind

	AgentID *string
}

// ReviewThreadWithMessages is a thread and everything said in it.
type ReviewThreadWithMessages struct {
	Thread reviewstore.ReviewThread

	Messages []reviewstore.ReviewMessage
}

type ListReviewThreadsParams struct {
	AgentID string
	// Reader is the side asking, which decides whose watermark MarkRead moves.
	Reader reviewstore.ReviewAuthorKind
	// MarkRead claims what is returned. An agent reading in order to act should claim, so the
	// same threads are not announced again; the panel drawing a diff should not.
	MarkRead bool
}

// ListReviewThreads is every thread on an agent's diff with its messages.
//
// When the caller claims what it reads, the watermark is stamped to the newest message
// actually returned rather than to the clock. A message written between the read and the
// stamp would otherwise be marked read without ever being handed over.
func (s *ReviewService) ListReviewThreads(
	ctx context.Context,
	params ListReviewThreadsParams,
) ([]ReviewThreadWithMessages, error) {
	threads, err := s.reviews.ListReviewThreads(ctx, params.AgentID)
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

	messages, err := s.reviews.ListReviewMessages(ctx, ids)
	if err != nil {
		return nil, err
	}

	byThread := map[string][]reviewstore.ReviewMessage{}
	newest := time.Time{}
	for _, message := range messages {
		byThread[message.ThreadID] = append(byThread[message.ThreadID], message)
		if message.CreatedAt.After(newest) {
			newest = message.CreatedAt
		}
	}

	answered := make([]ReviewThreadWithMessages, 0, len(threads))
	for _, thread := range threads {
		answered = append(answered, ReviewThreadWithMessages{Thread: thread, Messages: byThread[thread.ID]})
	}

	if params.MarkRead && !newest.IsZero() {
		if err := s.reviews.MarkReviewThreadsRead(ctx, ids, params.Reader, newest); err != nil {
			return nil, err
		}
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
func (s *ReviewService) CreateReviewThread(
	ctx context.Context,
	params CreateReviewThreadParams,
) (*ReviewThreadWithMessages, error) {
	thread, err := s.reviews.CreateReviewThread(ctx, reviewstore.CreateReviewThreadParams{
		AgentID:     params.AgentID,
		FilePath:    params.FilePath,
		OldStart:    params.OldStart,
		OldEnd:      params.OldEnd,
		NewStart:    params.NewStart,
		NewEnd:      params.NewEnd,
		QuotedLines: params.QuotedLines,
	})
	if err != nil {
		return nil, err
	}

	message, err := s.writeReviewMessage(ctx, thread.ID, params.Body, params.Author)
	if err != nil {
		return nil, err
	}

	return &ReviewThreadWithMessages{Thread: *thread, Messages: []reviewstore.ReviewMessage{*message}}, nil
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
) (*reviewstore.ReviewMessage, error) {
	if err := s.requireReviewThread(ctx, params.ThreadID); err != nil {
		return nil, err
	}

	return s.writeReviewMessage(ctx, params.ThreadID, params.Body, params.Author)
}

// writeReviewMessage writes the message and records the activity it is. Recording reopens the
// thread, and for the person reviewing it also advances the watermark the agent's unread flag
// is measured against, which is what makes a rewrite count as something new to read.
func (s *ReviewService) writeReviewMessage(
	ctx context.Context,
	threadID, body string,
	author ReviewAuthor,
) (*reviewstore.ReviewMessage, error) {
	message, err := s.reviews.AddReviewMessage(ctx, reviewstore.AddReviewMessageParams{
		ThreadID:      threadID,
		Kind:          author.Kind,
		AuthorAgentID: author.AgentID,
		Body:          body,
	})
	if err != nil {
		return nil, err
	}

	if err := s.reviews.RecordReviewThreadActivity(ctx, threadID, author.Kind); err != nil {
		return nil, err
	}

	return message, nil
}

// EditReviewMessage rewrites what was said. A rewrite by the person reviewing counts as new
// activity, since the agent was told the old text and the thread now says something else.
func (s *ReviewService) EditReviewMessage(
	ctx context.Context,
	id, body string,
	author ReviewAuthor,
) (*reviewstore.ReviewMessage, error) {
	existing, err := s.reviews.GetReviewMessage(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("%w: %q", ErrReviewMessageNotFound, id)
	}

	message, err := s.reviews.SetReviewMessageBody(ctx, id, body)
	if err != nil {
		return nil, err
	}

	if err := s.reviews.RecordReviewThreadActivity(ctx, existing.ThreadID, author.Kind); err != nil {
		return nil, err
	}

	return message, nil
}

// ResolveReviewThread settles a thread or reopens it.
func (s *ReviewService) ResolveReviewThread(
	ctx context.Context,
	id string,
	resolved bool,
) (*reviewstore.ReviewThread, error) {
	if err := s.requireReviewThread(ctx, id); err != nil {
		return nil, err
	}

	return s.reviews.SetReviewThreadResolved(ctx, id, resolved)
}

// DeleteReviewThread removes a thread and everything said in it.
func (s *ReviewService) DeleteReviewThread(ctx context.Context, id string) error {
	if err := s.requireReviewThread(ctx, id); err != nil {
		return err
	}

	return s.reviews.DeleteReviewThread(ctx, id)
}

// DeleteReviewMessage removes one message, leaving the thread it was in.
func (s *ReviewService) DeleteReviewMessage(ctx context.Context, id string) error {
	existing, err := s.reviews.GetReviewMessage(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("%w: %q", ErrReviewMessageNotFound, id)
	}

	return s.reviews.DeleteReviewMessage(ctx, id)
}

type NotifyReviewThreadsParams struct {
	AgentID string
	// Interrupt stops the agent where it stands before the mail is sent, so it reads the
	// review now rather than at the end of whatever it is doing.
	Interrupt bool
}

// NotifyReviewThreads tells an agent it has review activity waiting, as one piece of mail
// carrying a count and the name of the tool that fetches the conversation. The feedback
// itself is deliberately not sent: the threads are the record, and mail that quoted them
// would be a second copy going stale the moment either side wrote again.
//
// An agent already up to date is told nothing, so pressing the key twice does not announce a
// review that has not changed.
func (s *ReviewService) NotifyReviewThreads(ctx context.Context, params NotifyReviewThreadsParams) (int64, error) {
	unread, err := s.reviews.CountUnreadReviewThreads(ctx, params.AgentID)
	if err != nil {
		return 0, err
	}
	if unread == 0 {
		return 0, nil
	}

	if params.Interrupt {
		s.interruptAgent(ctx, params.AgentID)
	}

	body := fmt.Sprintf(
		"weave: %d review thread(s) on your diff have activity you have not read, call list_review_threads",
		unread)

	if _, err := s.mailer.SendSystemMail(ctx, params.AgentID, body); err != nil {
		return 0, err
	}

	return unread, nil
}

// interruptAgent sends whatever the agent's CLI reads as an interrupt. It is best effort:
// failing to interrupt is not a reason to withhold the notification, since the mail still
// reaches the agent at the end of what it is doing.
func (s *ReviewService) interruptAgent(ctx context.Context, agentID string) {
	agent, err := s.agents.GetByID(ctx, agentID)
	if err != nil || agent == nil || agent.Pane == "" {
		return
	}

	running, err := s.harnesses.GetByID(agent.Harness)
	if err != nil {
		return
	}

	keys := running.GetInterruptKeys()
	if len(keys) == 0 {
		return
	}

	if err := s.panes.SendKeys(agent.Pane, keys...); err != nil {
		return
	}

	time.Sleep(interruptSettleFor)
}

// requireReviewThread refuses an id that names no thread, so a write against one that has
// been deleted answers not found rather than failing on a foreign key.
func (s *ReviewService) requireReviewThread(ctx context.Context, id string) error {
	thread, err := s.reviews.GetReviewThread(ctx, id)
	if err != nil {
		return err
	}
	if thread == nil {
		return fmt.Errorf("%w: %q", ErrReviewThreadNotFound, id)
	}

	return nil
}
