package agents

import (
	"context"
	"errors"
	"fmt"

	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	reviewstore "weave/internal/store/review"
)

// ErrReviewThreadNotFound is answered when a thread id names nothing.
var ErrReviewThreadNotFound = errors.New("review thread not found")

// ErrReviewMessageNotFound is answered when a message id names nothing.
var ErrReviewMessageNotFound = errors.New("review message not found")

// ErrReviewNotOwner is answered when an agent reaches for a review that is not on its own
// diff, which is somebody else's conversation whatever it means to do with it.
var ErrReviewNotOwner = errors.New("that review is on another agent's diff")

// ReviewNoticeKind names the one piece of mail an agent holds at a time saying its diff has
// been reviewed. It is the doorbell rather than the message: the conversation stays in the
// threads, so what the agent reads is what the review now says rather than what it said when
// the notice was written.
const ReviewNoticeKind = "review"

const reviewNoticeBody = "weave: your diff has review activity you have not read, call list_review_threads"

// reviewRepository is the review as whole conversations. The operations are the ones the panel
// and the agents actually perform, rather than the tables under them, because each has to land
// whole: a message and the watermark accounting for it, or a read and the claim on what it
// returned.
type reviewRepository interface {
	CreateThreadWithMessage(ctx context.Context, params reviewstore.CreateThreadParams) (*reviewstore.ThreadWithMessages, error)
	ListAndMarkRead(ctx context.Context, params reviewstore.ListThreadsParams) ([]reviewstore.ThreadWithMessages, error)
	GetThread(ctx context.Context, id string) (*reviewstore.ReviewThread, error)
	AddMessage(ctx context.Context, params reviewstore.AddMessageParams) (*reviewstore.ReviewMessage, error)
	EditMessage(ctx context.Context, params reviewstore.EditMessageParams) (*reviewstore.ReviewMessage, error)
	DeleteMessage(ctx context.Context, id string, author reviewstore.Author) (bool, error)
	MarkThreadReadByHuman(ctx context.Context, id string) (*reviewstore.ReviewThread, error)
	SetResolved(ctx context.Context, id string, resolved bool) (*reviewstore.ReviewThread, error)
	DeleteThread(ctx context.Context, id string) error
	CountUnread(ctx context.Context, agentID string) (int64, error)
	ListAgentsWithUnread(ctx context.Context) ([]string, error)
}

// systemNoticeSender is weave's own mailbox write: one notice of a kind per agent, which is
// what keeps a hundred edits to a review from becoming a hundred doorbells.
type systemNoticeSender interface {
	SendSystemNotice(ctx context.Context, toAgentID, kind, body string) (*mailstore.Mail, error)
}

// agentInterrupter stops an agent where it stands, so it reads a review now rather than at the
// end of whatever it is doing. What that takes is the harness's business, not this service's.
type agentInterrupter interface {
	Interrupt(ctx context.Context, agentID string) error
}

type ReviewServiceParams struct {
	Reviews     reviewRepository
	Notices     systemNoticeSender
	Agents      agentGetter
	Interrupter agentInterrupter
}

// ReviewService owns the conversations pinned to the lines of an agent's diff. It holds the
// only copy: mail carries no feedback, it only says there is some, so a message rewritten
// after the agent has read it is simply read again as it now stands.
type ReviewService struct {
	reviews     reviewRepository
	notices     systemNoticeSender
	agents      agentGetter
	interrupter agentInterrupter
}

// NewReviewService builds the service over the review repository and the things it tells.
func NewReviewService(params ReviewServiceParams) *ReviewService {
	return &ReviewService{
		reviews:     params.Reviews,
		notices:     params.Notices,
		agents:      params.Agents,
		interrupter: params.Interrupter,
	}
}

type ListReviewThreadsParams struct {
	AgentID string
	// Reader is the side asking, which decides what claiming means and whether it is allowed
	// at all: only the agent whose diff this is may claim its own review.
	Reader reviewstore.Author
	// MarkRead claims what is returned. An agent reading in order to act should claim, so the
	// same threads are not announced again; the panel drawing a diff should not.
	MarkRead bool
}

// ListReviewThreads is every thread on an agent's diff with its messages. An agent may look at
// another agent's review, but only the agent being reviewed may claim it: claiming for
// somebody else would tell the panel that agent had read something it was never shown.
func (s *ReviewService) ListReviewThreads(
	ctx context.Context,
	params ListReviewThreadsParams,
) ([]reviewstore.ThreadWithMessages, error) {
	markRead := params.MarkRead
	if markRead && !ownsReview(params.Reader, params.AgentID) {
		return nil, fmt.Errorf("%w: %q cannot claim it", ErrReviewNotOwner, params.AgentID)
	}

	return s.reviews.ListAndMarkRead(ctx, reviewstore.ListThreadsParams{
		AgentID:  params.AgentID,
		MarkRead: markRead,
	})
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
	Author reviewstore.Author
}

