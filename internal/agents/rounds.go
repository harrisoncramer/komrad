package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"weave/internal/diff"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	reviewstore "weave/internal/store/review"
)

// reviewDirName is where released reviews are kept under weave's own state directory, rather than
// beside the agent's work, since a linked worktree's own info/exclude is not read by git and a
// file put in the worktree would show up in the diff being reviewed.
const reviewDirName = "reviews"

// ErrReviewNoRound is answered when a review is asked for on an agent no round has been taken
// of, which is what happens before the diff has been opened once.
var ErrReviewNoRound = errors.New("no round has been taken of that agent yet")

// ErrReviewNotEntitled is answered when an agent asks to review one it is not attached to.
var ErrReviewNotEntitled = errors.New("an agent may only review the agent it is a child of")

// ErrReviewOwnWork is answered when an agent asks to review its own work, which is not what
// answering the review it was given is.
var ErrReviewOwnWork = errors.New("an agent cannot review its own work")

// ErrReviewAlreadyGiven is answered when an agent asks to review a round it has reviewed once,
// which is what keeps a reviewer and the agent it reviews from answering each other forever.
var ErrReviewAlreadyGiven = errors.New("that round has already been reviewed; wait for the agent to write something")

// ErrReviewEmpty is answered when a review is released with nothing written in it.
var ErrReviewEmpty = errors.New("that review has nothing in it")

// reviewReaper is the half of the review store the lifecycle needs, which is enough to find the
// refs an agent's snapshots are held by and to drop the drafts it was writing about others.
type reviewReaper interface {
	ListRounds(ctx context.Context, agentID string) ([]reviewstore.Round, error)
	DeleteOpenReviewsBy(ctx context.Context, authorAgentIDs []string) error
}

type reviewRoundReadWriter interface {
	CreateRound(ctx context.Context, params reviewstore.CreateRoundParams) (*reviewstore.Round, error)
	GetRound(ctx context.Context, id string) (*reviewstore.Round, error)
	LatestRound(ctx context.Context, agentID string) (*reviewstore.Round, error)
	ListRounds(ctx context.Context, agentID string) ([]reviewstore.Round, error)
}

type reviewBatchReadWriter interface {
	OpenReview(ctx context.Context, params reviewstore.OpenReviewParams) (*reviewstore.Review, error)
	GetReview(ctx context.Context, id string) (*reviewstore.Review, error)
	OpenReviewOf(ctx context.Context, params reviewstore.AuthorParams) (*reviewstore.Review, error)
	LastReleasedReviewOf(ctx context.Context, params reviewstore.AuthorParams) (*reviewstore.Review, error)
	ListReviews(ctx context.Context, agentID string) ([]reviewstore.Review, error)
	CountOpenComments(ctx context.Context, reviewID string) (int, error)
	ReleaseReview(ctx context.Context, params reviewstore.ReleaseReviewParams) (*reviewstore.Review, error)
	DeleteReview(ctx context.Context, id string) error
}

type reviewSnapshotter interface {
	Take(ctx context.Context, params diff.SnapshotParams) (*diff.Snapshot, error)
	Hold(ctx context.Context, dir, ref, sha string) error
	FileAt(ctx context.Context, dir, sha, path string) ([]string, error)
}

// GitSnapshotter takes a round's snapshot with git, which is the only implementation outside
// the tests.
type GitSnapshotter struct{}

// Take freezes the worktree as a commit nothing else can reach.
func (GitSnapshotter) Take(ctx context.Context, params diff.SnapshotParams) (*diff.Snapshot, error) {
	return diff.Take(ctx, params)
}

// Hold points a ref at a snapshot so nothing collects it.
func (GitSnapshotter) Hold(ctx context.Context, dir, ref, sha string) error {
	return diff.Hold(ctx, dir, ref, sha)
}

// FileAt reads one file as the round froze it.
func (GitSnapshotter) FileAt(ctx context.Context, dir, sha, path string) ([]string, error) {
	return diff.FileAt(ctx, dir, sha, path)
}

// RoundRef is the ref a snapshot is held by so nothing collects it, named after the agent and the
// commit so that taking it is idempotent and needs no row to exist first, and so that two agents
// whose worktrees hash the same hold their rounds separately rather than sharing one ref that
// either of them deleting would pull out from under the other.
func RoundRef(agentID, snapshotSHA string) string {
	return "refs/weave/rounds/" + agentID + "/" + snapshotSHA
}

