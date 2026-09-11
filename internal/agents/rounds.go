package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"weave/internal/git"
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
var ErrReviewNoRound = errors.New("no round has been taken of that agent yet")

// ErrReviewNotEntitled is answered when an agent asks to review one it is not attached to.
var ErrReviewNotEntitled = errors.New("an agent may only review the agent it is a child of")

// ErrReviewOwnWork is answered when an agent asks to review its own work.
var ErrReviewOwnWork = errors.New("an agent cannot review its own work")

// ErrReviewAlreadyGiven is answered when an agent asks to review a round it has reviewed once.
var ErrReviewAlreadyGiven = errors.New("that round has already been reviewed; wait for the agent to write something")

// ErrReviewInFlight is answered when somebody writes a comment on one snapshot while holding
// unsent comments on another.
var ErrReviewInFlight = errors.New("you have unsent comments on an earlier snapshot")

// ErrReviewNotAnswered is answered when somebody starts a second review while the agent has yet
// to act on the one they already sent.
var ErrReviewNotAnswered = errors.New("the agent has not answered your last review yet")

// ErrReviewNotCurrent is answered when a comment's line numbers were read from a snapshot other
// than the one of the round it would be filed against.
var ErrReviewNotCurrent = errors.New("those lines were read from an older snapshot")

// ErrReviewEmpty is answered when a review is released with nothing written in it.
var ErrReviewEmpty = errors.New("that review has nothing in it")

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
	Take(ctx context.Context, params git.TakeSnapshotParams) (*git.Snapshot, error)
	Hold(ctx context.Context, dir, ref, sha string) error
	FileAt(ctx context.Context, dir, sha, path string) ([]string, error)
	snapshotReleaser
}

// snapshotReleaser drops the ref holding a round's snapshot.
type snapshotReleaser interface {
	Release(ctx context.Context, dir, ref string) error
}

// GitSnapshotter takes a round's snapshot with git.
type GitSnapshotter struct{}

// Take freezes the worktree as a commit nothing else can reach.
func (GitSnapshotter) Take(ctx context.Context, params git.TakeSnapshotParams) (*git.Snapshot, error) {
	return git.TakeSnapshot(ctx, params)
}

// Hold points a ref at a snapshot so nothing collects it.
func (GitSnapshotter) Hold(ctx context.Context, dir, ref, sha string) error {
	return git.HoldRef(ctx, dir, ref, sha)
}

// FileAt reads one file as the round froze it.
func (GitSnapshotter) FileAt(ctx context.Context, dir, sha, path string) ([]string, error) {
	return git.ReadFileLines(ctx, dir, sha, path)
}

