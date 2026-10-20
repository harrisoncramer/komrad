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

// ErrReviewNoSnapshot is answered when a review is asked for on a task no snapshot has been taken of.
var ErrReviewNoSnapshot = fail.Precondition("no snapshot has been taken of that task yet")

// errReviewUnnamedTask is answered when a person calls without naming a task.
var errReviewUnnamedTask = fail.InvalidArgument("pass task, by id or by slug")

// errReviewNoTask is answered when an agent on no task calls without naming one.
var errReviewNoTask = fail.Precondition("you work on no task, so pass task, by id or by slug")

// errReviewWorktreeRemoved is answered when a snapshot is asked for on a task whose worktree is gone.
var errReviewWorktreeRemoved = fail.Precondition("that task's worktree was removed, so there is nothing to take a snapshot of")

// errReviewNoSnapshotDir is answered when a task's worktree was removed and its workflow has no directory to read its snapshots from.
var errReviewNoSnapshotDir = fail.Precondition("that task's worktree was removed and its workflow has no directory, so its snapshots cannot be read")

// ErrReviewNotEntitled is answered when an agent asks to review a task it does not work on.
var ErrReviewNotEntitled = fail.Denied("an agent may only review the task it works on")

// ErrReviewOwnWork is answered when an agent asks to review its own work.
var ErrReviewOwnWork = fail.Denied("an agent cannot review its own work")

// ErrReviewAlreadyGiven is answered when an agent asks to review a snapshot it has reviewed once.
var ErrReviewAlreadyGiven = fail.Precondition("that snapshot has already been reviewed; wait for the agent to write something")

// errReviewInFlight is answered when a reviewing agent writes a comment on one snapshot while
// holding unsent comments on another.
var errReviewInFlight = fail.Precondition("you have unsent comments on an earlier snapshot")

// ErrReviewVerdictFromPerson is answered when a person sends a verdict.
var ErrReviewVerdictFromPerson = fail.InvalidArgument("a person's review carries comments and no verdict")

// errReviewVerdictMissing is answered when a reviewing agent releases a review without saying
// what it concluded.
var errReviewVerdictMissing = fail.InvalidArgument("a review from an agent needs a verdict")

// ErrReviewNotCurrent is answered when a comment's line numbers were read from a commit no
// snapshot of that task froze.
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

// TakeSnapshot freezes the checkout of a task now and records it as the task's next snapshot.
func (s *ReviewService) TakeSnapshot(ctx context.Context, ref string, author ReviewAuthor) (*reviewstore.Snapshot, error) {
	task, err := s.getTask(ctx, ref, author)
	if err != nil {
		return nil, err
	}
	if err := s.requireMayRead(ctx, *task, author); err != nil {
		return nil, err
	}

	return s.takeTaskSnapshot(ctx, *task)
}

// takeTaskSnapshot freezes a task's checkout, handing back the newest snapshot when the tree has not moved since it was taken.
func (s *ReviewService) takeTaskSnapshot(ctx context.Context, task taskstore.Task) (*reviewstore.Snapshot, error) {
	if task.Checkout.WorktreeRemoved {
		return nil, fmt.Errorf("%w: %q", errReviewWorktreeRemoved, task.Slug)
	}
	if task.Checkout.Cwd == "" {
		return nil, fmt.Errorf("%w: %q has no directory to take a snapshot of", ErrReviewNoSnapshot, task.Slug)
	}

	unlock := s.snapshotLocks.lock(task.ID)
	defer unlock()

	frozen, err := s.freezeCheckout(ctx, task)
	if err != nil {
		return nil, err
	}

	// A tree that has not moved is the snapshot that was already taken of it, so opening the diff
	// twice without the agent writing anything does not walk the ordinal on.
	latest, err := s.reviews.GetLatestSnapshot(ctx, task.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if latest != nil && latest.SHA == frozen.SHA && latest.BaseSHA == frozen.BaseSHA {
		return latest, nil
	}

	ref := reviewstore.SnapshotRef(task.ID, frozen.SHA)
	if err := s.snapshots.HoldRef(ctx, task.Checkout.Cwd, ref, frozen.SHA); err != nil {
		return nil, err
	}

	snapshot, err := s.reviews.CreateSnapshot(ctx, reviewstore.CreateSnapshotParams{
		TaskID:  task.ID,
		BaseSHA: frozen.BaseSHA,
		SHA:     frozen.SHA,
	})
	if err != nil {
		s.releaseUnheldRef(ctx, task, frozen.SHA)

		return nil, err
	}

	return snapshot, nil
}

// releaseUnheldRef drops a ref taken for a snapshot that was never written, unless an earlier snapshot
// of the same tree already stands on it.
func (s *ReviewService) releaseUnheldRef(ctx context.Context, task taskstore.Task, snapshotSHA string) {
	_, err := s.reviews.GetSnapshotBySHA(ctx, task.ID, snapshotSHA)
	if !errors.Is(err, store.ErrNotFound) {
		return
	}

	if err := s.snapshots.ReleaseRef(ctx, task.Checkout.Cwd, reviewstore.SnapshotRef(task.ID, snapshotSHA)); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to release the ref of a snapshot that was never written",
			append([]slog.Attr{slog.String(logging.TaskID, task.ID)}, logging.Err(err)...)...)
	}
}