// CreateReviewThread pins a conversation to a place in a diff and writes its first message.
func (s *ReviewService) CreateReviewThread(
	ctx context.Context,
	params CreateReviewThreadParams,
) (*reviewstore.ThreadWithMessages, error) {
	return s.reviews.CreateThreadWithMessage(ctx, reviewstore.CreateThreadParams{
		AgentID:     params.AgentID,
		FilePath:    params.FilePath,
		OldStart:    params.OldStart,
		OldEnd:      params.OldEnd,
		NewStart:    params.NewStart,
		NewEnd:      params.NewEnd,
		QuotedLines: params.QuotedLines,
		Body:        params.Body,
		Author:      params.Author,
	})
}

type AddReviewMessageParams struct {
	ThreadID string
	Body     string
	Author   reviewstore.Author
}

// AddReviewMessage says something in an existing thread. An agent may only speak in the
// conversation about its own diff.
func (s *ReviewService) AddReviewMessage(
	ctx context.Context,
	params AddReviewMessageParams,
) (*reviewstore.ReviewMessage, error) {
	if err := s.requireOwnThread(ctx, params.ThreadID, params.Author); err != nil {
		return nil, err
	}

	message, err := s.reviews.AddMessage(ctx, reviewstore.AddMessageParams{
		ThreadID: params.ThreadID,
		Body:     params.Body,
		Author:   params.Author,
	})
	if err != nil {
		return nil, err
	}
	if message == nil {
		return nil, fmt.Errorf("%w: %q", ErrReviewThreadNotFound, params.ThreadID)
	}

	return message, nil
}

type EditReviewMessageParams struct {
	ID     string
	Body   string
	Author reviewstore.Author
}

// EditReviewMessage rewrites what was said, which counts as new activity: the other side was
// told the old text and the thread now says something else. Only the author may rewrite it.
func (s *ReviewService) EditReviewMessage(
	ctx context.Context,
	params EditReviewMessageParams,
) (*reviewstore.ReviewMessage, error) {
	message, err := s.reviews.EditMessage(ctx, reviewstore.EditMessageParams{
		ID:     params.ID,
		Body:   params.Body,
		Author: params.Author,
	})
	if err != nil {
		return nil, err
	}
	if message == nil {
		return nil, fmt.Errorf("%w: %q", ErrReviewMessageNotFound, params.ID)
	}

	return message, nil
}

// ResolveReviewThread settles a thread or reopens it.
func (s *ReviewService) ResolveReviewThread(
	ctx context.Context,
	id string,
	resolved bool,
	author reviewstore.Author,
) (*reviewstore.ReviewThread, error) {
	if err := s.requireOwnThread(ctx, id, author); err != nil {
		return nil, err
	}

	thread, err := s.reviews.SetResolved(ctx, id, resolved)
	if err != nil {
		return nil, err
	}
	if thread == nil {
		return nil, fmt.Errorf("%w: %q", ErrReviewThreadNotFound, id)
	}

	return thread, nil
}

// MarkReviewThreadRead is the person reviewing acknowledging one conversation, which is what
// opening it means. Without it a thread the agent has replied in stays marked as something
// they have not seen for as long as it exists.
func (s *ReviewService) MarkReviewThreadRead(ctx context.Context, id string) (*reviewstore.ReviewThread, error) {
	thread, err := s.reviews.MarkThreadReadByHuman(ctx, id)
	if err != nil {
		return nil, err
	}
	if thread == nil {
		return nil, fmt.Errorf("%w: %q", ErrReviewThreadNotFound, id)
	}

	return thread, nil
}

// DeleteReviewThread removes a thread and everything said in it.
func (s *ReviewService) DeleteReviewThread(ctx context.Context, id string) error {
	thread, err := s.reviews.GetThread(ctx, id)
	if err != nil {
		return err
	}
	if thread == nil {
		return fmt.Errorf("%w: %q", ErrReviewThreadNotFound, id)
	}

	return s.reviews.DeleteThread(ctx, id)
}

