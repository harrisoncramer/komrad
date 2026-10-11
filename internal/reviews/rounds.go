package reviews

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"komrad/internal/agenterr"
	"komrad/internal/events"
	"komrad/internal/fail"
	"komrad/internal/git"
	"komrad/internal/logging"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	reviewstore "komrad/internal/store/review"
	taskstore "komrad/internal/store/task"
	"komrad/lib/xptr"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

// reviewDirName is where released reviews are kept under komrad's own state directory.
const reviewDirName = "reviews"

// ErrReviewNoRound is answered when a review is asked for on a task no round has been taken of.
var ErrReviewNoRound = fail.Precondition("no round has been taken of that task yet")

// errReviewUnnamedTask is answered when a person calls without naming a task.
var errReviewUnnamedTask = fail.InvalidArgument("pass task, by id or by slug")

// errReviewNoAgent is answered when a person opens a round on a task no agent was ever on.
var errReviewNoAgent = fail.Precondition("that task has never had an agent on it")

// errReviewNoTask is answered when an agent on no task calls without naming one.
var errReviewNoTask = fail.Precondition("you work on no task, so pass task, by id or by slug")

// errReviewWorktreeRemoved is answered when a round is asked for on a task whose worktree is gone.
var errReviewWorktreeRemoved = fail.Precondition("that task's worktree was removed, so there is nothing to take a round of")

// ErrReviewNotEntitled is answered when an agent asks to review a task it does not work on.
var ErrReviewNotEntitled = fail.Denied("an agent may only review the task it works on")

// ErrReviewOwnWork is answered when an agent asks to review its own work.
var ErrReviewOwnWork = fail.Denied("an agent cannot review its own work")

// ErrReviewAlreadyGiven is answered when an agent asks to review a round it has reviewed once.
var ErrReviewAlreadyGiven = fail.Precondition("that round has already been reviewed; wait for the agent to write something")

// errReviewInFlight is answered when a reviewing agent writes a comment on one snapshot while
// holding unsent comments on another.
var errReviewInFlight = fail.Precondition("you have unsent comments on an earlier snapshot")

// ErrReviewVerdictFromPerson is answered when a person sends a verdict.
var ErrReviewVerdictFromPerson = fail.InvalidArgument("a person's review carries comments and no verdict")

// errReviewVerdictMissing is answered when a reviewing agent releases a review without saying
// what it concluded.
var errReviewVerdictMissing = fail.InvalidArgument("a review from an agent needs a verdict")

// ErrReviewNotCurrent is answered when a comment's line numbers were read from a snapshot no
// round of that task was taken of.
var ErrReviewNotCurrent = fail.Precondition("those lines were read from a snapshot komrad never froze")

// ErrReviewEmpty is answered when a review is released with nothing written in it.
var ErrReviewEmpty = fail.Precondition("that review has nothing in it")

// ErrReviewNotFound is answered when a review id names nothing the caller may see.
var ErrReviewNotFound = fail.NotFound("review not found")

type reviewSnapshotter interface {
	TakeSnapshot(ctx context.Context, params git.TakeSnapshotParams) (*git.Snapshot, error)
	HoldRef(ctx context.Context, dir, ref, sha string) error
	ReadFileLines(ctx context.Context, dir, sha, path string) ([]string, error)
	ReleaseRef(ctx context.Context, dir, ref string) error
}

// OpenRound freezes the checkout of a task now and records it as the task's next round.
func (s *ReviewService) OpenRound(ctx context.Context, ref string, author ReviewAuthor) (*reviewstore.Round, error) {
	task, err := s.getTask(ctx, ref, author)
	if err != nil {
		return nil, err
	}
	if err := s.requireMayRead(ctx, *task, author); err != nil {
		return nil, err
	}

	agentID, err := s.getRecordingAgentID(ctx, *task, author)
	if err != nil {
		return nil, err
	}

	return s.openTaskRound(ctx, *task, agentID)
}

// getRecordingAgentID reads the agent a row written on a task is filed under, which is the caller
// when it is an agent and otherwise the newest agent on the task.
func (s *ReviewService) getRecordingAgentID(ctx context.Context, task taskstore.Task, author ReviewAuthor) (string, error) {
	if author.AgentID != nil {
		return *author.AgentID, nil
	}

	agents, err := s.agents.ListOnTasks(ctx, []string{task.ID})
	if err != nil {
		return "", err
	}
	if len(agents) == 0 {
		return "", fmt.Errorf("%w: %q", errReviewNoAgent, task.Slug)
	}

	return agents[len(agents)-1].ID, nil
}

