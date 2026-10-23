package reviews

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"komrad/internal/events"
	"komrad/internal/position"
	"komrad/internal/reviews/history"
	pullrequeststore "komrad/internal/store/pullrequest"
	reviewstore "komrad/internal/store/review"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

// forgeTraceTimeout is how long tracing one task's new forge threads may take before komrad
// places the rest on their files.
const forgeTraceTimeout = 10 * time.Second

// NewForgeMirrorParams is what a forge mirror is built over.
type NewForgeMirrorParams struct {
	Reviews      *reviewstore.ReviewStore
	ForgeThreads *pullrequeststore.ForgeThreadStore
	Tracer       *history.Tracer
	Events       eventRecorder
	// BotNames is the logins whose comments are left out of the mirror, read on each pass.
	BotNames func() ([]string, error)
}

// ForgeMirror copies the forge's review threads into the reviews of the tasks on a pull request.
type ForgeMirror struct {
	reviews      *reviewstore.ReviewStore
	forgeThreads *pullrequeststore.ForgeThreadStore
	tracer       *history.Tracer
	events       eventRecorder
	botNames     func() ([]string, error)
}

// NewForgeMirror builds a forge mirror over the review and forge thread stores.
func NewForgeMirror(params NewForgeMirrorParams) *ForgeMirror {
	return &ForgeMirror{
		reviews:      params.Reviews,
		forgeThreads: params.ForgeThreads,
		tracer:       params.Tracer,
		events:       params.Events,
		botNames:     params.BotNames,
	}
}

// Mirror brings every task with a live agent on a pull request up to date with the forge's threads on it.
func (m *ForgeMirror) Mirror(ctx context.Context, pullRequestID uuid.UUID) error {
	threads, err := m.forgeThreads.ListOfPullRequest(ctx, pullRequestID)
	if err != nil {
		return err
	}
	threads, err = m.withoutBots(threads)
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
			errs = append(errs, fmt.Errorf("failed to mirror the forge threads onto task %q: %w", target.TaskID, err))
		}
	}

	return errors.Join(errs...)
}

// withoutBots drops the comments of the configured bot logins from each thread, so komrad mirrors
// a thread a bot opened from its first comment by anyone else.
func (m *ForgeMirror) withoutBots(threads []pullrequeststore.ForgeThread) ([]pullrequeststore.ForgeThread, error) {
	if m.botNames == nil {
		return threads, nil
	}
	names, err := m.botNames()
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return threads, nil
	}

	bots := make(map[string]bool, len(names))
	for _, name := range names {
		bots[strings.ToLower(name)] = true
	}
	for i := range threads {
		threads[i].Comments = xslice.Filter(threads[i].Comments, func(comment pullrequeststore.ForgeComment) bool {
			return !bots[strings.ToLower(comment.AuthorLogin)]
		})
	}

	return threads, nil
}

// mirroredThread is a forge thread a task already holds, with its forge comments by id.
type mirroredThread struct {
	ThreadID       uuid.UUID
	ResolvedAt     *time.Time
	ResolvedByKind agentv1.ReviewAuthorKind
	ForgeResolved  bool
	Comments       map[uuid.UUID]mirroredComment
}

type mirroredComment struct {
	MessageID uuid.UUID
	EditedAt  *time.Time
}

