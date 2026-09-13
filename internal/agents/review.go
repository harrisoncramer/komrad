package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"weave/internal/fail"
	"weave/internal/logging"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	reviewstore "weave/internal/store/review"
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

// reviewThreadReadWriter is the half of the store that keeps conversations: where they are
// pinned, whether the person has read the replies, and whether they are still owed an answer.
type reviewThreadReadWriter interface {
	CreateThread(ctx context.Context, params reviewstore.CreateThreadParams) (*reviewstore.Thread, error)
	ListThreads(ctx context.Context, agentID string) ([]reviewstore.Thread, error)
	GetThread(ctx context.Context, id string) (*reviewstore.Thread, error)
	MarkThreadRead(ctx context.Context, id string) (*reviewstore.Thread, error)
	SetThreadResolved(ctx context.Context, id string, resolved bool) (*reviewstore.Thread, error)
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

// reviewerResumer puts a reviewer back in a pane, so a dormant one is not left with mail
// waiting forever.
type reviewerResumer interface {
	ResumeAgent(ctx context.Context, id string) (*agentstore.Agent, error)
}

type reviewNoticeSender interface {
	SendNotice(ctx context.Context, params SendNoticeParams) (*mailstore.Mail, error)
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
func (a ReviewAuthor) IsHuman() bool { return a.Kind != reviewstore.AuthorAgent }

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
	Mailer      reviewNoticeSender
	Resumer     reviewerResumer
	Interrupter agentInterrupter
	// StateDir is where weave keeps its own files, which is where a released review is written.
	StateDir string
	// NoticeDebounce overrides how long armReviewerNotice waits before telling a reviewer, which
	// defaults to reviewerWakeDebounce when left at zero.
	NoticeDebounce time.Duration
}

// ReviewService owns the conversations pinned to the lines of an agent's diff, and holds the
// only copy.
type ReviewService struct {
	threads        reviewThreadReadWriter
	messages       reviewMessageReadWriter
	viewed         reviewViewedFileReadWriter
	rounds         reviewRoundReadWriter
	reviews        reviewBatchReadWriter
	snapshots      reviewSnapshotter
	workflows      workflowGetter
	agents         agentGetter
	mailer         reviewNoticeSender
	resumer        reviewerResumer
	interrupter    agentInterrupter
	stateDir       string
	noticeDebounce time.Duration
	noticesMu      sync.Mutex
	notices        map[string]*pendingReviewerNotice
}

// NewReviewService builds the service over the review store and the things it tells.
func NewReviewService(params NewReviewServiceParams) *ReviewService {
	debounce := params.NoticeDebounce
	if debounce == 0 {
		debounce = reviewerWakeDebounce
	}

	return &ReviewService{
		threads:        params.Threads,
		messages:       params.Messages,
		viewed:         params.Viewed,
		rounds:         params.Rounds,
		reviews:        params.Reviews,
		snapshots:      params.Snapshots,
		workflows:      params.Workflows,
		agents:         params.Agents,
		mailer:         params.Mailer,
		resumer:        params.Resumer,
		interrupter:    params.Interrupter,
		stateDir:       params.StateDir,
		noticeDebounce: debounce,
		notices:        map[string]*pendingReviewerNotice{},
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
	// means whichever round is latest.
	RoundID string
	// Suggestion is replacement lines the reviewer proposes for the range, empty on an ordinary
	// comment.
	Suggestion []string
	// BaseSHA is the commit the old side of these line numbers was read against, empty for the
	// round's own base.
	BaseSHA string
	// SnapshotSHA is the commit the new side of these line numbers was read against, empty for a
	// caller that read the round's own snapshot.
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
	open, err := s.openReviewFor(ctx, params.AgentID, params.Author)
	if err != nil {
		return nil, err
	}

	roundID, baseSHA := params.RoundID, params.BaseSHA
	if roundID == "" {
		round, err := s.GetLatestRound(ctx, params.AgentID)
		if err != nil {
			return nil, err
		}

		roundID = round.ID
	}

	// Explainer: a review is written against one snapshot. Letting a second snapshot into the
	// batch already open would send the agent comments about two versions of its work in one
	// message, with nothing in the batch saying which comment meant which. So the reader sends
	// what they have, or throws it away, before starting on a newer snapshot.
	if open.RoundID != "" && open.RoundID != roundID {
		return nil, fmt.Errorf("%w: send or discard the comments you have on the earlier snapshot first", ErrReviewInFlight)
	}

	// A caller that says nothing read its numbers against the round's own base, which is what
	// every caller did before a diff could be narrowed.
	if baseSHA == "" || params.SnapshotSHA != "" {
		round, err := s.rounds.GetRound(ctx, roundID)
		if err != nil {
			return nil, err
		}

		// Explainer: a thread's new side is line numbers of one snapshot, and the row says which
		// round it belongs to rather than which snapshot it was read from. So a caller that read
		// an older snapshot is refused here: the numbers would be filed against a round whose
		// file they do not describe, and nothing later could tell that they were.
		if params.SnapshotSHA != "" && params.SnapshotSHA != round.SnapshotSHA {
			return nil, fmt.Errorf("%w: %s is not the snapshot of the round this would be filed against",
				ErrReviewNotCurrent, params.SnapshotSHA[:min(len(params.SnapshotSHA), 12)])
		}

		if baseSHA == "" {
			baseSHA = round.BaseSHA
		}
	}

	thread, err := s.threads.CreateThread(ctx, reviewstore.CreateThreadParams{
		AgentID:       params.AgentID,
		ReviewID:      open.ID,
		RoundID:       roundID,
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

// AddReviewMessageParams is what to say in an existing thread, and as whom.
type AddReviewMessageParams struct {
	ThreadID   string
	Body       string
	Suggestion []string
	Author     ReviewAuthor
}

// AddReviewMessage says something in an existing thread, filed under the review a reviewer
// still has open and under none at all for the agent answering.
func (s *ReviewService) AddReviewMessage(
	ctx context.Context,
	params AddReviewMessageParams,
) (*reviewstore.Message, error) {
	thread, err := s.getOwnThread(ctx, params.ThreadID, params.Author)
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

	message, err := s.messages.CreateMessage(ctx, reviewstore.CreateMessageParams{
		ThreadID:      params.ThreadID,
		ReviewID:      reviewID,
		Suggestion:    params.Suggestion,
		AuthorKind:    params.Author.Kind,
		AuthorAgentID: params.Author.AgentID,
		Body:          params.Body,
	})
	if err != nil {
		return nil, err
	}

	if reviewID == nil {
		s.ringTheReviewer(ctx, thread.AgentID)
	}

	return message, nil
}

// resumeWakeTimeout bounds a wake nobody is waiting on, so a post-resume hook that hangs cannot
// leave one running for the life of the panel.
const resumeWakeTimeout = hookTimeout + 5*time.Minute

// wakeTheReviewer puts a reviewer back in a pane, on its own so the workflow's post-resume hook
// does not hold up the agent writing the answer.
func (s *ReviewService) wakeTheReviewer(ctx context.Context, reviewer agentstore.Agent) {
	if s.resumer == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), resumeWakeTimeout)
	defer cancel()

	// Answers written at once each ask for the same reviewer, and the claim inside the resume is
	// what makes that one window rather than three.
	if _, err := s.resumer.ResumeAgent(ctx, reviewer.ID); err != nil && !errors.Is(err, ErrResumeInFlight) {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to put a reviewer back in a pane to be told about an answer",
			append([]slog.Attr{slog.String(logging.AgentID, reviewer.ID)}, logging.Err(err)...)...)
	}
}

// reviewerWakeDebounce is how long armReviewerNotice waits for another answer from the same
// author before it tells the reviewer, so a run of replies made one thread at a time collapses
// into the one notice and wake the last of them leaves behind.
const reviewerWakeDebounce = 5 * time.Second

// pendingReviewerNotice is the debounce armed for one author's pending answer, tracked by a
// generation a stale callback checks itself against.
type pendingReviewerNotice struct {
	timer      *time.Timer
	generation uint64
}

// ringTheReviewer arms the debounced notice for the author that just answered, on a context
// detached from the request so the notice can still be sent once that request has returned.
func (s *ReviewService) ringTheReviewer(ctx context.Context, agentID string) {
	author, err := s.agents.GetByID(ctx, agentID)
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to read the agent a review answer was written on",
			append([]slog.Attr{slog.String(logging.AgentID, agentID)}, logging.Err(err)...)...)

		return
	}

	s.armReviewerNotice(context.WithoutCancel(ctx), *author)
}

