package reviews

import (
	"context"
	"errors"
	"fmt"
	"time"

	"komrad/internal/events"
	"komrad/internal/git"
	"komrad/internal/position"
	pullrequeststore "komrad/internal/store/pullrequest"
	reviewstore "komrad/internal/store/review"
	"komrad/lib/xslice"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

// forgeTraceTimeout is how long tracing one agent's new forge threads may take before the rest
// are placed on their files.
const forgeTraceTimeout = 10 * time.Second

type rangeMapper interface {
	MapRanges(ctx context.Context, params git.MapRangesParams) ([]position.Mapping, error)
}

// NewForgeMirrorParams is what a forge mirror is built over.
type NewForgeMirrorParams struct {
	Reviews      *reviewstore.ReviewStore
	ForgeThreads *pullrequeststore.ForgeThreadStore
	Git          rangeMapper
	Events       eventRecorder
}

// ForgeMirror copies the forge's review threads into the reviews of the agents on a pull request.
type ForgeMirror struct {
	reviews      *reviewstore.ReviewStore
	forgeThreads *pullrequeststore.ForgeThreadStore
	git          rangeMapper
	events       eventRecorder
}

// NewForgeMirror builds a forge mirror over the review and forge thread stores.
func NewForgeMirror(params NewForgeMirrorParams) *ForgeMirror {
	return &ForgeMirror{
		reviews:      params.Reviews,
		forgeThreads: params.ForgeThreads,
		git:          params.Git,
		events:       params.Events,
	}
}

// Mirror brings every live agent on a pull request up to date with the forge's threads on it.
func (m *ForgeMirror) Mirror(ctx context.Context, pullRequestID string) error {
	threads, err := m.forgeThreads.ListOfPullRequest(ctx, pullRequestID)
	if err != nil {
		return err
	}
	if len(threads) == 0 {
		return nil
	}

	targets, err := m.reviews.ListForgeMirrorTargets(ctx, pullRequestID)
	if err != nil {
		return err
	}

	var errs []error
	for _, target := range targets {
		if err := m.mirrorOnto(ctx, target, threads); err != nil {
			errs = append(errs, fmt.Errorf("failed to mirror the forge threads onto agent %q: %w", target.AgentID, err))
		}
	}

	return errors.Join(errs...)
}

// mirroredThread is a forge thread an agent already holds, with its forge comments by id.
type mirroredThread struct {
	ThreadID       string
	ResolvedAt     *time.Time
	ResolvedByKind reviewstore.AuthorKind
	ForgeResolved  bool
	Comments       map[string]mirroredComment
}

type mirroredComment struct {
	MessageID string
	EditedAt  *time.Time
}

func indexMirrored(rows []reviewstore.ForgeMirrored) map[string]*mirroredThread {
	byForge := map[string]*mirroredThread{}
	for _, row := range rows {
		thread, ok := byForge[row.ForgeThreadID]
		if !ok {
			thread = &mirroredThread{
				ThreadID:       row.ThreadID,
				ResolvedAt:     row.ResolvedAt,
				ResolvedByKind: row.ResolvedByKind,
				ForgeResolved:  row.ForgeResolved,
				Comments:       map[string]mirroredComment{},
			}
			byForge[row.ForgeThreadID] = thread
		}

		if row.MessageID != nil && row.ForgeCommentID != nil {
			thread.Comments[*row.ForgeCommentID] = mirroredComment{MessageID: *row.MessageID, EditedAt: row.ForgeEditedAt}
		}
	}

	return byForge
}

// forgeReviewKey is one forge reviewer's submission, which one sync gathers into one review.
type forgeReviewKey struct {
	ForgeReviewID string
	Login         string
}

func (m *ForgeMirror) mirrorOnto(ctx context.Context, target reviewstore.ForgeMirrorTarget, threads []pullrequeststore.ForgeThread) error {
	rows, err := m.reviews.ListForgeMirrored(ctx, target.AgentID)
	if err != nil {
		return err
	}
	known := indexMirrored(rows)

	var fresh []pullrequeststore.ForgeThread
	for _, thread := range threads {
		if _, ok := known[thread.ID]; !ok && isWorthMirroring(thread) {
			fresh = append(fresh, thread)
		}
	}
	places := m.place(ctx, target, fresh)

	return m.events.InsertManyTx(ctx, func(tx pgx.Tx) ([]river.JobArgs, error) {
		reviews := reviewstore.NewReviewStore(tx)
		opened := map[forgeReviewKey]*reviewstore.Review{}
		var order []forgeReviewKey

		reviewFor := func(comment pullrequeststore.ForgeComment) (*reviewstore.Review, error) {
			key := forgeReviewKey{ForgeReviewID: comment.ForgeReviewID, Login: comment.AuthorLogin}
			if review, ok := opened[key]; ok {
				return review, nil
			}

			review, err := reviews.OpenForgeReview(ctx, reviewstore.OpenForgeReviewParams{
				AgentID:  target.AgentID,
				RoundID:  target.RoundID,
				Login:    comment.AuthorLogin,
				Released: comment.Trusted,
			})
			if err != nil {
				return nil, err
			}
			opened[key] = review
			order = append(order, key)

			return review, nil
		}

		for _, thread := range threads {
			mirrored, ok := known[thread.ID]
			if !ok {
				if !isWorthMirroring(thread) {
					continue
				}

				created, err := createForgeThread(ctx, reviews, createForgeThreadParams{
					Target:    target,
					Thread:    thread,
					Place:     places[thread.ID],
					ReviewFor: reviewFor,
				})
				if err != nil {
					return nil, err
				}
				mirrored = created
			}

			if err := mirrorComments(ctx, reviews, mirrorCommentsParams{
				Mirrored:  mirrored,
				Thread:    thread,
				ReviewFor: reviewFor,
			}); err != nil {
				return nil, err
			}

			if err := syncResolved(ctx, reviews, mirrored, thread); err != nil {
				return nil, err
			}
		}

		return releasedForgeEvents(ctx, reviews, target.AgentID, xslice.Map(order, func(key forgeReviewKey) reviewstore.Review {
			return *opened[key]
		}))
	})
}

// isWorthMirroring reports whether a forge thread not yet mirrored should be, leaving out one
// already resolved on the forge so the agent is not told about it.
func isWorthMirroring(thread pullrequeststore.ForgeThread) bool {
	return len(thread.Comments) > 0 && !thread.Resolved
}

type createForgeThreadParams struct {
	Target    reviewstore.ForgeMirrorTarget
	Thread    pullrequeststore.ForgeThread
	Place     forgePlace
	ReviewFor func(pullrequeststore.ForgeComment) (*reviewstore.Review, error)
}

func createForgeThread(ctx context.Context, reviews *reviewstore.ReviewStore, params createForgeThreadParams) (*mirroredThread, error) {
	first := params.Thread.Comments[0]

	review, err := params.ReviewFor(first)
	if err != nil {
		return nil, err
	}

	thread, err := reviews.CreateThread(ctx, reviewstore.CreateThreadParams{
		AgentID:       params.Target.AgentID,
		ReviewID:      review.ID,
		RoundID:       params.Target.RoundID,
		BaseSHA:       params.Target.BaseSHA,
		FilePath:      params.Place.FilePath,
		NewStart:      params.Place.NewStart,
		NewEnd:        params.Place.NewStart,
		Body:          first.Body,
		AuthorKind:    reviewstore.AuthorForge,
		External:      externalOf(first),
		ForgeThreadID: &params.Thread.ID,
	})
	if err != nil {
		return nil, err
	}

	return &mirroredThread{
		ThreadID: thread.ID,
		Comments: map[string]mirroredComment{first.ID: {EditedAt: first.EditedAt}},
	}, nil
}

type mirrorCommentsParams struct {
	Mirrored  *mirroredThread
	Thread    pullrequeststore.ForgeThread
	ReviewFor func(pullrequeststore.ForgeComment) (*reviewstore.Review, error)
}

// mirrorComments adds the forge comments the thread does not hold yet and rewrites the ones the
// forge says were edited.
func mirrorComments(ctx context.Context, reviews *reviewstore.ReviewStore, params mirrorCommentsParams) error {
	mirrored := params.Mirrored
	for _, comment := range params.Thread.Comments {
		held, ok := mirrored.Comments[comment.ID]
		if ok {
			if held.MessageID == "" || isSameEdit(held.EditedAt, comment.EditedAt) {
				continue
			}

			review, err := params.ReviewFor(comment)
			if err != nil {
				return err
			}

			if err := reviews.UpdateForgeMessage(ctx, reviewstore.UpdateForgeMessageParams{
				ID:       held.MessageID,
				Body:     comment.Body,
				EditedAt: comment.EditedAt,
				ReviewID: review.ID,
			}); err != nil {
				return err
			}

			continue
		}

		review, err := params.ReviewFor(comment)
		if err != nil {
			return err
		}

		// A reply is written with no round, since one with a round reopens the thread and the forge
		// alone says whether it is resolved.
		if _, err := reviews.CreateMessage(ctx, reviewstore.CreateMessageParams{
			ThreadID:   mirrored.ThreadID,
			ReviewID:   &review.ID,
			AuthorKind: reviewstore.AuthorForge,
			Body:       comment.Body,
			External:   externalOf(comment),
		}); err != nil {
			return err
		}
	}

	return nil
}

// syncResolved follows the forge when it resolves or reopens a thread, leaving alone a thread the
// person changed while the forge stayed put.
func syncResolved(ctx context.Context, reviews *reviewstore.ReviewStore, mirrored *mirroredThread, thread pullrequeststore.ForgeThread) error {
	if thread.Resolved == mirrored.ForgeResolved {
		return nil
	}

	if err := reviews.SetForgeResolved(ctx, mirrored.ThreadID, thread.Resolved); err != nil {
		return err
	}

	resolved := mirrored.ResolvedAt != nil
	switch {
	case thread.Resolved && !resolved:
	case !thread.Resolved && resolved && mirrored.ResolvedByKind == reviewstore.AuthorForge:
	default:
		return nil
	}

	_, err := reviews.SetThreadResolved(ctx, reviewstore.SetThreadResolvedParams{
		ID:       mirrored.ThreadID,
		Resolved: thread.Resolved,
		ByKind:   reviewstore.AuthorForge,
	})

	return err
}

// releasedForgeEvents tells the agent about each of the forge reviews that went out released.
func releasedForgeEvents(ctx context.Context, reviews *reviewstore.ReviewStore, agentID string, opened []reviewstore.Review) ([]river.JobArgs, error) {
	released := xslice.Filter(opened, func(review reviewstore.Review) bool { return review.ReleasedAt != nil })
	if len(released) == 0 {
		return nil, nil
	}

	threads, err := reviews.ListThreads(ctx, agentID)
	if err != nil {
		return nil, err
	}

	messages, err := reviews.ListMessages(ctx, xslice.Map(threads, func(thread reviewstore.Thread) string { return thread.ID }))
	if err != nil {
		return nil, err
	}

	jobs := make([]river.JobArgs, 0, len(released))
	for _, review := range released {
		round, err := reviews.GetRound(ctx, review.RoundID)
		if err != nil {
			return nil, err
		}

		tally := tallyForge(review.ID, threads, messages)

		event, err := events.NewEvent(Released{
			ReviewID:   review.ID,
			AgentID:    agentID,
			Ordinal:    round.Ordinal,
			Verdict:    reviewstore.VerdictCommented,
			Comments:   tally.Comments,
			Replies:    tally.Replies,
			ForgeLogin: xptrValue(review.ExternalLogin),
		})
		if err != nil {
			return nil, err
		}

		jobs = append(jobs, *event)
	}

	return jobs, nil
}

// tallyForge counts the threads a forge review opened and the earlier threads it replied in.
func tallyForge(reviewID string, threads []reviewstore.Thread, messages []reviewstore.Message) batchTally {
	var tally batchTally

	replied := map[string]bool{}
	for _, message := range messages {
		if message.ReviewID != nil && *message.ReviewID == reviewID {
			replied[message.ThreadID] = true
		}
	}

	for _, thread := range threads {
		switch {
		case thread.ReviewID == reviewID:
			tally.Comments++
		case replied[thread.ID]:
			tally.Replies++
		}
	}

	return tally
}

func xptrValue(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

// forgePlace is where a new forge thread is pinned in the agent's latest round.
type forgePlace struct {
	FilePath string
	NewStart int32
}

// place traces each new forge thread from the commit the forge counted it in to the agent's
// latest snapshot, keeping its file when the line is gone or the commit cannot be read.
func (m *ForgeMirror) place(ctx context.Context, target reviewstore.ForgeMirrorTarget, threads []pullrequeststore.ForgeThread) map[string]forgePlace {
	places := map[string]forgePlace{}
	byFrom := map[string][]pullrequeststore.ForgeThread{}

	for _, thread := range threads {
		places[thread.ID] = forgePlace{FilePath: thread.Path}

		if thread.Path == "" || thread.Line <= 0 || thread.OldSide || thread.CommitSHA == "" {
			continue
		}

		if thread.CommitSHA == target.SnapshotSHA {
			places[thread.ID] = forgePlace{FilePath: thread.Path, NewStart: int32(thread.Line)}

			continue
		}

		byFrom[thread.CommitSHA] = append(byFrom[thread.CommitSHA], thread)
	}

	if len(byFrom) == 0 {
		return places
	}

	ctx, cancel := context.WithTimeout(ctx, forgeTraceTimeout)
	defer cancel()

	for from, group := range byFrom {
		mappings, err := m.git.MapRanges(ctx, git.MapRangesParams{
			Dir:     target.Cwd,
			FromSHA: from,
			ToSHA:   target.SnapshotSHA,
		})
		if err != nil {
			continue
		}

		results := position.Transform(position.TransformParams{
			Positions: xslice.Map(group, func(thread pullrequeststore.ForgeThread) position.Position {
				return position.Position{
					Path:  thread.Path,
					Lines: position.Range{Start: thread.Line, End: thread.Line + 1},
				}
			}),
			Mappings: mappings,
			Policy:   position.FileOnConflict,
		})

		traced := map[int]bool{}
		for _, result := range results {
			// A file copied to two paths answers twice, and the first answer is kept.
			if result.Index < 0 || result.Index >= len(group) || traced[result.Index] {
				continue
			}
			traced[result.Index] = true

			places[group[result.Index].ID] = placeOf(group[result.Index], result)
		}
	}

	return places
}

func placeOf(thread pullrequeststore.ForgeThread, result position.Result) forgePlace {
	path := result.Position.Path
	if path == "" {
		path = thread.Path
	}

	if result.Outcome != position.Placed || result.Position.IsFileLevel() {
		return forgePlace{FilePath: path}
	}

	return forgePlace{FilePath: path, NewStart: int32(result.Position.Lines.Start)}
}

// isSameEdit reports whether two edit times name the same edit, to the second the forge keeps.
func isSameEdit(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}

	return a.Truncate(time.Second).Equal(b.Truncate(time.Second))
}

// externalOf is who wrote a forge comment and which comment it was.
func externalOf(comment pullrequeststore.ForgeComment) *reviewstore.External {
	return &reviewstore.External{Login: comment.AuthorLogin, CommentID: comment.ID, EditedAt: comment.EditedAt}
}

// releaseHeldForge sends every forge review the person was holding on an agent, with an event
// for each.
func releaseHeldForge(ctx context.Context, tx pgx.Tx, agentID string) ([]reviewstore.Review, []river.JobArgs, error) {
	reviews := reviewstore.NewReviewStore(tx)

	released, err := reviews.ReleaseForgeReviews(ctx, agentID)
	if err != nil {
		return nil, nil, err
	}

	jobs, err := releasedForgeEvents(ctx, reviews, agentID, released)
	if err != nil {
		return nil, nil, err
	}

	return released, jobs, nil
}

// releaseHeldForgeInThread sends the forge reviews the person was holding that wrote in one
// thread, with an event for each.
func releaseHeldForgeInThread(ctx context.Context, tx pgx.Tx, agentID, threadID string) ([]river.JobArgs, error) {
	reviews := reviewstore.NewReviewStore(tx)

	released, err := reviews.ReleaseForgeReviewsInThread(ctx, threadID)
	if err != nil {
		return nil, err
	}

	return releasedForgeEvents(ctx, reviews, agentID, released)
}