// Release drops the ref a snapshot was held by, leaving it to be collected.
func (GitSnapshotter) Release(ctx context.Context, dir, ref string) error {
	return git.ReleaseRef(ctx, dir, ref)
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
	if agent.Cwd == "" {
		return nil, fmt.Errorf("%w: %q has no directory to take a round of", ErrReviewNoRound, agent.Name)
	}

	base := ""
	if flow, err := s.workflows.Get(ctx, agent.WorkflowID); err == nil {
		base = flow.Worktree.BaseBranch
	}

	snapshot, err := s.snapshots.Take(ctx, git.TakeSnapshotParams{Dir: agent.Cwd, Base: base})
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

	if err := s.snapshots.Hold(ctx, agent.Cwd, roundRef(agentID, snapshot.SHA), snapshot.SHA); err != nil {
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

// ListTallies is how many rounds of review each agent has taken part in, read once for the
// whole listing.
func (s *ReviewService) ListTallies(ctx context.Context) ([]reviewstore.Tally, error) {
	return s.reviews.ListTallies(ctx)
}

// ListLiveReviewers is every reviewer still owed an answer, read once for the whole listing.
func (s *ReviewService) ListLiveReviewers(ctx context.Context) ([]reviewstore.LiveReviewer, error) {
	return s.reviews.ListLiveReviewers(ctx)
}

// ListReviews is every review given on an agent, in the order their rounds were taken, with the
// path of each dropped for anyone but the person reviewing.
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

	for i := range reviews {
		if reviews[i].AuthorAgentID == nil || *reviews[i].AuthorAgentID != *author.AgentID {
			reviews[i].Path = ""
		}
	}

	return reviews, nil
}

// openReviewFor is the review an author is writing against an agent, opened against the newest
// round when they have none open.
func (s *ReviewService) openReviewFor(
	ctx context.Context,
	agentID string,
	author ReviewAuthor,
) (*reviewstore.Review, error) {
	if err := s.requireEntitled(ctx, agentID, author); err != nil {
		return nil, err
	}

	params := reviewstore.AuthorParams{
		AgentID:       agentID,
		AuthorKind:    author.Kind,
		AuthorAgentID: author.AgentID,
	}

	open, err := s.reviews.OpenReviewOf(ctx, params)
	if err == nil {
		return open, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	round, err := s.GetLatestRound(ctx, agentID)
	if err != nil {
		return nil, err
	}

	if err := s.requireNotReviewedAlready(ctx, params, *round); err != nil {
		return nil, err
	}

	if err := s.requirePreviousReviewAnswered(ctx, agentID, params, *round); err != nil {
		return nil, err
	}

	return s.reviews.OpenReview(ctx, reviewstore.OpenReviewParams{
		AgentID:       agentID,
		RoundID:       round.ID,
		AuthorKind:    author.Kind,
		AuthorAgentID: author.AgentID,
	})
}

// requirePreviousReviewAnswered holds a reviewer to one review at a time, refusing a second
// until the agent has written something in answer to the last.
func (s *ReviewService) requirePreviousReviewAnswered(
	ctx context.Context,
	agentID string,
	params reviewstore.AuthorParams,
	round reviewstore.Round,
) error {
	// An agent reviewing is held to one review per round elsewhere, in its own words, so this is
	// the person's rule alone.
	if params.AuthorKind != reviewstore.AuthorHuman {
		return nil
	}

	// Explainer: the danger a second review runs is being about code that predates the agent's
	// answer to the first, so the two arrive as one set of comments describing two versions of
	// the work. The question is whether a round exists that the last review was not against,
	// which is a matter of which round rather than of when, since a snapshot of an unchanged tree
	// gives back the round already taken of it.
	last, err := s.reviews.GetLastReleasedReview(ctx, params)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if last.ReleasedAt == nil || last.RoundID != round.ID {
		return nil
	}

	// An agent nobody can reach will never answer, so a reader is left able to write notes on it
	// rather than locked out by a wait that cannot end.
	agent, err := s.getLiveAgent(ctx, agentID)
	if err != nil || agent == nil {
		return nil
	}

	return fmt.Errorf("%w: it has written nothing since", ErrReviewNotAnswered)
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

	last, err := s.reviews.GetLastReleasedReview(ctx, params)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if last.RoundID == round.ID {
		return fmt.Errorf("%w: round %d", ErrReviewAlreadyGiven, round.Ordinal)
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
		return fmt.Errorf("%w: %q", ErrAgentNotFound, *author.AgentID)
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

		open, err = s.openReviewFor(ctx, params.AgentID, params.Author)
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

	doorbell, err := s.mailer.SendNotice(ctx, SendNoticeParams{
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
		return nil, fmt.Errorf("the review reached %q but weave could not record that it had: %w", agent.Name, err)
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
func (s *ReviewService) writeReview(
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

		lines, err := s.snapshots.FileAt(ctx, params.Dir, params.Round.SnapshotSHA, path)
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
		return nil, fmt.Errorf("%w: %q", ErrAgentNotFound, agentID)
	}
	if err != nil {
		return nil, err
	}
	if agent.ArchivedAt != nil {
		return nil, fmt.Errorf("%w: %q is archived, so there is nobody to hand a review to; restore it first", ErrAgentArchived, agent.Name)
	}

	return agent, nil
}