func indexMirrored(rows []reviewstore.ForgeMirrored) map[uuid.UUID]*mirroredThread {
	byForge := map[uuid.UUID]*mirroredThread{}
	for _, row := range rows {
		thread, ok := byForge[row.ForgeThreadID]
		if !ok {
			thread = &mirroredThread{
				ThreadID:       row.ThreadID,
				ResolvedAt:     row.ResolvedAt,
				ResolvedByKind: row.ResolvedByKind,
				ForgeResolved:  row.ForgeResolved,
				Comments:       map[uuid.UUID]mirroredComment{},
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
	rows, err := m.reviews.ListForgeMirrored(ctx, target.TaskID)
	if err != nil {
		return err
	}
	known := indexMirrored(rows)
	sent, err := m.listSentReplyIDs(ctx, known)
	if err != nil {
		return err
	}
	threads = trustEchoes(sent, threads)

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
				TaskID:     target.TaskID,
				SnapshotID: target.SnapshotID,
				Login:      comment.AuthorLogin,
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
				place, fresh := places[thread.ID]
				if !fresh {
					continue
				}

				created, err := createForgeThread(ctx, reviews, createForgeThreadParams{
					Target:    target,
					Thread:    thread,
					Place:     place,
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

		return releasedForgeEvents(ctx, reviews, target.TaskID, xslice.Map(order, func(key forgeReviewKey) reviewstore.Review {
			return *opened[key]
		}))
	})
}

// listSentReplyIDs returns the forge comment ids of the replies komrad posted on a task's mirrored threads.
func (m *ForgeMirror) listSentReplyIDs(ctx context.Context, known map[uuid.UUID]*mirroredThread) (map[string]bool, error) {
	threadIDs := make([]uuid.UUID, 0, len(known))
	for _, thread := range known {
		threadIDs = append(threadIDs, thread.ThreadID)
	}
	replies, err := m.reviews.ListForgeReplies(ctx, threadIDs)
	if err != nil {
		return nil, err
	}

	sent := map[string]bool{}
	for _, reply := range replies {
		if reply.ForgeCommentProviderID != nil {
			sent[*reply.ForgeCommentProviderID] = true
		}
	}

	return sent, nil
}

// trustEchoes marks each comment komrad posted as a reply trusted.
func trustEchoes(sent map[string]bool, threads []pullrequeststore.ForgeThread) []pullrequeststore.ForgeThread {
	return xslice.Map(threads, func(thread pullrequeststore.ForgeThread) pullrequeststore.ForgeThread {
		thread.Comments = xslice.Map(thread.Comments, func(comment pullrequeststore.ForgeComment) pullrequeststore.ForgeComment {
			comment.Trusted = comment.Trusted || sent[comment.ProviderID]

			return comment
		})

		return thread
	})
}

// isWorthMirroring reports whether a forge thread not yet mirrored should be, leaving out one
// already resolved on the forge so the agent is not told about it, and one on the pull request
// as a whole, which no file holds.
func isWorthMirroring(thread pullrequeststore.ForgeThread) bool {
	return len(thread.Comments) > 0 && !thread.Resolved && thread.Path != ""
}

type createForgeThreadParams struct {
	Target    reviewstore.ForgeMirrorTarget
	Thread    pullrequeststore.ForgeThread
	Place     history.Place
	ReviewFor func(pullrequeststore.ForgeComment) (*reviewstore.Review, error)
}

func createForgeThread(ctx context.Context, reviews *reviewstore.ReviewStore, params createForgeThreadParams) (*mirroredThread, error) {
	first := params.Thread.Comments[0]

	review, err := params.ReviewFor(first)
	if err != nil {
		return nil, err
	}

	thread, err := reviews.CreateThread(ctx, reviewstore.CreateThreadParams{
		TaskID:        params.Target.TaskID,
		ReviewID:      &review.ID,
		SnapshotID:    params.Target.SnapshotID,
		BaseSHA:       params.Target.BaseSHA,
		FilePath:      params.Place.FilePath,
		NewStart:      params.Place.NewStart,
		NewEnd:        params.Place.NewStart,
		Body:          first.Body,
		AuthorKind:    agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_FORGE,
		External:      externalOf(first),
		ForgeThreadID: &params.Thread.ID,
		Held:          !first.Trusted,
	})
	if err != nil {
		return nil, err
	}

	return &mirroredThread{
		ThreadID: thread.ID,
		Comments: map[uuid.UUID]mirroredComment{first.ID: {EditedAt: first.EditedAt}},
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
	for _, comment := range params.Thread.Comments {
		held, ok := params.Mirrored.Comments[comment.ID]
		if ok && (held.MessageID == uuid.Nil || isSameEdit(held.EditedAt, comment.EditedAt)) {
			continue
		}

		if ok {
			if err := reviews.UpdateForgeMessage(ctx, reviewstore.UpdateForgeMessageParams{
				ID:       held.MessageID,
				Body:     comment.Body,
				EditedAt: comment.EditedAt,
				Held:     !comment.Trusted,
			}); err != nil {
				return err
			}

			continue
		}

		review, err := params.ReviewFor(comment)
		if err != nil {
			return err
		}

		// Komrad writes a reply with no snapshot so it does not reopen the thread.
		if _, err := reviews.CreateMessage(ctx, reviewstore.CreateMessageParams{
			ThreadID:   params.Mirrored.ThreadID,
			ReviewID:   &review.ID,
			AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_FORGE,
			Body:       comment.Body,
			External:   externalOf(comment),
			Held:       !comment.Trusted,
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
	forgeResolves := thread.Resolved && !resolved
	forgeReopens := !thread.Resolved && resolved && mirrored.ResolvedByKind == agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_FORGE
	if !forgeResolves && !forgeReopens {
		return nil
	}

	_, err := reviews.SetThreadResolved(ctx, reviewstore.SetThreadResolvedParams{
		ID:       mirrored.ThreadID,
		Resolved: thread.Resolved,
		ByKind:   agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_FORGE,
	})

	return err
}

// releasedForgeEvents tells the agent about each of the forge reviews a sync opened that holds
// something it may read.
func releasedForgeEvents(ctx context.Context, reviews *reviewstore.ReviewStore, taskID uuid.UUID, opened []reviewstore.Review) ([]river.JobArgs, error) {
	if len(opened) == 0 {
		return nil, nil
	}

	threads, err := reviews.ListThreads(ctx, taskID)
	if err != nil {
		return nil, err
	}

	messages, err := reviews.ListMessages(ctx, xslice.Map(threads, func(thread reviewstore.Thread) uuid.UUID { return thread.ID }))
	if err != nil {
		return nil, err
	}

	jobs := make([]river.JobArgs, 0, len(opened))
	for _, review := range opened {
		tally := tallyForge(review.ID, threads, messages)
		if tally.Comments == 0 && tally.Replies == 0 {
			continue
		}

		snapshot, err := reviews.GetSnapshot(ctx, review.SnapshotID)
		if err != nil {
			return nil, err
		}

		event, err := events.NewEvent(Released{
			ReviewID:   review.ID,
			TaskID:     taskID,
			Ordinal:    snapshot.Ordinal,
			Verdict:    agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED,
			Comments:   tally.Comments,
			Replies:    tally.Replies,
			ForgeLogin: xptr.Deref(review.ExternalLogin),
		})
		if err != nil {
			return nil, err
		}

		jobs = append(jobs, *event)
	}

	return jobs, nil
}

// tallyForge counts the threads a forge review opened and the earlier threads it replied in,
// leaving out whatever is held from the agent.
func tallyForge(reviewID uuid.UUID, threads []reviewstore.Thread, messages []reviewstore.Message) batchTally {
	var tally batchTally

	replied := map[uuid.UUID]bool{}
	for _, message := range messages {
		if message.ReviewID != nil && *message.ReviewID == reviewID && !message.Held {
			replied[message.ThreadID] = true
		}
	}

	for _, thread := range threads {
		switch {
		case !replied[thread.ID]:
		case thread.ReviewID != nil && *thread.ReviewID == reviewID:
			tally.Comments++
		default:
			tally.Replies++
		}
	}

	return tally
}

// place traces each new forge thread from the commit the forge counted it in to the task's
// latest snapshot, keeping its file when the line is gone or the commit cannot be read.
func (m *ForgeMirror) place(ctx context.Context, target reviewstore.ForgeMirrorTarget, threads []pullrequeststore.ForgeThread) map[uuid.UUID]history.Place {
	places := map[uuid.UUID]history.Place{}
	byFrom := map[string][]history.Anchor{}

	for _, thread := range threads {
		places[thread.ID] = history.Place{FilePath: thread.Path, FileOnly: true}

		if thread.Line <= 0 || thread.OldSide || thread.CommitSHA == "" {
			continue
		}

		if thread.CommitSHA == target.SnapshotSHA {
			places[thread.ID] = history.Place{
				FilePath: thread.Path,
				NewStart: int32(thread.Line),
				Exact:    true,
			}

			continue
		}

		byFrom[thread.CommitSHA] = append(byFrom[thread.CommitSHA], history.Anchor{
			ID: thread.ID,
			Position: position.Position{
				Path:  thread.Path,
				Lines: position.Range{Start: thread.Line, End: thread.Line + 1},
			},
		})
	}

	if len(byFrom) == 0 {
		return places
	}

	ctx, cancel := context.WithTimeout(ctx, forgeTraceTimeout)
	defer cancel()

	for from, anchors := range byFrom {
		traced, err := m.tracer.TracePair(ctx, history.TracePairParams{
			Dir:     target.Cwd,
			FromSHA: from,
			ToSHA:   target.SnapshotSHA,
			Anchors: anchors,
		})
		if err != nil {
			continue
		}

		maps.Copy(places, traced)
	}

	return places
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
	return &reviewstore.External{
		Login:     comment.AuthorLogin,
		CommentID: comment.ID,
		EditedAt:  comment.EditedAt,
	}
}