// armReviewerNotice (re)starts the debounce for this author's pending answer, resolved fresh by
// fireReviewerNotice once the window elapses.
func (s *ReviewService) armReviewerNotice(ctx context.Context, author agentstore.Agent) {
	s.noticesMu.Lock()
	defer s.noticesMu.Unlock()

	pending, ok := s.notices[author.ID]
	if !ok {
		pending = &pendingReviewerNotice{}
		s.notices[author.ID] = pending
	} else {
		pending.timer.Stop()
		pending.generation++
	}

	generation := pending.generation
	pending.timer = time.AfterFunc(s.noticeDebounce, func() {
		s.fireReviewerNotice(ctx, author, generation)
	})
}

// fireReviewerNotice sends the notice and wakes the author's current reviewer, unless a later
// answer already moved this author's debounce past the generation this callback was scheduled
// under.
func (s *ReviewService) fireReviewerNotice(ctx context.Context, author agentstore.Agent, generation uint64) {
	if !s.takeReviewerNotice(author.ID, generation) {
		return
	}

	reviewer, err := s.sendReviewerNotice(ctx, author)
	if err != nil {
		return
	}

	s.wakeTheReviewer(ctx, *reviewer)
}

// takeReviewerNotice reports whether the pending notice under this generation is still armed,
// removing it either way so a later answer starts clean.
func (s *ReviewService) takeReviewerNotice(authorID string, generation uint64) bool {
	s.noticesMu.Lock()
	defer s.noticesMu.Unlock()

	pending, ok := s.notices[authorID]
	if !ok || pending.generation != generation {
		return false
	}
	delete(s.notices, authorID)

	return true
}

