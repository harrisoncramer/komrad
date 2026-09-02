package agents

import (
	"context"
	"errors"
	"fmt"
	"time"

	"weave/internal/store"
	mailstore "weave/internal/store/mail"
	reviewstore "weave/internal/store/review"
	"weave/lib/xslice"
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
	EditMessage(ctx context.Context, id, body string) (*reviewstore.Message, error)
	GetMessage(ctx context.Context, id string) (*reviewstore.Message, error)
	ListMessages(ctx context.Context, threadIDs []string) ([]reviewstore.Message, error)
	CountUnsentMessages(ctx context.Context, agentID string) (int, error)
	ReleaseUnsentMessages(ctx context.Context, agentID, doorbellMailID string) ([]reviewstore.Message, error)
	PointMessagesAtDoorbell(ctx context.Context, ids []string, doorbellMailID string) ([]reviewstore.Message, error)
	ListExpiredMessages(ctx context.Context, agentID string) ([]reviewstore.Message, error)
	DeleteMessage(ctx context.Context, id string) error
}

// reviewViewedFileReadWriter is the record of which files the person reviewing has read, kept
// apart from the conversations since it is about their own progress rather than about anything
// said to the agent.
type reviewViewedFileReadWriter interface {
	MarkFileViewed(ctx context.Context, params reviewstore.MarkFileViewedParams) (*reviewstore.ViewedFile, error)
	UnmarkFileViewed(ctx context.Context, agentID, filePath string) error
	ListViewedFiles(ctx context.Context, agentID string) ([]reviewstore.ViewedFile, error)
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
	Agents      agentGetter
	Mailer      reviewNoticeSender
	Interrupter agentInterrupter
}

// ReviewService owns the conversations pinned to the lines of an agent's diff and holds the only
// copy, since mail says feedback exists without carrying any of it.
type ReviewService struct {
	threads     reviewThreadReadWriter
	messages    reviewMessageReadWriter
	viewed      reviewViewedFileReadWriter
	agents      agentGetter
	mailer      reviewNoticeSender
	interrupter agentInterrupter
}

// NewReviewService builds the service over the review store and the things it tells.
func NewReviewService(params ReviewServiceParams) *ReviewService {
	return &ReviewService{
		threads:     params.Threads,
		messages:    params.Messages,
		viewed:      params.Viewed,
		agents:      params.Agents,
		mailer:      params.Mailer,
		interrupter: params.Interrupter,
	}
}

// ListReviewThreads is every thread on an agent's diff with its messages, which anyone may
// read since reading it changes nothing.
func (s *ReviewService) ListReviewThreads(
	ctx context.Context,
	agentID string,
) ([]ReviewThreadWithMessages, error) {
	threads, err := s.threads.ListThreads(ctx, agentID)
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
	AgentID  string
	FilePath string
	// Fingerprint is the shape the file had when it was read, which the panel compares against on
	// its next pass to decide whether the mark still stands, and which is read only when Viewed
	// is set.
	Fingerprint string
	Viewed      bool
	Author      ReviewAuthor
}

// SetReviewFileViewed marks one file of a diff read or drops the mark, which only the person
// reviewing may do, since it is a record of what they have looked at.
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

type ListReviewViewedFilesParams struct {
	AgentID string
	Author  ReviewAuthor
}