// DeleteReviewMessage removes one message, leaving the thread it was in. Only its author may.
func (s *ReviewService) DeleteReviewMessage(ctx context.Context, id string, author reviewstore.Author) error {
	deleted, err := s.reviews.DeleteMessage(ctx, id, author)
	if err != nil {
		return err
	}
	if !deleted {
		return fmt.Errorf("%w: %q", ErrReviewMessageNotFound, id)
	}

	return nil
}

type NotifyReviewThreadsParams struct {
	AgentID string
	// Interrupt stops the agent where it stands before the notice is sent, so it reads the
	// review now rather than at the end of whatever it is doing.
	Interrupt bool
}

// ReviewNotification is what telling an agent about its review actually achieved, which is
// worth saying rather than assuming: the notice may already have been waiting, the interrupt
// may not have reached a pane, and an agent sitting at a prompt of its own will not be typed
// at until it answers.
type ReviewNotification struct {
	// Unread is how many threads hold activity the agent has not read.
	Unread int64
	// Rang is true when this call is what put the notice in the mailbox, and false when one
	// was already waiting there unread.
	Rang bool
	// Interrupted is true when the agent's harness was stopped where it stood.
	Interrupted bool
	// Waiting is true when the agent is sitting at a prompt, which weave will not type
	// through: the notice waits in the mailbox until the agent answers whatever it is asking.
	Waiting bool
}

// NotifyReviewThreads tells an agent it has review activity waiting, as one piece of mail
// naming the tool that fetches the conversation. The feedback itself is deliberately not sent:
// the threads are the record, and mail that quoted them would be a second copy going stale the
// moment either side wrote again.
func (s *ReviewService) NotifyReviewThreads(
	ctx context.Context,
	params NotifyReviewThreadsParams,
) (ReviewNotification, error) {
	unread, err := s.reviews.CountUnread(ctx, params.AgentID)
	if err != nil {
		return ReviewNotification{}, err
	}
	if unread == 0 {
		return ReviewNotification{}, nil
	}

	agent, err := s.agents.GetByID(ctx, params.AgentID)
	if err != nil {
		return ReviewNotification{}, err
	}
	if agent == nil {
		return ReviewNotification{}, fmt.Errorf("%w: %q", ErrAgentNotFound, params.AgentID)
	}

	answered := ReviewNotification{
		Unread:  unread,
		Waiting: agent.State == agentstore.StateWaiting,
	}

	if params.Interrupt && s.interrupter != nil {
		answered.Interrupted = s.interrupter.Interrupt(ctx, params.AgentID) == nil
	}

	sent, err := s.notices.SendSystemNotice(ctx, params.AgentID, ReviewNoticeKind, reviewNoticeBody)
	if err != nil {
		return ReviewNotification{}, err
	}
	answered.Rang = sent != nil

	return answered, nil
}

// ReconcileDoorbells makes sure every agent with review activity it has not read is holding a
// notice about it. It runs on a loop rather than only when the panel rings, because a notice
// has a delivery deadline: an agent that was dormant while its doorbell expired would
// otherwise never be told, even though the review is still sitting there unread.
func (s *ReviewService) ReconcileDoorbells(ctx context.Context) error {
	agentIDs, err := s.reviews.ListAgentsWithUnread(ctx)
	if err != nil {
		return err
	}

	for _, agentID := range agentIDs {
		agent, err := s.agents.GetByID(ctx, agentID)
		if err != nil {
			return err
		}
		if agent == nil || agent.ArchivedAt != nil {
			continue
		}

		if _, err := s.notices.SendSystemNotice(ctx, agentID, ReviewNoticeKind, reviewNoticeBody); err != nil {
			return err
		}
	}

	return nil
}

// requireOwnThread refuses an id that names no thread, so a write against one that has been
// deleted answers not found rather than failing on a foreign key, and refuses an agent
// reaching into the conversation about somebody else's diff.
func (s *ReviewService) requireOwnThread(ctx context.Context, id string, author reviewstore.Author) error {
	thread, err := s.reviews.GetThread(ctx, id)
	if err != nil {
		return err
	}
	if thread == nil {
		return fmt.Errorf("%w: %q", ErrReviewThreadNotFound, id)
	}
	if !ownsReview(author, thread.AgentID) {
		return fmt.Errorf("%w: %q", ErrReviewNotOwner, id)
	}

	return nil
}

// ownsReview reports whether an author may act on the review of agentID. The person reviewing
// owns every one of them; an agent owns only its own.
func ownsReview(author reviewstore.Author, agentID string) bool {
	if author.Kind != reviewstore.ReviewAuthorAgent {
		return true
	}

	return author.AgentID != nil && *author.AgentID == agentID
}