// openTaskRound freezes a task's checkout through one of its agents, handing back the newest
// round when the tree has not moved since it was taken.
func (s *ReviewService) openTaskRound(ctx context.Context, task taskstore.Task, agentID string) (*reviewstore.Round, error) {
	if task.Checkout.WorktreeRemoved {
		return nil, fmt.Errorf("%w: %q", errReviewWorktreeRemoved, task.Slug)
	}
	if task.Checkout.Cwd == "" {
		return nil, fmt.Errorf("%w: %q has no directory to take a round of", ErrReviewNoRound, task.Slug)
	}

	unlock := s.roundLocks.lock(task.ID)
	defer unlock()

	snapshot, err := s.takeSnapshot(ctx, task)
	if err != nil {
		return nil, err
	}

	// A tree that has not moved is the round that was already taken of it, so opening the diff
	// twice without the agent writing anything does not walk the ordinal on.
	latest, err := s.reviews.GetLatestRound(ctx, task.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if latest != nil && latest.SnapshotSHA == snapshot.SHA && latest.BaseSHA == snapshot.BaseSHA {
		return latest, nil
	}

	ref := reviewstore.RoundRef(task.ID, snapshot.SHA)
	if err := s.snapshots.HoldRef(ctx, task.Checkout.Cwd, ref, snapshot.SHA); err != nil {
		return nil, err
	}

	round, err := s.reviews.CreateRound(ctx, reviewstore.CreateRoundParams{
		AgentID:     agentID,
		TaskID:      task.ID,
		BaseSHA:     snapshot.BaseSHA,
		SnapshotSHA: snapshot.SHA,
	})
	if err != nil {
		s.releaseUnheldRef(ctx, task, snapshot.SHA)

		return nil, err
	}

	return round, nil
}

// releaseUnheldRef drops a ref taken for a round that was never written, unless an earlier round
// of the same tree already stands on it.
func (s *ReviewService) releaseUnheldRef(ctx context.Context, task taskstore.Task, snapshotSHA string) {
	_, err := s.reviews.GetRoundBySnapshot(ctx, task.ID, snapshotSHA)
	if !errors.Is(err, store.ErrNotFound) {
		return
	}

	if err := s.snapshots.ReleaseRef(ctx, task.Checkout.Cwd, reviewstore.RoundRef(task.ID, snapshotSHA)); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to release the ref of a round that was never written",
			append([]slog.Attr{slog.String(logging.TaskID, task.ID)}, logging.Err(err)...)...)
	}
}

// getLatestRound is the newest round of a task, ErrReviewNoRound when none has been taken.
func (s *ReviewService) getLatestRound(ctx context.Context, taskID string) (*reviewstore.Round, error) {
	round, err := s.reviews.GetLatestRound(ctx, taskID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %q", ErrReviewNoRound, taskID)
	}
	if err != nil {
		return nil, err
	}

	return round, nil
}

// ListRounds is every round taken of a task, oldest first.
func (s *ReviewService) ListRounds(
	ctx context.Context,
	ref string,
	author ReviewAuthor,
) ([]reviewstore.Round, error) {
	task, err := s.getTask(ctx, ref, author)
	if err != nil {
		return nil, err
	}
	if err := s.requireMayRead(ctx, *task, author); err != nil {
		return nil, err
	}

	return s.reviews.ListRounds(ctx, task.ID)
}