// ListReviewViewedFiles is every file of an agent's diff the person reviewing has marked read,
// which is theirs alone, so an agent is never told which parts of its work have been looked at.
func (s *ReviewService) ListReviewViewedFiles(
	ctx context.Context,
	params ListReviewViewedFilesParams,
) ([]reviewstore.ViewedFile, error) {
	if !params.Author.IsHuman() {
		return nil, fmt.Errorf("%w: reading which files have been viewed", ErrReviewHumanOnly)
	}

	return s.viewed.ListViewedFiles(ctx, params.AgentID)
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

// reviewDoorbellBody says that there is review to read and where to read it, and carries no
// feedback, so a comment rewritten after it is sent is read as it now stands rather than twice.
const reviewDoorbellBody = "weave: there is review on your diff you have not read, call list_review_threads"

// ReleaseReviewMessagesResult reports what letting go of a review did.
type ReleaseReviewMessagesResult struct {
	// Released is how many comments were stamped as sent, which is the durable record that
	// they were let go of, and is how many the one doorbell raised for them covers.
	Released int
}

type ReleaseReviewMessagesParams struct {
	AgentID string
	// Interrupt stops the agent where it stands before the mail is sent, so it reads the
	// review now rather than at the end of whatever it is doing.
	Interrupt bool
	Author    ReviewAuthor
}

// ReleaseReviewMessages lets go of every comment on an agent's review the person has written
// and not sent, stamping each and raising one doorbell for it, so that a review is written,
// reconsidered and let go of as one thought rather than reaching the agent a line at a time.
func (s *ReviewService) ReleaseReviewMessages(
	ctx context.Context,
	params ReleaseReviewMessagesParams,
) (*ReleaseReviewMessagesResult, error) {
	if !params.Author.IsHuman() {
		return nil, fmt.Errorf("%w: releasing a review", ErrReviewHumanOnly)
	}
	if err := s.requireLiveAgent(ctx, params.AgentID); err != nil {
		return nil, err
	}

	unsent, err := s.messages.CountUnsentMessages(ctx, params.AgentID)
	if err != nil {
		return nil, err
	}
	if unsent == 0 {
		return &ReleaseReviewMessagesResult{}, nil
	}

	if params.Interrupt {
		s.interrupt(ctx, params.AgentID)
	}

	doorbell, err := s.ring(ctx, params.AgentID)
	if err != nil {
		return nil, err
	}

	released, err := s.messages.ReleaseUnsentMessages(ctx, params.AgentID, doorbell.ID)
	if err != nil {
		return nil, err
	}

	return &ReleaseReviewMessagesResult{Released: len(released)}, nil
}

// RingReviewMessagesAgainResult reports what ringing an agent's review again did.
type RingReviewMessagesAgainResult struct {
	// Rung is how many released comments the fresh doorbell was raised for, which is every one
	// left with none the agent could still read.
	Rung int
}

type RingReviewMessagesAgainParams struct {
	AgentID string
	// Interrupt stops the agent where it stands before the mail is sent.
	Interrupt bool
	Author    ReviewAuthor
}

// RingReviewMessagesAgain raises a doorbell again for every released comment whose own ran out
// of time unclaimed, which is the only way those reach an agent that was not listening.
func (s *ReviewService) RingReviewMessagesAgain(
	ctx context.Context,
	params RingReviewMessagesAgainParams,
) (*RingReviewMessagesAgainResult, error) {
	if !params.Author.IsHuman() {
		return nil, fmt.Errorf("%w: ringing a review again", ErrReviewHumanOnly)
	}
	if err := s.requireLiveAgent(ctx, params.AgentID); err != nil {
		return nil, err
	}

	expired, err := s.messages.ListExpiredMessages(ctx, params.AgentID)
	if err != nil {
		return nil, err
	}
	if len(expired) == 0 {
		return &RingReviewMessagesAgainResult{}, nil
	}

	if params.Interrupt {
		s.interrupt(ctx, params.AgentID)
	}

	doorbell, err := s.ring(ctx, params.AgentID)
	if err != nil {
		return nil, err
	}

	ids := xslice.Map(expired, func(message reviewstore.Message) string { return message.ID })

	rung, err := s.messages.PointMessagesAtDoorbell(ctx, ids, doorbell.ID)
	if err != nil {
		return nil, err
	}

	return &RingReviewMessagesAgainResult{Rung: len(rung)}, nil
}

// ring is the one doorbell a whole release is heard through, either the notice the agent is
// already holding unread or a fresh one when it holds none, so that a review of any size costs
// the mailbox a single piece of mail and leaves the rest of it for what other agents have to
// say.
func (s *ReviewService) ring(ctx context.Context, agentID string) (*mailstore.Mail, error) {
	return s.mailer.SendNotice(ctx, SendNoticeParams{
		ToAgentID: agentID,
		Body:      reviewDoorbellBody,
		Kind:      mailstore.KindReviewActivity,
	})
}

// requireLiveAgent refuses a review of an agent that has been archived, since there is nobody
// there to hand it to and stamping a comment sent would say it had been told.
func (s *ReviewService) requireLiveAgent(ctx context.Context, agentID string) error {
	agent, err := s.agents.GetByID(ctx, agentID)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w: %q", ErrAgentNotFound, agentID)
	}
	if err != nil {
		return err
	}
	if agent.ArchivedAt != nil {
		return fmt.Errorf("%w: %q is archived, so there is nobody to hand a review to; restore it first", ErrAgentArchived, agent.Name)
	}

	return nil
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
