package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"weave/internal/fail"
	"weave/internal/git"
	"weave/internal/mail"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	reviewstore "weave/internal/store/review"
)

// reviewDirName is where released reviews are kept under weave's own state directory, rather
// than in the worktree where they would show up in the diff being reviewed.
const reviewDirName = "reviews"

// ErrReviewNoRound is answered when a review is asked for on an agent no round has been taken
// of.
var ErrReviewNoRound = fail.Precondition("no round has been taken of that agent yet")

// ErrReviewNotEntitled is answered when an agent asks to review one it is not attached to.
var ErrReviewNotEntitled = fail.Denied("an agent may only review the agent it is a child of")

// ErrReviewOwnWork is answered when an agent asks to review its own work.
var ErrReviewOwnWork = fail.Denied("an agent cannot review its own work")

// ErrReviewAlreadyGiven is answered when an agent asks to review a round it has reviewed once.
var ErrReviewAlreadyGiven = fail.Precondition("that round has already been reviewed; wait for the agent to write something")

// ErrReviewInFlight is answered when somebody writes a comment on one snapshot while holding
// unsent comments on another.
var ErrReviewInFlight = fail.Precondition("you have unsent comments on an earlier snapshot")

// ErrReviewNotAnswered is a second review started while the last one still has comments nobody
// has settled.
var ErrReviewNotAnswered = fail.Precondition("your last review is still owed an answer")

// ErrReviewNotCurrent is answered when a comment's line numbers were read from a snapshot no
// round of that agent was taken of.
var ErrReviewNotCurrent = fail.Precondition("those lines were read from a snapshot weave never froze")

// ErrReviewEmpty is answered when a review is released with nothing written in it.
var ErrReviewEmpty = fail.Precondition("that review has nothing in it")

// reviewReaper is the half of the review store the lifecycle needs, being the refs an agent's
// snapshots are held by and the drafts it was writing about others.
type reviewReaper interface {
	ListRounds(ctx context.Context, agentID string) ([]reviewstore.Round, error)
	DeleteOpenReviewsBy(ctx context.Context, authorAgentIDs []string) error
}

type reviewRoundReadWriter interface {
	CreateRound(ctx context.Context, params reviewstore.CreateRoundParams) (*reviewstore.Round, error)
	GetRound(ctx context.Context, id string) (*reviewstore.Round, error)
	GetLatestRound(ctx context.Context, agentID string) (*reviewstore.Round, error)
	GetRoundBySnapshot(ctx context.Context, agentID, snapshotSHA string) (*reviewstore.Round, error)
	ListRounds(ctx context.Context, agentID string) ([]reviewstore.Round, error)
}

type reviewBatchReadWriter interface {
	OpenReview(ctx context.Context, params reviewstore.OpenReviewParams) (*reviewstore.Review, error)
	GetReview(ctx context.Context, id string) (*reviewstore.Review, error)
	OpenReviewOf(ctx context.Context, params reviewstore.AuthorParams) (*reviewstore.Review, error)
	GetLastReleasedReview(ctx context.Context, params reviewstore.AuthorParams) (*reviewstore.Review, error)
	ListReviews(ctx context.Context, agentID string) ([]reviewstore.Review, error)
	CountOpenComments(ctx context.Context, reviewID string) (int, error)
	ReleaseReview(ctx context.Context, params reviewstore.ReleaseReviewParams) (*reviewstore.Review, error)
	DeleteReview(ctx context.Context, id string) error
	ListTallies(ctx context.Context) ([]reviewstore.Tally, error)
	liveReviewerLister
}

type reviewSnapshotter interface {
	TakeSnapshot(ctx context.Context, params git.TakeSnapshotParams) (*git.Snapshot, error)
	HoldRef(ctx context.Context, dir, ref, sha string) error
	ReadFileLines(ctx context.Context, dir, sha, path string) ([]string, error)
	snapshotReleaser
}

// snapshotReleaser drops the ref holding a round's snapshot.
type snapshotReleaser interface {
	ReleaseRef(ctx context.Context, dir, ref string) error
}