// OpenRound freezes what an agent has written now and records it as its next round, which is what
// every review is given against, so that nothing a comment is pinned to can move underneath the
// person reading it.
func (s *ReviewService) OpenRound(ctx context.Context, agentID string, author ReviewAuthor) (*reviewstore.Round, error) {
	if err := s.mayRead(ctx, agentID, author); err != nil {
		return nil, err
	}

	agent, err := s.liveAgent(ctx, agentID)
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

	snapshot, err := s.snapshots.Take(ctx, diff.SnapshotParams{Dir: agent.Cwd, Base: base})
	if err != nil {
		return nil, err
	}

	// A tree that has not moved is the round that was already taken of it, so opening the diff
	// twice without the agent writing anything does not walk the ordinal on.
	latest, err := s.rounds.LatestRound(ctx, agentID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if latest != nil && latest.SnapshotSHA == snapshot.SHA && latest.BaseSHA == snapshot.BaseSHA {
		return latest, nil
	}

	if err := s.snapshots.Hold(ctx, agent.Cwd, RoundRef(agentID, snapshot.SHA), snapshot.SHA); err != nil {
		return nil, err
	}

	return s.rounds.CreateRound(ctx, reviewstore.CreateRoundParams{
		AgentID:     agentID,
		BaseSHA:     snapshot.BaseSHA,
		SnapshotSHA: snapshot.SHA,
	})
}

// LatestRound is the newest round of an agent, ErrReviewNoRound when none has been taken.
func (s *ReviewService) LatestRound(ctx context.Context, agentID string) (*reviewstore.Round, error) {
	round, err := s.rounds.LatestRound(ctx, agentID)
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
	if err := s.mayRead(ctx, agentID, author); err != nil {
		return nil, err
	}

	return s.rounds.ListRounds(ctx, agentID)
}

// ListReviews is every review given on an agent, in the order their rounds were taken, with the
// path of each dropped for anyone but the person reviewing, since it names a file on disk holding
// somebody else's words.
func (s *ReviewService) ListReviews(
	ctx context.Context,
	agentID string,
	author ReviewAuthor,
) ([]reviewstore.Review, error) {
	if err := s.mayRead(ctx, agentID, author); err != nil {
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

// openReviewFor is the batch an author is writing against an agent, opened against the newest
// round when they have none open, which is what makes writing a comment start a review rather
// than a gesture of its own.
func (s *ReviewService) openReviewFor(
	ctx context.Context,
	agentID string,
	author ReviewAuthor,
) (*reviewstore.Review, error) {
	if err := s.entitled(ctx, agentID, author); err != nil {
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

	round, err := s.LatestRound(ctx, agentID)
	if err != nil {
		return nil, err
	}

	if err := s.notReviewedAlready(ctx, params, *round); err != nil {
		return nil, err
	}

	return s.reviews.OpenReview(ctx, reviewstore.OpenReviewParams{
		AgentID:       agentID,
		RoundID:       round.ID,
		AuthorKind:    author.Kind,
		AuthorAgentID: author.AgentID,
	})
}

// notReviewedAlready holds a reviewing agent to one review per round, which is what bounds a
// reviewer and its parent ping-ponging: a new round appears only when the parent does work, so
// the loop is limited by real progress rather than by a number somebody has to tune.
func (s *ReviewService) notReviewedAlready(
	ctx context.Context,
	params reviewstore.AuthorParams,
	round reviewstore.Round,
) error {
	if params.AuthorKind != reviewstore.AuthorAgent {
		return nil
	}

	last, err := s.reviews.LastReleasedReviewOf(ctx, params)
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

// entitled says whether an author may give a review on an agent, which the person always may and
// an agent may only of the agent it is a child of, its own work excluded, since answering a
// review it was given is a different call.
func (s *ReviewService) entitled(ctx context.Context, agentID string, author ReviewAuthor) error {
	if !author.IsHuman() && author.AgentID != nil && *author.AgentID == agentID {
		return ErrReviewOwnWork
	}

	return s.mayRead(ctx, agentID, author)
}

// mayRead says whether an author may look at an agent's rounds and reviews at all, which the
// agent under review may do of its own work, since the threads it is answering are anchored to
// those rounds and it is told to diff them.
func (s *ReviewService) mayRead(ctx context.Context, agentID string, author ReviewAuthor) error {
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
	// Interrupt stops the agent where it stands before the mail is sent, so it reads the review
	// now rather than at the end of whatever it is doing.
	Interrupt bool
	Author    ReviewAuthor
}

// ReleaseReviewResult reports what letting go of a review did.
type ReleaseReviewResult struct {
	Review reviewstore.Review
	// Comments is how many the released batch carried.
	Comments int
	// Path is where the review was written for the agent to read.
	Path string
}

// ReleaseReview lets go of the author's open batch by writing the whole review out as one file and
// sending one piece of ordinary mail saying where it is, leaving when the agent is interrupted or
// typed at to mail, whose affair that is.
func (s *ReviewService) ReleaseReview(
	ctx context.Context,
	params ReleaseReviewParams,
) (*ReleaseReviewResult, error) {
	if err := s.entitled(ctx, params.AgentID, params.Author); err != nil {
		return nil, err
	}

	agent, err := s.liveAgent(ctx, params.AgentID)
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
	if comments == 0 && params.Verdict == reviewstore.VerdictCommented {
		return nil, fmt.Errorf("%w: %q", ErrReviewEmpty, params.AgentID)
	}

	round, err := s.rounds.GetRound(ctx, open.RoundID)
	if err != nil {
		return nil, err
	}

	threads, err := s.reviewThreads(ctx, open.ID)
	if err != nil {
		return nil, err
	}

	path, err := s.writeReview(ctx, *agent, *round, *open, params.Verdict, threads)
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
		Path:           path,
		DoorbellMailID: &doorbell.ID,
	})
	if err != nil {
		// The mail has gone, so the agent has the review whatever the panel goes on to draw.
		return nil, fmt.Errorf("the review reached %q but weave could not record that it had: %w", agent.Name, err)
	}

	return &ReleaseReviewResult{Review: *released, Comments: comments, Path: path}, nil
}

// reviewNotice is the whole of what mail carries, which is the round, the verdict, how much there
// is and where to read it, since the review itself is in the file.
func reviewNotice(ordinal int32, verdict reviewstore.Verdict, comments int, path string) string {
	return fmt.Sprintf("weave: review of round %d, %s, %d comment(s), read %s",
		ordinal, strings.ReplaceAll(string(verdict), "_", " "), comments, path)
}

// reviewThreads is what one batch actually says, gathered by the messages written under it
// rather than by the threads opened under it, since a further comment on a thread from an
// earlier round belongs to this batch while the thread it sits in does not.
func (s *ReviewService) reviewThreads(ctx context.Context, reviewID string) ([]ReviewThreadWithMessages, error) {
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

// writeReview puts the whole review under weave's own state directory rather than in the agent's
// worktree, where it would show up in the diff being reviewed, and leaves it there for good, since
// the moment worth reading an old review is the moment its agent is gone.
func (s *ReviewService) writeReview(
	ctx context.Context,
	agent agentstore.Agent,
	round reviewstore.Round,
	review reviewstore.Review,
	verdict reviewstore.Verdict,
	threads []ReviewThreadWithMessages,
) (string, error) {
	dir := filepath.Join(s.stateDir, reviewDirName, agent.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to make room for the review of %q: %w", agent.Name, err)
	}

	body := RenderReview(RenderReviewParams{
		Agent:   agent,
		Round:   round,
		Author:  s.authorName(ctx, review),
		Verdict: verdict,
		Threads: threads,
		Source:  s.snapshotLines(ctx, agent.Cwd, round, threads),
	})

	full := filepath.Join(dir, fmt.Sprintf("round-%d-%s.md", round.Ordinal, review.ID))
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		return "", fmt.Errorf("failed to write the review of %q: %w", agent.Name, err)
	}

	return full, nil
}

// authorName is what to call whoever gave a review in the file it is written to, which is empty
// for the person and the agent's name for an agent, falling back to its id when that agent has
// since been deleted.
func (s *ReviewService) authorName(ctx context.Context, review reviewstore.Review) string {
	if review.AuthorAgentID == nil {
		return ""
	}

	author, err := s.agents.GetByID(ctx, *review.AuthorAgentID)
	if err != nil {
		return *review.AuthorAgentID
	}

	return author.Name
}

// snapshotLines reads every file a review touches as the round froze it so each comment can carry
// the code around it, leaving out a file that cannot be read rather than failing the release,
// since a comment is still worth sending without the lines around it.
func (s *ReviewService) snapshotLines(
	ctx context.Context,
	dir string,
	round reviewstore.Round,
	threads []ReviewThreadWithMessages,
) map[string][]string {
	if dir == "" {
		return nil
	}

	held := make(map[string][]string, len(threads))
	for _, thread := range threads {
		path := thread.Thread.FilePath
		if path == "" {
			continue
		}
		if _, done := held[path]; done {
			continue
		}

		lines, err := s.snapshots.FileAt(ctx, dir, round.SnapshotSHA, path)
		if err != nil {
			lines = nil
		}

		held[path] = lines
	}

	return held
}

// liveAgent refuses an agent that has been archived, since there is nobody there to hand a review
// to and releasing one would say it had been told.
func (s *ReviewService) liveAgent(ctx context.Context, agentID string) (*agentstore.Agent, error) {
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