// getLatestSnapshot is the newest snapshot of a task, ErrReviewNoSnapshot when none has been taken.
func (s *ReviewService) getLatestSnapshot(ctx context.Context, taskID string) (*reviewstore.Snapshot, error) {
	snapshot, err := s.reviews.GetLatestSnapshot(ctx, taskID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %q", ErrReviewNoSnapshot, taskID)
	}
	if err != nil {
		return nil, err
	}

	return snapshot, nil
}

// ListSnapshots is every snapshot taken of a task, oldest first.
func (s *ReviewService) ListSnapshots(
	ctx context.Context,
	ref string,
	author ReviewAuthor,
) ([]reviewstore.Snapshot, error) {
	task, err := s.getTask(ctx, ref, author)
	if err != nil {
		return nil, err
	}
	if err := s.requireMayRead(ctx, *task, author); err != nil {
		return nil, err
	}

	return s.reviews.ListSnapshots(ctx, task.ID)
}

// ListReviews is every review given on a task, in the order their snapshots were taken,
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

	return s.listReviews(ctx, task.ID, author)
}

func (s *ReviewService) listReviews(ctx context.Context, taskID string, author ReviewAuthor) ([]reviewstore.Review, error) {
	reviews, err := s.reviews.ListReviews(ctx, taskID)
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

// snapshotFor is the snapshot a comment's line numbers were read from: the one the caller names, the
// one holding the commit it read, or the snapshot its own open review sits on.
func (s *ReviewService) snapshotFor(
	ctx context.Context,
	taskID string,
	params CreateReviewThreadParams,
) (*reviewstore.Snapshot, error) {
	if params.SnapshotID != "" {
		return s.reviews.GetSnapshot(ctx, params.SnapshotID)
	}

	if params.SnapshotSHA != "" {
		snapshot, err := s.reviews.GetSnapshotBySHA(ctx, taskID, params.SnapshotSHA)
		if errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s is the commit of no snapshot of this task",
				ErrReviewNotCurrent, params.SnapshotSHA[:min(len(params.SnapshotSHA), 12)])
		}

		return snapshot, err
	}

	open, err := s.reviews.OpenReviewOf(ctx, reviewstore.OpenReviewOfParams{
		TaskID:        taskID,
		AuthorKind:    params.Author.Kind,
		AuthorAgentID: params.Author.AgentID,
	})
	if err == nil {
		return s.reviews.GetSnapshot(ctx, open.SnapshotID)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	return s.getLatestSnapshot(ctx, taskID)
}

// openReviewForParams says whose work an author is writing about, and from where.
type openReviewForParams struct {
	TaskID string
	Author ReviewAuthor
	// Snapshot is the snapshot the author read their line numbers from, nil for a caller with nothing
	// to say about which snapshot that was.
	Snapshot *reviewstore.Snapshot
}

// openReviewFor is the review an author is writing against a task, opened against the snapshot
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

	snapshot := params.Snapshot
	if snapshot == nil {
		snapshot, err = s.getLatestSnapshot(ctx, params.TaskID)
		if err != nil {
			return nil, err
		}
	}

	if err := s.requireNotReviewedAlready(ctx, author, *snapshot); err != nil {
		return nil, err
	}

	return s.reviews.OpenReview(ctx, reviewstore.OpenReviewParams{
		TaskID:        params.TaskID,
		SnapshotID:    snapshot.ID,
		AuthorKind:    params.Author.Kind,
		AuthorAgentID: params.Author.AgentID,
	})
}

// requireNotReviewedAlready holds a reviewing agent to one review per snapshot, so a reviewer can only
// review again once the writer has done more work.
func (s *ReviewService) requireNotReviewedAlready(
	ctx context.Context,
	params reviewstore.OpenReviewOfParams,
	snapshot reviewstore.Snapshot,
) error {
	if params.AuthorKind != reviewstore.AuthorAgent {
		return nil
	}

	reviews, err := s.reviews.ListReviews(ctx, params.TaskID)
	if err != nil {
		return err
	}

	for _, review := range reviews {
		if review.ReleasedAt == nil || review.SnapshotID != snapshot.ID {
			continue
		}
		if review.AuthorAgentID == nil || params.AuthorAgentID == nil {
			continue
		}
		if *review.AuthorAgentID == *params.AuthorAgentID {
			return fmt.Errorf("%w: snapshot %d", ErrReviewAlreadyGiven, snapshot.Ordinal)
		}
	}

	return nil
}