// ListReviews is every review given on a task, in the order their rounds were taken,
// keeping another author's unreleased review, and the path of their released one, from an agent
// caller.
func (s *ReviewService) ListReviews(
	ctx context.Context,
	ref string,
	author ReviewAuthor,
) ([]reviewstore.Review, error) {
	task, err := s.getTask(ctx, ref, author)
	if err != nil {
		return nil, err
	}
	if err := s.requireMayRead(ctx, *task, author); err != nil {
		return nil, err
	}

	reviews, err := s.reviews.ListReviews(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	if author.isHuman() {
		return reviews, nil
	}

	kept := make([]reviewstore.Review, 0, len(reviews))
	for _, review := range reviews {
		if doesOwnReviewBatch(author, review) {
			kept = append(kept, review)

			continue
		}

		if review.ReleasedAt == nil {
			continue
		}

		review.Path = ""
		kept = append(kept, review)
	}

	return kept, nil
}

// doesOwnReviewBatch reports whether the author is the agent that opened the review.
func doesOwnReviewBatch(author ReviewAuthor, review reviewstore.Review) bool {
	return author.AgentID != nil &&
		review.AuthorAgentID != nil &&
		*review.AuthorAgentID == *author.AgentID
}

// roundFor is the round a comment's line numbers were read from: the one the caller names, the
// one holding the snapshot it read, or the round its own open review sits on.
func (s *ReviewService) roundFor(
	ctx context.Context,
	taskID string,
	params CreateReviewThreadParams,
) (*reviewstore.Round, error) {
	if params.RoundID != "" {
		return s.reviews.GetRound(ctx, params.RoundID)
	}

	if params.SnapshotSHA != "" {
		round, err := s.reviews.GetRoundBySnapshot(ctx, taskID, params.SnapshotSHA)
		if errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s is the snapshot of no round of this task",
				ErrReviewNotCurrent, params.SnapshotSHA[:min(len(params.SnapshotSHA), 12)])
		}

		return round, err
	}

	open, err := s.reviews.OpenReviewOf(ctx, reviewstore.OpenReviewOfParams{
		TaskID:        taskID,
		AuthorKind:    params.Author.Kind,
		AuthorAgentID: params.Author.AgentID,
	})
	if err == nil {
		return s.reviews.GetRound(ctx, open.RoundID)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	return s.getLatestRound(ctx, taskID)
}

// openReviewForParams says whose work an author is writing about, and from where.
type openReviewForParams struct {
	TaskID string
	Author ReviewAuthor
	// Round is the round the author read their line numbers from, nil for a caller with nothing
	// to say about which round that was.
	Round *reviewstore.Round
}