// sendReviewerNotice looks up the author's live reviewer and tells it the author answered,
// logging and returning an error for anything other than the ordinary case of no reviewer left
// to tell.
func (s *ReviewService) sendReviewerNotice(ctx context.Context, author agentstore.Agent) (*agentstore.Agent, error) {
	reviewer, err := liveReviewerFor(ctx, s.agents, s.reviews, author)
	if errors.Is(err, ErrNoLiveReviewer) {
		return nil, err
	}
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to find the reviewer to tell about a review answer",
			append([]slog.Attr{slog.String(logging.AgentID, author.ID)}, logging.Err(err)...)...)

		return nil, err
	}

	if _, err := s.mailer.SendNotice(ctx, SendNoticeParams{
		ToAgentID: reviewer.ID,
		Body:      answerNotice(author.Name),
		Kind:      mailstore.KindReviewActivity,
		About:     &author.ID,
	}); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to tell a reviewer that its review was answered",
			append([]slog.Attr{slog.String(logging.AgentID, reviewer.ID)}, logging.Err(err)...)...)

		return nil, err
	}

	return reviewer, nil
}

// answerNotice is the notice a reviewer gets, which says where to look rather than what was
// said.
func answerNotice(authorName string) string {
	return fmt.Sprintf("weave: %s answered your review. Read the threads with list_review_threads, "+
		"open a round over its work as it now stands, and release a verdict on that round.", authorName)
}

// Close sends whatever notice each still-debounced answer was waiting to tell, without waking
// the reviewer, so a panel restart mid-window does not strand a reviewer with no word its
// answer landed; the mail is durable, and a resumed pane is handed it whenever it next shows up.
func (s *ReviewService) Close() {
	s.noticesMu.Lock()
	pending := s.notices
	s.notices = map[string]*pendingReviewerNotice{}
	s.noticesMu.Unlock()

	ctx := context.Background()
	for authorID, notice := range pending {
		notice.timer.Stop()

		author, err := s.agents.GetByID(ctx, authorID)
		if err != nil {
			continue
		}

		if _, err := s.sendReviewerNotice(ctx, *author); err != nil {
			continue
		}
	}
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
	if err := s.requireOwnMessage(ctx, params.ID, params.Author); err != nil {
		return nil, err
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

// SetReviewFileViewedParams names one file of a round to mark read or unread.
type SetReviewFileViewedParams struct {
	RoundID  string
	FilePath string
	Viewed   bool
	Author   ReviewAuthor
}

// SetReviewFileViewed marks one file of a round read or drops the mark, which only the person
// reviewing may do.
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

// ListReviewViewedFilesParams names the round whose read files are wanted.
type ListReviewViewedFilesParams struct {
	RoundID string
	Author  ReviewAuthor
}

// ListReviewViewedFiles is every file of a round the person reviewing has marked read, which is
// theirs alone.
func (s *ReviewService) ListReviewViewedFiles(
	ctx context.Context,
	params ListReviewViewedFilesParams,
) ([]reviewstore.ViewedFile, error) {
	if !params.Author.IsHuman() {
		return nil, fmt.Errorf("%w: reading which files have been viewed", ErrReviewHumanOnly)
	}

	return s.viewed.ListViewedFiles(ctx, params.RoundID)
}

// interrupt stops the agent where it stands, and failing to is no reason to withhold the
// notification.
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