// roundRef is the ref a snapshot is held by so nothing collects it, named after the agent and
// the commit.
func roundRef(agentID, snapshotSHA string) string {
	return "refs/weave/rounds/" + agentID + "/" + snapshotSHA
}

// OpenRound freezes what an agent has written now and records it as its next round.
func (s *ReviewService) OpenRound(ctx context.Context, agentID string, author ReviewAuthor) (*reviewstore.Round, error) {
	if err := s.requireMayRead(ctx, agentID, author); err != nil {
		return nil, err
	}

	agent, err := s.getLiveAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if agent.ParentAgentID != nil {
		return nil, fmt.Errorf("%w: %q is a child, writing in a checkout it does not own", ErrReviewNeedsOwnWork, agent.Name)
	}
	if agent.Cwd == "" {
		return nil, fmt.Errorf("%w: %q has no directory to take a round of", ErrReviewNoRound, agent.Name)
	}

	base := ""
	if flow, err := s.workflows.Get(ctx, agent.WorkflowID); err == nil {
		base = flow.Worktree.BaseBranch
	}

	snapshot, err := s.snapshots.TakeSnapshot(ctx, git.TakeSnapshotParams{Dir: agent.Cwd, Base: base})
	if err != nil {
		return nil, err
	}

	// A tree that has not moved is the round that was already taken of it, so opening the diff
	// twice without the agent writing anything does not walk the ordinal on.
	latest, err := s.rounds.GetLatestRound(ctx, agentID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if latest != nil && latest.SnapshotSHA == snapshot.SHA && latest.BaseSHA == snapshot.BaseSHA {
		return latest, nil
	}

	if err := s.snapshots.HoldRef(ctx, agent.Cwd, roundRef(agentID, snapshot.SHA), snapshot.SHA); err != nil {
		return nil, err
	}

	return s.rounds.CreateRound(ctx, reviewstore.CreateRoundParams{
		AgentID:     agentID,
		BaseSHA:     snapshot.BaseSHA,
		SnapshotSHA: snapshot.SHA,
	})
}

// GetLatestRound is the newest round of an agent, ErrReviewNoRound when none has been taken.
func (s *ReviewService) GetLatestRound(ctx context.Context, agentID string) (*reviewstore.Round, error) {
	round, err := s.rounds.GetLatestRound(ctx, agentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %q", ErrReviewNoRound, agentID)
	}
	if err != nil {
		return nil, err
	}

	return round, nil
}

// ListRounds is every round taken of an agent, oldest first.
func (s *ReviewService) ListRounds(
	ctx context.Context,
	agentID string,
	author ReviewAuthor,
) ([]reviewstore.Round, error) {
	if err := s.requireMayRead(ctx, agentID, author); err != nil {
		return nil, err
	}

	return s.rounds.ListRounds(ctx, agentID)
}

// ListLiveReviewers is every reviewer still owed an answer, read once for the whole listing.
func (s *ReviewService) ListLiveReviewers(ctx context.Context) ([]reviewstore.LiveReviewer, error) {
	return s.reviews.ListLiveReviewers(ctx)
}

// ListReviews is every review given on an agent, in the order their rounds were taken, keeping
// another author's unreleased review, and the path of their released one, from an agent caller.
func (s *ReviewService) ListReviews(
	ctx context.Context,
	agentID string,
	author ReviewAuthor,
) ([]reviewstore.Review, error) {
	if err := s.requireMayRead(ctx, agentID, author); err != nil {
		return nil, err
	}

	reviews, err := s.reviews.ListReviews(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if author.IsHuman() {
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

// openReviewForParams says whose work an author is writing about, and from where.
type openReviewForParams struct {
	AgentID string
	Author  ReviewAuthor
	// Round is the round the author read their line numbers from, nil for a caller with nothing
	// to say about which round that was.
	Round *reviewstore.Round
	// Reply says the words are going into a thread that already exists.
	Reply bool
}

// roundFor is the round a comment's line numbers were read from: the one the caller names, the
// one holding the snapshot it read, or the round its own open review sits on.
func (s *ReviewService) roundFor(
	ctx context.Context,
	params CreateReviewThreadParams,
) (*reviewstore.Round, error) {
	if params.RoundID != "" {
		return s.rounds.GetRound(ctx, params.RoundID)
	}

	if params.SnapshotSHA != "" {
		round, err := s.rounds.GetRoundBySnapshot(ctx, params.AgentID, params.SnapshotSHA)
		if errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s is the snapshot of no round of this agent",
				ErrReviewNotCurrent, params.SnapshotSHA[:min(len(params.SnapshotSHA), 12)])
		}

		return round, err
	}

	open, err := s.reviews.OpenReviewOf(ctx, reviewstore.AuthorParams{
		AgentID:       params.AgentID,
		AuthorKind:    params.Author.Kind,
		AuthorAgentID: params.Author.AgentID,
	})
	if err == nil {
		return s.rounds.GetRound(ctx, open.RoundID)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	return s.GetLatestRound(ctx, params.AgentID)
}

// openReviewFor is the review an author is writing against an agent, opened against the round
// they read when they have none.
func (s *ReviewService) openReviewFor(
	ctx context.Context,
	params openReviewForParams,
) (*reviewstore.Review, error) {
	if err := s.requireEntitled(ctx, params.AgentID, params.Author); err != nil {
		return nil, err
	}

	author := reviewstore.AuthorParams{
		AgentID:       params.AgentID,
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
		round, err = s.GetLatestRound(ctx, params.AgentID)
		if err != nil {
			return nil, err
		}
	}

	if err := s.requireNotReviewedAlready(ctx, author, *round); err != nil {
		return nil, err
	}

	// Answering in a thread is not a second review, so the wait for the agent to act is not
	// something to hold a reply behind.
	if !params.Reply {
		if err := s.requirePreviousReviewAnswered(ctx, params.AgentID, author); err != nil {
			return nil, err
		}
	}

	return s.reviews.OpenReview(ctx, reviewstore.OpenReviewParams{
		AgentID:       params.AgentID,
		RoundID:       round.ID,
		AuthorKind:    params.Author.Kind,
		AuthorAgentID: params.Author.AgentID,
	})
}

// requirePreviousReviewAnswered holds a reviewer to one review at a time, refusing a second
// until the agent has answered the last.
func (s *ReviewService) requirePreviousReviewAnswered(
	ctx context.Context,
	agentID string,
	params reviewstore.AuthorParams,
) error {
	// An agent reviewing is held to one review per round elsewhere, in its own words, so this is
	// the person's rule alone.
	if params.AuthorKind != reviewstore.AuthorHuman {
		return nil
	}

	last, err := s.reviews.GetLastReleasedReview(ctx, params)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if last.IsSettled() {
		return nil
	}

	// An agent nobody can reach will never answer, so a reader is left able to write notes on it
	// rather than locked out by a wait that cannot end.
	agent, err := s.getLiveAgent(ctx, agentID)
	if err != nil || agent == nil {
		return nil
	}

	return ErrReviewNotAnswered.Saying(
		"your last review has %d comment(s) nobody has settled", last.OpenThreads)
}

// requireNotReviewedAlready holds a reviewing agent to one review per round, so a reviewer and
// its parent can only go round again once the parent has done more work.
func (s *ReviewService) requireNotReviewedAlready(
	ctx context.Context,
	params reviewstore.AuthorParams,
	round reviewstore.Round,
) error {
	if params.AuthorKind != reviewstore.AuthorAgent {
		return nil
	}

	reviews, err := s.reviews.ListReviews(ctx, params.AgentID)
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

// requireEntitled refuses an author that may not give a review on an agent, which the person
// always may, an agent only on the agent it is a child of, and nobody on their own work.
func (s *ReviewService) requireEntitled(
	ctx context.Context,
	agentID string,
	author ReviewAuthor,
) error {
	if !author.IsHuman() && author.AgentID != nil && *author.AgentID == agentID {
		return ErrReviewOwnWork
	}

	return s.requireMayRead(ctx, agentID, author)
}

// requireMayRead refuses an author that may not look at an agent's rounds and reviews at all.
func (s *ReviewService) requireMayRead(
	ctx context.Context,
	agentID string,
	author ReviewAuthor,
) error {
	if author.IsHuman() {
		return nil
	}
	if author.AgentID == nil {
		return fmt.Errorf("%w: %q", ErrReviewNotEntitled, agentID)
	}
	if *author.AgentID == agentID {
		return nil
	}

	reviewer, err := s.agents.GetByID(ctx, *author.AgentID)
	if errors.Is(err, store.ErrNotFound) {
		return ErrAgentNotFound.Saying("no agent with id %q", *author.AgentID)
	}
	if err != nil {
		return err
	}
	if reviewer.ParentAgentID == nil || *reviewer.ParentAgentID != agentID {
		return fmt.Errorf("%w: %q", ErrReviewNotEntitled, agentID)
	}

	return nil
}

// ReleaseReviewParams says whose review to let go of and what it concluded.
type ReleaseReviewParams struct {
	AgentID string
	Verdict reviewstore.Verdict
	// Body is what the reviewer says about the change as a whole rather than about any one line,
	// empty when they said nothing.
	Body string
	// Interrupt stops the agent where it stands before the mail is sent, so it reads the review
	// now rather than at the end of whatever it is doing.
	Interrupt bool
	Author    ReviewAuthor
}

// ReleaseReviewResult reports what letting go of a review did.
type ReleaseReviewResult struct {
	Review reviewstore.Review
	// Comments is how many the released review carried.
	Comments int
	// Path is where the review was written for the agent to read.
	Path string
}

// ReleaseReview releases the author's open review, writing it out as one file and sending one
// piece of ordinary mail saying where that file is.
func (s *ReviewService) ReleaseReview(
	ctx context.Context,
	params ReleaseReviewParams,
) (*ReleaseReviewResult, error) {
	if err := s.requireEntitled(ctx, params.AgentID, params.Author); err != nil {
		return nil, err
	}

	agent, err := s.getLiveAgent(ctx, params.AgentID)
	if err != nil {
		return nil, err
	}

	// A verdict is worth sending on its own, so approving a round nobody wrote a word about opens
	// the batch it goes out under rather than refusing, which leaves notes with no conclusion as
	// the one case where an empty review says nothing at all.
	open, err := s.reviews.OpenReviewOf(ctx, reviewstore.AuthorParams{
		AgentID:       params.AgentID,
		AuthorKind:    params.Author.Kind,
		AuthorAgentID: params.Author.AgentID,
	})
	if errors.Is(err, store.ErrNotFound) {
		if params.Verdict == reviewstore.VerdictCommented {
			return nil, fmt.Errorf("%w: %q", ErrReviewEmpty, params.AgentID)
		}

		open, err = s.openReviewFor(ctx, openReviewForParams{AgentID: params.AgentID, Author: params.Author})
	}
	if err != nil {
		return nil, err
	}

	comments, err := s.reviews.CountOpenComments(ctx, open.ID)
	if err != nil {
		return nil, err
	}
	// Notes with no conclusion have to say something, whether pinned to a line or about the work
	// as a whole.
	if comments == 0 && strings.TrimSpace(params.Body) == "" && params.Verdict == reviewstore.VerdictCommented {
		return nil, fmt.Errorf("%w: %q", ErrReviewEmpty, params.AgentID)
	}

	round, err := s.rounds.GetRound(ctx, open.RoundID)
	if err != nil {
		return nil, err
	}

	threads, err := s.listBatchThreads(ctx, open.ID)
	if err != nil {
		return nil, err
	}

	path, err := s.writeReview(ctx, *agent, *round, *open, params, threads)
	if err != nil {
		return nil, err
	}

	if params.Interrupt {
		s.interrupt(ctx, params.AgentID)
	}

	doorbell, err := s.mailer.SendNotice(ctx, mail.SendNoticeParams{
		ToAgentID: params.AgentID,
		Body:      reviewNotice(round.Ordinal, params.Verdict, comments, path),
		Kind:      mailstore.KindReviewActivity,
	})
	if err != nil {
		return nil, err
	}

	released, err := s.reviews.ReleaseReview(ctx, reviewstore.ReleaseReviewParams{
		ID:             open.ID,
		Verdict:        params.Verdict,
		Body:           params.Body,
		Path:           path,
		DoorbellMailID: &doorbell.ID,
	})
	if err != nil {
		// The mail has gone, so the agent has the review whatever the panel goes on to draw.
		return nil, fmt.Errorf("failed to record that the review reached %q: %w", agent.Name, err)
	}

	return &ReleaseReviewResult{
		Review:   *released,
		Comments: comments,
		Path:     path,
	}, nil
}

// reviewNotice is the whole of what the mail carries: the round, the verdict, how much there is
// and where to read it.
func reviewNotice(ordinal int32, verdict reviewstore.Verdict, comments int, path string) string {
	return fmt.Sprintf("weave: review of round %d, %s, %d comment(s), read %s",
		ordinal, strings.ReplaceAll(string(verdict), "_", " "), comments, path)
}

// listBatchThreads is what one review actually says, gathered by the messages written under it
// rather than by the threads opened under it.
func (s *ReviewService) listBatchThreads(ctx context.Context, reviewID string) ([]ReviewThreadWithMessages, error) {
	review, err := s.reviews.GetReview(ctx, reviewID)
	if err != nil {
		return nil, err
	}

	all, err := s.ListReviewThreads(ctx, ListReviewThreadsParams{
		AgentID: review.AgentID,
		Author:  ReviewAuthor{Kind: reviewstore.AuthorHuman},
	})
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

// writeReview puts the whole review under weave's own state directory rather than in the
// agent's worktree, and leaves it there for good.
func (s *ReviewService) writeReview( //nolint:revive // a params struct is the follow-up
	ctx context.Context,
	agent agentstore.Agent,
	round reviewstore.Round,
	review reviewstore.Review,
	params ReleaseReviewParams,
	threads []ReviewThreadWithMessages,
) (string, error) {
	dir := filepath.Join(s.stateDir, reviewDirName, agent.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to make room for the review of %q: %w", agent.Name, err)
	}

	body := renderReview(renderReviewParams{
		Agent:   agent,
		Round:   round,
		Author:  s.getAuthorName(ctx, review),
		Verdict: params.Verdict,
		Said:    params.Body,
		Threads: threads,
		Source: s.getSnapshotLines(ctx, getSnapshotLinesParams{
			Dir:     agent.Cwd,
			Round:   round,
			Threads: threads,
		}),
	})

	full := filepath.Join(dir, fmt.Sprintf("round-%d-%s.md", round.Ordinal, review.ID))
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		return "", fmt.Errorf("failed to write the review of %q: %w", agent.Name, err)
	}

	return full, nil
}

// getAuthorName is what to call whoever gave a review in the file it is written to, empty for
// the person and the agent's name for an agent.
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

// getSnapshotLinesParams says which round to read the files out of and which of them are wanted.
type getSnapshotLinesParams struct {
	// Dir is anywhere inside the reviewed agent's worktree.
	Dir   string
	Round reviewstore.Round
	// Threads names the files a review has a comment on.
	Threads []ReviewThreadWithMessages
}

// getSnapshotLines reads every file a review touches as the round froze it, leaving out a file
// that cannot be read rather than failing the release.
func (s *ReviewService) getSnapshotLines(ctx context.Context, params getSnapshotLinesParams) map[string][]string {
	if params.Dir == "" {
		return nil
	}

	held := make(map[string][]string, len(params.Threads))
	for _, thread := range params.Threads {
		path := thread.Thread.FilePath
		if path == "" {
			continue
		}
		if _, done := held[path]; done {
			continue
		}

		lines, err := s.snapshots.ReadFileLines(ctx, params.Dir, params.Round.SnapshotSHA, path)
		if err != nil {
			lines = nil
		}

		held[path] = lines
	}

	return held
}

// getLiveAgent refuses an agent that has been archived.
func (s *ReviewService) getLiveAgent(ctx context.Context, agentID string) (*agentstore.Agent, error) {
	agent, err := s.agents.GetByID(ctx, agentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrAgentNotFound.Saying("no agent with id %q", agentID)
	}
	if err != nil {
		return nil, err
	}
	if agent.ArchivedAt != nil {
		return nil, fmt.Errorf("%w: %q is archived, so there is nobody to hand a review to; restore it first", ErrAgentArchived, agent.Name)
	}

	return agent, nil
}