// openReviewFor is the review an author is writing against a task, opened against the round
// they read when they have none.
func (s *ReviewService) openReviewFor(
	ctx context.Context,
	params openReviewForParams,
) (*reviewstore.Review, error) {
	author := reviewstore.OpenReviewOfParams{
		TaskID:        params.TaskID,
		AuthorKind:    params.Author.Kind,
		AuthorAgentID: params.Author.AgentID,
	}

	open, err := s.reviews.OpenReviewOf(ctx, author)
	if err == nil {
		return open, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	round := params.Round
	if round == nil {
		round, err = s.getLatestRound(ctx, params.TaskID)
		if err != nil {
			return nil, err
		}
	}

	if err := s.requireNotReviewedAlready(ctx, author, *round); err != nil {
		return nil, err
	}

	return s.reviews.OpenReview(ctx, reviewstore.OpenReviewParams{
		TaskID:        params.TaskID,
		RoundID:       round.ID,
		AuthorKind:    params.Author.Kind,
		AuthorAgentID: params.Author.AgentID,
	})
}

// requireNotReviewedAlready holds a reviewing agent to one review per round, so a reviewer and
// its parent can only go round again once the parent has done more work.
func (s *ReviewService) requireNotReviewedAlready(
	ctx context.Context,
	params reviewstore.OpenReviewOfParams,
	round reviewstore.Round,
) error {
	if params.AuthorKind != reviewstore.AuthorAgent {
		return nil
	}

	reviews, err := s.reviews.ListReviews(ctx, params.TaskID)
	if err != nil {
		return err
	}

	for _, review := range reviews {
		if review.ReleasedAt == nil || review.RoundID != round.ID {
			continue
		}
		if review.AuthorAgentID == nil || params.AuthorAgentID == nil {
			continue
		}
		if *review.AuthorAgentID == *params.AuthorAgentID {
			return fmt.Errorf("%w: round %d", ErrReviewAlreadyGiven, round.Ordinal)
		}
	}

	return nil
}

// requireEntitled refuses an author that may not give a review on a task, which the person always
// may, a child agent only on the task it works on, and a top-level agent never.
func (s *ReviewService) requireEntitled(
	ctx context.Context,
	task taskstore.Task,
	author ReviewAuthor,
) error {
	if author.isHuman() {
		return nil
	}

	agent, err := s.getAuthorOnTask(ctx, task, author)
	if err != nil {
		return err
	}
	if agent.ParentAgentID == nil {
		return ErrReviewOwnWork
	}

	return nil
}

// requireMayRead refuses an author that may not look at a task's rounds and reviews at all.
func (s *ReviewService) requireMayRead(
	ctx context.Context,
	task taskstore.Task,
	author ReviewAuthor,
) error {
	if author.isHuman() {
		return nil
	}

	_, err := s.getAuthorOnTask(ctx, task, author)

	return err
}

// getAuthorOnTask reads the agent behind an author, refusing one that works on another task.
func (s *ReviewService) getAuthorOnTask(
	ctx context.Context,
	task taskstore.Task,
	author ReviewAuthor,
) (*agentstore.Agent, error) {
	if author.AgentID == nil {
		return nil, fmt.Errorf("%w: %q", ErrReviewNotEntitled, task.Slug)
	}

	agent, err := s.getAgent(ctx, *author.AgentID)
	if err != nil {
		return nil, err
	}
	if agent.TaskID == nil || *agent.TaskID != task.ID {
		return nil, fmt.Errorf("%w: %q", ErrReviewNotEntitled, task.Slug)
	}

	return agent, nil
}

// getTask reads the task a call names, by id or by slug, or the caller's own task when it names
// none.
func (s *ReviewService) getTask(ctx context.Context, ref string, author ReviewAuthor) (*taskstore.Task, error) {
	if ref != "" {
		return s.taskFinder.GetTask(ctx, ref)
	}
	if author.AgentID == nil {
		return nil, errReviewUnnamedTask
	}

	agent, err := s.getAgent(ctx, *author.AgentID)
	if err != nil {
		return nil, err
	}

	task, err := s.tasks.GetLinked(ctx, agent.TaskID)
	if errors.Is(err, taskstore.ErrNoTask) {
		return nil, errReviewNoTask
	}
	if err != nil {
		return nil, err
	}

	return task, nil
}

// isTaskWriter reports whether an author is the live writer of a task.
func (s *ReviewService) isTaskWriter(ctx context.Context, author ReviewAuthor, taskID string) (bool, error) {
	if author.isHuman() || author.AgentID == nil {
		return false, nil
	}

	writer, err := s.agents.GetTaskWriter(ctx, taskID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return writer.ID == *author.AgentID, nil
}

// ReleaseReviewResult reports what letting go of a review did.
type ReleaseReviewResult struct {
	Review reviewstore.Review
	// Comments is how many the released review carried.
	Comments int
	// Path is where the review was written for the agent to read.
	Path string
}

// ReleaseReviewParams says whose review to let go of and what it concluded.
type ReleaseReviewParams struct {
	// Task is the task the review is of, by id or by slug, empty for the caller's own.
	Task string
	// Verdict is what a reviewing agent concluded, empty from a person, who sends comments alone.
	Verdict reviewstore.Verdict
	// Body is what the reviewer says about the change as a whole rather than about any one line,
	// empty when they said nothing.
	Body   string
	Author ReviewAuthor
}

// ReleaseReview releases the author's open review, writing it out as one file and sending one
// piece of ordinary mail naming the review.
func (s *ReviewService) ReleaseReview(
	ctx context.Context,
	params ReleaseReviewParams,
) (*ReleaseReviewResult, error) {
	task, err := s.getTask(ctx, params.Task, params.Author)
	if err != nil {
		return nil, err
	}
	if err := s.requireEntitled(ctx, *task, params.Author); err != nil {
		return nil, err
	}

	verdict, err := resolveVerdict(params)
	if err != nil {
		return nil, err
	}

	// A release opens a batch unless it is an agent's comment-only review with nothing in it.
	open, err := s.reviews.OpenReviewOf(ctx, reviewstore.OpenReviewOfParams{
		TaskID:        task.ID,
		AuthorKind:    params.Author.Kind,
		AuthorAgentID: params.Author.AgentID,
	})
	if errors.Is(err, store.ErrNotFound) {
		if isSayingNothing(params.Author, verdict) {
			return nil, fmt.Errorf("%w: %q", ErrReviewEmpty, task.Slug)
		}

		open, err = s.openReviewFor(ctx, openReviewForParams{TaskID: task.ID, Author: params.Author})
	}
	if err != nil {
		return nil, err
	}

	comments, err := s.reviews.CountOpenComments(ctx, open.ID)
	if err != nil {
		return nil, err
	}

	threads, err := s.listBatchThreads(ctx, open.ID)
	if err != nil {
		return nil, err
	}

	tally := tallyBatch(open.ID, threads)
	// An agent's notes with no conclusion have to say something, whether pinned to a line, a
	// reply in an earlier thread, or about the work as a whole.
	if comments == 0 && tally.Replies == 0 && strings.TrimSpace(params.Body) == "" && isSayingNothing(params.Author, verdict) {
		return nil, fmt.Errorf("%w: %q", ErrReviewEmpty, task.Slug)
	}

	round, err := s.reviews.GetRound(ctx, open.RoundID)
	if err != nil {
		return nil, err
	}

	path, err := s.writeReview(ctx, writeReviewParams{
		Task:    *task,
		Round:   *round,
		Review:  *open,
		Verdict: verdict,
		Body:    params.Body,
		Threads: s.threadsInContext(ctx, task.Checkout.Cwd, threads),
	})
	if err != nil {
		return nil, err
	}

	// A person's review stops the task's writer so it reads the comments now.
	if params.Author.isHuman() {
		s.interruptWriter(ctx, task.ID)
	}

	var released *reviewstore.ReleasedReview
	err = s.events.InsertManyTx(ctx, func(tx pgx.Tx) ([]river.JobArgs, error) {
		var err error
		released, err = reviewstore.NewReviewStore(tx).ReleaseReview(ctx, reviewstore.ReleaseReviewParams{
			ID:      open.ID,
			Verdict: verdict,
			Body:    params.Body,
			Path:    path,
		})
		if err != nil {
			return nil, err
		}

		event, err := events.NewEvent(Released{
			ReviewID:   open.ID,
			TaskID:     task.ID,
			Ordinal:    round.Ordinal,
			ByPerson:   params.Author.isHuman(),
			ReviewerID: xptr.Deref(params.Author.AgentID),
			Verdict:    verdict,
			Comments:   tally.Comments,
			Replies:    tally.Replies,
			Reopens:    released.Reopens,
			Path:       path,
			HasBody:    strings.TrimSpace(params.Body) != "",
		})
		if err != nil {
			return nil, err
		}

		return []river.JobArgs{*event}, nil
	})
	if err != nil {
		return nil, err
	}

	return &ReleaseReviewResult{
		Review:   released.Review,
		Comments: comments,
		Path:     path,
	}, nil
}

// releaseMail is the whole of what the mail carries: the round, the verdict, how much there is
// and how to read it.
func releaseMail(params Released) string {
	said := fmt.Sprintf("%d comment(s)", params.Comments)
	if params.Replies > 0 {
		said += fmt.Sprintf(", %d reply(ies) in earlier threads", params.Replies)
	}
	if params.Reopens > 0 {
		said += fmt.Sprintf(", %d of them reopening a comment you had resolved", params.Reopens)
	}

	if params.ForgeLogin != "" {
		return fmt.Sprintf("komrad: %s from @%s on the forge, on round %d. "+
			"They are not from the person who runs you. "+
			"Call get_review with review id %s and answer each thread with add_review_message. Never post to the forge.",
			said, params.ForgeLogin, params.Ordinal, params.ReviewID)
	}

	if !params.ByPerson {
		said = strings.ReplaceAll(string(params.Verdict), "_", " ") + ", " + said
	}

	if params.Verdict == reviewstore.VerdictApproved && params.Comments == 0 && params.Replies == 0 && !params.HasBody {
		return fmt.Sprintf("komrad: review of round %d released: %s. There is nothing to read or answer.", params.Ordinal, said)
	}

	return fmt.Sprintf("komrad: review of round %d released: %s. "+
		"Call get_review with review id %s, or read %s, and answer each thread with add_review_message.",
		params.Ordinal, said, params.ReviewID, params.Path)
}

// resolveVerdict is the verdict a release is stored with, refusing one from a person and
// requiring one from an agent.
func resolveVerdict(params ReleaseReviewParams) (reviewstore.Verdict, error) {
	switch {
	case params.Author.isHuman() && params.Verdict != "":
		return "", ErrReviewVerdictFromPerson
	case params.Author.isHuman():
		return reviewstore.VerdictCommented, nil
	case params.Verdict == "":
		return "", errReviewVerdictMissing
	}

	return params.Verdict, nil
}

// isSayingNothing reports whether an empty release would be refused, which is true only of an
// agent releasing as commented.
func isSayingNothing(author ReviewAuthor, verdict reviewstore.Verdict) bool {
	return !author.isHuman() && verdict == reviewstore.VerdictCommented
}

// batchTally is what one batch said, split by whether it opened a thread or added to one.
type batchTally struct {
	// Comments is the threads the batch opened.
	Comments int
	// Replies is the threads opened earlier that the batch said more in.
	Replies int
}

// tallyBatch counts the threads a batch opened and the replies it carries into earlier ones.
func tallyBatch(reviewID string, threads []ReviewThreadWithMessages) batchTally {
	var tally batchTally
	for _, thread := range threads {
		if thread.Thread.ReviewID == reviewID {
			tally.Comments++

			continue
		}

		tally.Replies++
	}

	return tally
}

// listBatchThreads is what one review actually says, gathered by the messages written under it
// rather than by the threads opened under it.
func (s *ReviewService) listBatchThreads(ctx context.Context, reviewID string) ([]ReviewThreadWithMessages, error) {
	review, err := s.reviews.GetReview(ctx, reviewID)
	if err != nil {
		return nil, err
	}

	all, err := s.listThreads(ctx, review.TaskID, ReviewAuthor{Kind: reviewstore.AuthorHuman})
	if err != nil {
		return nil, err
	}

	mine := make([]ReviewThreadWithMessages, 0, len(all))
	for _, thread := range all {
		said := make([]reviewstore.Message, 0, len(thread.Messages))
		for _, message := range thread.Messages {
			if message.ReviewID != nil && *message.ReviewID == reviewID {
				said = append(said, message)
			}
		}

		if len(said) == 0 {
			continue
		}

		mine = append(mine, ReviewThreadWithMessages{Thread: thread.Thread, Messages: said})
	}

	return mine, nil
}

// ReviewThreadInContext is a thread, what was said in it under one review, and the code it was
// written about as the round froze it.
type ReviewThreadInContext struct {
	Thread   reviewstore.Thread
	Messages []reviewstore.Message
	// Context is the snapshot around the thread's range, numbered and with the range marked,
	// empty when the snapshot could not be read.
	Context []string
}

// ReviewInFull is one released review in reading order.
type ReviewInFull struct {
	Review  reviewstore.Review
	Round   reviewstore.Round
	Threads []ReviewThreadInContext
}

// GetReview is one released review whole, which the agents on its task and the person may read,
// and which its author alone may read before it is released.
func (s *ReviewService) GetReview(ctx context.Context, reviewID string, author ReviewAuthor) (*ReviewInFull, error) {
	review, err := s.reviews.GetReview(ctx, reviewID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %q", ErrReviewNotFound, reviewID)
	}
	if err != nil {
		return nil, err
	}

	// An unreleased review is not found before it is refused.
	if review.ReleasedAt == nil && !author.isHuman() && !doesOwnReviewBatch(author, *review) {
		return nil, fmt.Errorf("%w: %q", ErrReviewNotFound, reviewID)
	}
	task, err := s.tasks.Get(ctx, review.TaskID)
	if err != nil {
		return nil, err
	}
	if err := s.requireMayRead(ctx, *task, author); err != nil {
		return nil, err
	}
	if !author.isHuman() && !doesOwnReviewBatch(author, *review) {
		review.Path = ""
	}

	round, err := s.reviews.GetRound(ctx, review.RoundID)
	if err != nil {
		return nil, err
	}

	threads, err := s.listBatchThreads(ctx, review.ID)
	if err != nil {
		return nil, err
	}
	if !author.isHuman() {
		readable := make([]ReviewThreadWithMessages, 0, len(threads))
		for _, thread := range threads {
			thread.Messages = releasedMessages(thread.Messages, author)
			if len(thread.Messages) > 0 {
				readable = append(readable, thread)
			}
		}
		threads = readable
	}

	return &ReviewInFull{
		Review:  *review,
		Round:   *round,
		Threads: s.threadsInContext(ctx, task.Checkout.Cwd, threads),
	}, nil
}

// writeReviewParams is everything one review file is written from.
type writeReviewParams struct {
	Task    taskstore.Task
	Round   reviewstore.Round
	Review  reviewstore.Review
	Verdict reviewstore.Verdict
	Body    string
	Threads []ReviewThreadInContext
}

// writeReview puts the whole review under komrad's own state directory rather than in the
// task's worktree, and leaves it there for good.
func (s *ReviewService) writeReview(ctx context.Context, params writeReviewParams) (string, error) {
	dir := filepath.Join(s.stateDir, reviewDirName, params.Task.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to make room for the review of %q: %w", params.Task.Slug, err)
	}

	body := renderReview(renderReviewParams{
		Task:    params.Task,
		Round:   params.Round,
		Author:  s.getAuthorName(ctx, params.Review),
		Verdict: params.Verdict,
		Said:    params.Body,
		Threads: params.Threads,
	})

	full := filepath.Join(dir, fmt.Sprintf("round-%d-%s.md", params.Round.Ordinal, params.Review.ID))
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		return "", fmt.Errorf("failed to write the review of %q: %w", params.Task.Slug, err)
	}

	return full, nil
}

// getAuthorName is what to call whoever gave a review, empty for the person and the agent's
// name for an agent.
func (s *ReviewService) getAuthorName(ctx context.Context, review reviewstore.Review) string {
	if review.AuthorAgentID == nil {
		return ""
	}

	author, err := s.agents.GetByID(ctx, *review.AuthorAgentID)
	if err != nil {
		return *review.AuthorAgentID
	}

	return author.Name
}

// threadsInContext draws the code around each thread out of the snapshot it was written against.
func (s *ReviewService) threadsInContext(
	ctx context.Context,
	dir string,
	threads []ReviewThreadWithMessages,
) []ReviewThreadInContext {
	snapshots := map[string]string{}
	held := map[[2]string][]string{}
	drawn := make([]ReviewThreadInContext, 0, len(threads))
	for _, thread := range threads {
		key := [2]string{thread.Thread.RoundID, thread.Thread.FilePath}
		lines, done := held[key]
		if !done && dir != "" {
			lines = s.readRoundFile(ctx, readRoundFileParams{
				Dir:       dir,
				RoundID:   thread.Thread.RoundID,
				Path:      thread.Thread.FilePath,
				Snapshots: snapshots,
			})
			held[key] = lines
		}

		drawn = append(drawn, ReviewThreadInContext{
			Thread:   thread.Thread,
			Messages: thread.Messages,
			Context:  contextOf(thread.Thread, lines),
		})
	}

	return drawn
}

type readRoundFileParams struct {
	Dir       string
	RoundID   string
	Path      string
	Snapshots map[string]string
}

// readRoundFile is a file as one round froze it, nil when the round or the file cannot be read.
func (s *ReviewService) readRoundFile(ctx context.Context, params readRoundFileParams) []string {
	snapshot, known := params.Snapshots[params.RoundID]
	if !known {
		round, err := s.reviews.GetRound(ctx, params.RoundID)
		if err == nil {
			snapshot = round.SnapshotSHA
		}

		params.Snapshots[params.RoundID] = snapshot
	}

	if snapshot == "" {
		return nil
	}

	lines, err := s.snapshots.ReadFileLines(ctx, params.Dir, snapshot, params.Path)
	if err != nil {
		return nil
	}

	return lines
}

// getAgent reads an agent, archived or not, ErrAgentNotFound for an id that names none.
func (s *ReviewService) getAgent(ctx context.Context, agentID string) (*agentstore.Agent, error) {
	agent, err := s.agents.GetByID(ctx, agentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, agenterr.ErrAgentNotFound.Saying("no agent with id %q", agentID)
	}
	if err != nil {
		return nil, err
	}

	return agent, nil
}

// interruptWriter stops a task's live writer where it stands, doing nothing when it has none.
func (s *ReviewService) interruptWriter(ctx context.Context, taskID string) {
	writer, err := s.agents.GetTaskWriter(ctx, taskID)
	if err != nil || !writer.Alive {
		return
	}

	s.interrupt(ctx, writer.ID)
}

// takeSnapshot freezes a task's checkout against the base branch its workflow cuts from.
func (s *ReviewService) takeSnapshot(ctx context.Context, task taskstore.Task) (*git.Snapshot, error) {
	base := ""
	if flow, err := s.workflows.Get(ctx, task.WorkflowID); err == nil {
		base = flow.Worktree.BaseBranch
	}

	return s.snapshots.TakeSnapshot(ctx, git.TakeSnapshotParams{Dir: task.Checkout.Cwd, Base: base})
}