// requireEntitled refuses an author that may not give a review on a task, which the person always
// may, a reviewer only on the task it works on, and any other agent never.
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
	if agent.Role != agentstore.RoleReviewer {
		return ErrReviewOwnWork
	}

	return nil
}

// requireMayRead refuses an author that may not look at a task's snapshots and reviews at all.
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

	snapshot, err := s.reviews.GetSnapshot(ctx, open.SnapshotID)
	if err != nil {
		return nil, err
	}
	dir, err := s.findSnapshotDir(ctx, *task)
	if err != nil {
		return nil, err
	}

	path, err := s.writeReview(ctx, writeReviewParams{
		Task:     *task,
		Snapshot: *snapshot,
		Review:   *open,
		Verdict:  verdict,
		Body:     params.Body,
		Threads:  s.threadsInContext(ctx, dir, threads),
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
			Ordinal:    snapshot.Ordinal,
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
// written about as the snapshot froze it.
type ReviewThreadInContext struct {
	Thread   reviewstore.Thread
	Messages []reviewstore.Message
	// Context is the snapshot around the thread's range, numbered and with the range marked,
	// empty when the snapshot could not be read.
	Context []string
}

// ReviewInFull is one released review in reading order.
type ReviewInFull struct {
	Review   reviewstore.Review
	Snapshot reviewstore.Snapshot
	Threads  []ReviewThreadInContext
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

	snapshot, err := s.reviews.GetSnapshot(ctx, review.SnapshotID)
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

	dir, err := s.findSnapshotDir(ctx, *task)
	if err != nil {
		return nil, err
	}

	return &ReviewInFull{
		Review:   *review,
		Snapshot: *snapshot,
		Threads:  s.threadsInContext(ctx, dir, threads),
	}, nil
}

// writeReviewParams is everything one review file is written from.
type writeReviewParams struct {
	Task     taskstore.Task
	Snapshot reviewstore.Snapshot
	Review   reviewstore.Review
	Verdict  reviewstore.Verdict
	Body     string
	Threads  []ReviewThreadInContext
}

// writeReview puts the whole review under komrad's own state directory rather than in the
// task's worktree, and leaves it there for good.
func (s *ReviewService) writeReview(ctx context.Context, params writeReviewParams) (string, error) {
	dir := filepath.Join(s.stateDir, reviewDirName, params.Task.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to make room for the review of %q: %w", params.Task.Slug, err)
	}

	body := renderReview(renderReviewParams{
		Task:     params.Task,
		Snapshot: params.Snapshot,
		Author:   s.getAuthorName(ctx, params.Review),
		Verdict:  params.Verdict,
		Said:     params.Body,
		Threads:  params.Threads,
	})

	full := filepath.Join(dir, fmt.Sprintf("snapshot-%d-%s.md", params.Snapshot.Ordinal, params.Review.ID))
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
		key := [2]string{thread.Thread.SnapshotID, thread.Thread.FilePath}
		lines, done := held[key]
		if !done && dir != "" {
			lines = s.readSnapshotFile(ctx, readSnapshotFileParams{
				Dir:        dir,
				SnapshotID: thread.Thread.SnapshotID,
				Path:       thread.Thread.FilePath,
				Snapshots:  snapshots,
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

type readSnapshotFileParams struct {
	Dir        string
	SnapshotID string
	Path       string
	Snapshots  map[string]string
}

// readSnapshotFile is a file as one snapshot froze it, nil when the snapshot or the file cannot be read.
func (s *ReviewService) readSnapshotFile(ctx context.Context, params readSnapshotFileParams) []string {
	sha, known := params.Snapshots[params.SnapshotID]
	if !known {
		snapshot, err := s.reviews.GetSnapshot(ctx, params.SnapshotID)
		if err == nil {
			sha = snapshot.SHA
		}

		params.Snapshots[params.SnapshotID] = sha
	}

	if sha == "" {
		return nil
	}

	lines, err := s.snapshots.ReadFileLines(ctx, params.Dir, sha, params.Path)
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

// freezeCheckout freezes a task's checkout against its fork point when it has a parent, else against its workflow's base branch.
func (s *ReviewService) freezeCheckout(ctx context.Context, task taskstore.Task) (*git.Snapshot, error) {
	base := ""
	if flow, err := s.workflows.Get(ctx, task.WorkflowID); err == nil {
		base = flow.Worktree.BaseBranch
	}

	params := git.TakeSnapshotParams{Dir: task.Checkout.Cwd, Base: base}
	if task.Parent != nil {
		params.BaseCommit = task.Parent.ForkPoint
	}

	return s.snapshots.TakeSnapshot(ctx, params)
}
