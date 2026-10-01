package reviews

import (
	"context"
	"errors"
	"slices"
	"time"

	"komrad/internal/fail"
	"komrad/internal/store"
	pullrequeststore "komrad/internal/store/pullrequest"
	reviewstore "komrad/internal/store/review"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

// ErrForgeThreadNotFound is answered when a forge thread id names nothing on the agent's pull requests.
var ErrForgeThreadNotFound = fail.NotFound("forge thread not found")

// ErrForgeThreadMoved is answered when the forge changed a comment the person read before they accepted it.
var ErrForgeThreadMoved = fail.Precondition("the forge thread changed since you read it")

// ErrForgeThreadNothingNew is answered when accepting a forge thread again would copy nothing and say nothing.
var ErrForgeThreadNothingNew = fail.InvalidArgument("every comment you read is already in your review")

// SeenForgeComment is one forge comment as the person read it.
type SeenForgeComment struct {
	ID       string
	EditedAt *time.Time
}

// AcceptForgeThreadParams is the forge thread the person accepts, the comments they read in it, and where a new thread is pinned.
type AcceptForgeThreadParams struct {
	AgentID       string
	ForgeThreadID string
	Seen          []SeenForgeComment
	BaseSHA       string
	SnapshotSHA   string
	FilePath      string
	OldStart      int32
	OldEnd        int32
	NewStart      int32
	NewEnd        int32
	QuotedLines   []string
	Note          string
	Author        ReviewAuthor
}

// AcceptForgeThread copies the forge comments the person read into their open review as a thread of their own, or into the thread they accepted it into before.
func (s *ReviewService) AcceptForgeThread(
	ctx context.Context,
	params AcceptForgeThreadParams,
) (*ReviewThreadWithMessages, error) {
	if !params.Author.IsHuman() {
		return nil, ErrReviewHumanOnly
	}

	offer, err := s.forgeThreads.GetForAgent(ctx, params.ForgeThreadID, params.AgentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrForgeThreadNotFound.Saying("no forge thread %q on agent %q", params.ForgeThreadID, params.AgentID)
	}
	if err != nil {
		return nil, err
	}

	seen, err := requireSeenUnchanged(offer.Comments, params.Seen)
	if err != nil {
		return nil, err
	}

	if offer.AcceptedThreadID != nil {
		return s.acceptAgain(ctx, *offer, seen, params)
	}

	return s.acceptFirst(ctx, *offer, seen, params)
}

func (s *ReviewService) acceptFirst(
	ctx context.Context,
	offer pullrequeststore.ForgeThread,
	seen []pullrequeststore.ForgeComment,
	params AcceptForgeThreadParams,
) (*ReviewThreadWithMessages, error) {
	place, err := s.placeThread(ctx, CreateReviewThreadParams{
		AgentID:     params.AgentID,
		BaseSHA:     params.BaseSHA,
		SnapshotSHA: params.SnapshotSHA,
		Author:      params.Author,
	})
	if err != nil {
		return nil, err
	}

	var thread *reviewstore.Thread
	err = s.events.InsertManyTx(ctx, func(tx pgx.Tx) ([]river.JobArgs, error) {
		reviews := reviewstore.NewReviewStore(tx)

		var err error
		thread, err = reviews.CreateThread(ctx, reviewstore.CreateThreadParams{
			AgentID:     params.AgentID,
			ReviewID:    place.ReviewID,
			RoundID:     place.RoundID,
			BaseSHA:     place.BaseSHA,
			FilePath:    params.FilePath,
			OldStart:    params.OldStart,
			OldEnd:      params.OldEnd,
			NewStart:    params.NewStart,
			NewEnd:      params.NewEnd,
			QuotedLines: params.QuotedLines,
			Body:        seen[0].Body,
			AuthorKind:  params.Author.Kind,
			External:    externalOf(seen[0]),
		})
		if err != nil {
			return nil, err
		}

		if err := addAccepted(ctx, reviews, addAcceptedParams{
			ThreadID: thread.ID,
			ReviewID: place.ReviewID,
			Comments: seen[1:],
			Note:     params.Note,
			Author:   params.Author,
		}); err != nil {
			return nil, err
		}

		return nil, pullrequeststore.NewForgeThreadStore(tx).SetAccepted(ctx, offer.ID, thread.ID, nil)
	})
	if err != nil {
		return nil, err
	}

	return s.withMessages(ctx, *thread)
}

func (s *ReviewService) acceptAgain(
	ctx context.Context,
	offer pullrequeststore.ForgeThread,
	seen []pullrequeststore.ForgeComment,
	params AcceptForgeThreadParams,
) (*ReviewThreadWithMessages, error) {
	thread, err := s.reviews.GetThread(ctx, *offer.AcceptedThreadID)
	if err != nil {
		return nil, err
	}

	messages, err := s.reviews.ListMessages(ctx, []string{thread.ID})
	if err != nil {
		return nil, err
	}
	if _, err := requireFresh(messages, seen, params.Note); err != nil {
		return nil, err
	}

	open, err := s.openReviewFor(ctx, openReviewForParams{AgentID: thread.AgentID, Author: params.Author})
	if err != nil {
		return nil, err
	}

	err = s.events.InsertManyTx(ctx, func(tx pgx.Tx) ([]river.JobArgs, error) {
		forgeThreads, reviews := pullrequeststore.NewForgeThreadStore(tx), reviewstore.NewReviewStore(tx)
		if err := forgeThreads.LockAccepted(ctx, offer.ID, thread.ID); err != nil {
			return nil, err
		}

		messages, err := reviews.ListMessages(ctx, []string{thread.ID})
		if err != nil {
			return nil, err
		}

		fresh, err := requireFresh(messages, seen, params.Note)
		if err != nil {
			return nil, err
		}

		if err := addAccepted(ctx, reviews, addAcceptedParams{
			ThreadID: thread.ID,
			ReviewID: open.ID,
			Comments: fresh,
			Note:     params.Note,
			Author:   params.Author,
		}); err != nil {
			return nil, err
		}

		return nil, forgeThreads.SetAccepted(ctx, offer.ID, thread.ID, &thread.ID)
	})
	if err != nil {
		return nil, err
	}

	return s.withMessages(ctx, *thread)
}

type addAcceptedParams struct {
	ThreadID string
	ReviewID string
	Comments []pullrequeststore.ForgeComment
	Note     string
	Author   ReviewAuthor
}

func addAccepted(ctx context.Context, reviews *reviewstore.ReviewStore, params addAcceptedParams) error {
	for _, comment := range params.Comments {
		if _, err := reviews.CreateMessage(ctx, reviewstore.CreateMessageParams{
			ThreadID:   params.ThreadID,
			ReviewID:   &params.ReviewID,
			AuthorKind: params.Author.Kind,
			Body:       comment.Body,
			External:   externalOf(comment),
		}); err != nil {
			return err
		}
	}

	if params.Note == "" {
		return nil
	}

	_, err := reviews.CreateMessage(ctx, reviewstore.CreateMessageParams{
		ThreadID:   params.ThreadID,
		ReviewID:   &params.ReviewID,
		AuthorKind: params.Author.Kind,
		Body:       params.Note,
	})

	return err
}

func (s *ReviewService) withMessages(ctx context.Context, thread reviewstore.Thread) (*ReviewThreadWithMessages, error) {
	messages, err := s.reviews.ListMessages(ctx, []string{thread.ID})
	if err != nil {
		return nil, err
	}

	return &ReviewThreadWithMessages{Thread: thread, Messages: messages}, nil
}

// requireSeenUnchanged is the stored comments the person read, oldest first.
func requireSeenUnchanged(stored []pullrequeststore.ForgeComment, seen []SeenForgeComment) ([]pullrequeststore.ForgeComment, error) {
	read := make(map[string]SeenForgeComment, len(seen))
	for _, comment := range seen {
		read[comment.ID] = comment
	}

	var kept []pullrequeststore.ForgeComment
	for _, comment := range stored {
		saw, ok := read[comment.ID]
		if !ok {
			continue
		}
		if !isSameEdit(saw.EditedAt, comment.EditedAt) {
			return nil, ErrForgeThreadMoved.Saying("a comment was edited on the forge since you read it")
		}

		kept = append(kept, comment)
		delete(read, comment.ID)
	}

	if len(read) > 0 {
		return nil, ErrForgeThreadMoved.Saying("a comment you read was deleted on the forge")
	}
	if len(kept) == 0 {
		return nil, ErrForgeThreadNothingNew.Saying("no comment of the forge thread was read")
	}

	return kept, nil
}

// requireFresh is the comments read that the thread does not hold yet.
func requireFresh(messages []reviewstore.Message, seen []pullrequeststore.ForgeComment, note string) ([]pullrequeststore.ForgeComment, error) {
	fresh := slices.DeleteFunc(slices.Clone(seen), func(comment pullrequeststore.ForgeComment) bool {
		return isCopied(messages, comment)
	})
	if len(fresh) == 0 && note == "" {
		return nil, ErrForgeThreadNothingNew
	}

	return fresh, nil
}

func isCopied(messages []reviewstore.Message, comment pullrequeststore.ForgeComment) bool {
	return slices.ContainsFunc(messages, func(message reviewstore.Message) bool {
		return message.External != nil &&
			message.External.CommentID == comment.ID &&
			isSameEdit(message.External.EditedAt, comment.EditedAt)
	})
}

func isSameEdit(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}

	return a.Truncate(time.Second).Equal(b.Truncate(time.Second))
}

func externalOf(comment pullrequeststore.ForgeComment) *reviewstore.External {
	return &reviewstore.External{
		Login:     comment.AuthorLogin,
		CommentID: comment.ID,
		EditedAt:  comment.EditedAt,
	}
}
