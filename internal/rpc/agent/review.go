package agent

import (
	"context"

	"komrad/internal/caller"
	"komrad/internal/fail"
	"komrad/internal/reviews"
	"komrad/internal/reviews/history"
	"komrad/internal/rpc/render"
	"komrad/internal/rpcerr"
	reviewstore "komrad/internal/store/review"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
)

type reviewConversation interface {
	ListReviewThreads(
		ctx context.Context,
		params reviews.ListReviewThreadsParams,
	) ([]reviews.ReviewThreadWithMessages, error)
	CreateReviewThread(ctx context.Context, params reviews.CreateReviewThreadParams) (*reviews.ReviewThreadWithMessages, error)
	AddReviewMessage(ctx context.Context, params reviews.AddReviewMessageParams) (*reviews.AddReviewMessageResult, error)
	AcceptForgeMessage(ctx context.Context, params reviews.AcceptForgeMessageParams) (*reviewstore.Message, error)
	EditReviewMessage(ctx context.Context, params reviews.EditReviewMessageParams) (*reviewstore.Message, error)
	MarkReviewThreadRead(ctx context.Context, params reviews.MarkReviewThreadReadParams) (*reviewstore.Thread, error)
	SetReviewThreadResolved(ctx context.Context, params reviews.SetReviewThreadResolvedParams) (*reviewstore.Thread, error)
	OpenRound(ctx context.Context, task string, author reviews.ReviewAuthor) (*reviewstore.Round, error)
	ListRounds(ctx context.Context, task string, author reviews.ReviewAuthor) ([]reviewstore.Round, error)
	GetHistory(ctx context.Context, params reviews.GetHistoryParams) (*reviews.ReviewHistory, error)
	ListReviews(ctx context.Context, task string, author reviews.ReviewAuthor) ([]reviewstore.Review, error)
	GetReview(ctx context.Context, reviewID string, author reviews.ReviewAuthor) (*reviews.ReviewInFull, error)
	ReleaseReview(ctx context.Context, params reviews.ReleaseReviewParams) (*reviews.ReleaseReviewResult, error)
	SetReviewFileViewed(ctx context.Context, params reviews.SetReviewFileViewedParams) (*reviewstore.ViewedFile, error)
	ListReviewViewedFiles(ctx context.Context, params reviews.ListReviewViewedFilesParams) ([]reviewstore.ViewedFile, error)
}

// ListReviewThreads reads the review pinned to a task's diff.
func (s *Handler) ListReviewThreads(ctx context.Context, req *agentv1.ListReviewThreadsRequest) (*agentv1.ListReviewThreadsResponse, *fail.Error) {
	threads, err := s.review.ListReviewThreads(ctx, reviews.ListReviewThreadsParams{
		Task:   req.GetTask(),
		Author: reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the review threads")
	}

	answered := xslice.Map(threads, func(thread reviews.ReviewThreadWithMessages) *agentv1.ReviewThread {
		return render.ReviewThread(thread.Thread, thread.Messages)
	})

	return &agentv1.ListReviewThreadsResponse{Threads: answered}, nil
}

// CreateReviewThread pins a conversation to a place in a diff and writes its first message.
func (s *Handler) CreateReviewThread(ctx context.Context, req *agentv1.CreateReviewThreadRequest) (*agentv1.CreateReviewThreadResponse, *fail.Error) {
	thread, err := s.review.CreateReviewThread(ctx, reviews.CreateReviewThreadParams{
		Task:        req.GetTask(),
		BaseSHA:     req.GetBaseSha(),
		SnapshotSHA: req.GetSnapshotSha(),
		FilePath:    req.GetFilePath(),
		OldStart:    req.GetOldStart(),
		OldEnd:      req.GetOldEnd(),
		NewStart:    req.GetNewStart(),
		NewEnd:      req.GetNewEnd(),
		QuotedLines: req.GetQuotedLines(),
		Body:        req.GetBody(),
		Suggestion:  req.GetSuggestion(),
		Author:      reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to create the review thread")
	}

	return &agentv1.CreateReviewThreadResponse{Thread: render.ReviewThread(thread.Thread, thread.Messages)}, nil
}

// AddReviewMessage says something in an existing thread, as whoever is calling.
func (s *Handler) AddReviewMessage(ctx context.Context, req *agentv1.AddReviewMessageRequest) (*agentv1.AddReviewMessageResponse, *fail.Error) {
	added, err := s.review.AddReviewMessage(ctx, reviews.AddReviewMessageParams{
		ThreadID:   req.GetThreadId(),
		Body:       req.GetBody(),
		Suggestion: req.GetSuggestion(),
		Author:     reviewAuthorFrom(ctx),
		Addressed:  req.GetAddressed(),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to add the review message")
	}

	return &agentv1.AddReviewMessageResponse{
		Message: render.ReviewMessage(added.Message),
		Hint:    added.Hint,
	}, nil
}

// EditReviewMessage rewrites what was said.
func (s *Handler) EditReviewMessage(ctx context.Context, req *agentv1.EditReviewMessageRequest) (*agentv1.EditReviewMessageResponse, *fail.Error) {
	message, err := s.review.EditReviewMessage(ctx, reviews.EditReviewMessageParams{
		ID:         req.GetId(),
		Body:       req.GetBody(),
		Suggestion: req.GetSuggestion(),
		Author:     reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to edit the review message")
	}

	return &agentv1.EditReviewMessageResponse{Message: render.ReviewMessage(*message)}, nil
}

// AcceptForgeMessage copies a held forge comment into the person's open review.
func (s *Handler) AcceptForgeMessage(ctx context.Context, req *agentv1.AcceptForgeMessageRequest) (*agentv1.AcceptForgeMessageResponse, *fail.Error) {
	message, err := s.review.AcceptForgeMessage(ctx, reviews.AcceptForgeMessageParams{
		MessageID: req.GetMessageId(),
		Author:    reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to accept the forge comment")
	}

	return &agentv1.AcceptForgeMessageResponse{Message: render.ReviewMessage(*message)}, nil
}

// MarkReviewThreadRead catches the calling side up on one conversation.
func (s *Handler) MarkReviewThreadRead(ctx context.Context, req *agentv1.MarkReviewThreadReadRequest) (*agentv1.MarkReviewThreadReadResponse, *fail.Error) {
	thread, err := s.review.MarkReviewThreadRead(ctx, reviews.MarkReviewThreadReadParams{
		ID:     req.GetId(),
		Author: reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to mark the review thread read")
	}

	return &agentv1.MarkReviewThreadReadResponse{
		Thread: render.ReviewThread(*thread, nil),
	}, nil
}

// SetReviewThreadResolved settles one conversation, or reopens it.
func (s *Handler) SetReviewThreadResolved(ctx context.Context, req *agentv1.SetReviewThreadResolvedRequest) (*agentv1.SetReviewThreadResolvedResponse, *fail.Error) {
	thread, err := s.review.SetReviewThreadResolved(ctx, reviews.SetReviewThreadResolvedParams{
		ID:       req.GetId(),
		Resolved: req.GetResolved(),
		Author:   reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to set the review thread resolved")
	}

	return &agentv1.SetReviewThreadResolvedResponse{
		Thread: render.ReviewThread(*thread, nil),
	}, nil
}

// OpenReviewRound freezes a task's checkout and records it as the task's next round.
func (s *Handler) OpenReviewRound(ctx context.Context, req *agentv1.OpenReviewRoundRequest) (*agentv1.OpenReviewRoundResponse, *fail.Error) {
	round, err := s.review.OpenRound(ctx, req.GetTask(), reviewAuthorFrom(ctx))
	if err != nil {
		return nil, rpcerr.From(err, "failed to open a review round")
	}

	return &agentv1.OpenReviewRoundResponse{Round: render.ReviewRound(*round)}, nil
}

// OpenReviewHistory opens a task's review as the panel draws it, freezing a round when none holds the worktree.
func (s *Handler) OpenReviewHistory(ctx context.Context, req *agentv1.OpenReviewHistoryRequest) (*agentv1.OpenReviewHistoryResponse, *fail.Error) {
	opened, err := s.review.GetHistory(ctx, reviews.GetHistoryParams{
		Task:   req.GetTask(),
		Fresh:  req.GetFresh(),
		Span:   historySpanOf(req),
		Author: reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to open the review history")
	}

	return render.ReviewHistory(*opened), nil
}

func historySpanOf(req *agentv1.OpenReviewHistoryRequest) history.Span {
	switch {
	case req.GetSnapshots() != nil:
		return history.Span{
			BaseSHA: req.GetSnapshots().GetBaseSha(),
			HeadSHA: req.GetSnapshots().GetHeadSha(),
		}
	case req.GetAnswer() != nil:
		return history.Span{Answer: &history.AnswerSpan{
			FromRoundID: req.GetAnswer().GetFromRoundId(),
			ToRoundID:   req.GetAnswer().GetToRoundId(),
		}}
	case req.GetOrigin() != nil:
		return history.Span{Origin: &history.OriginSpan{
			RoundID: req.GetOrigin().GetRoundId(),
			BaseSHA: req.GetOrigin().GetBaseSha(),
		}}
	}

	return history.Span{}
}

// ListReviewRounds reads every round taken of a task.
func (s *Handler) ListReviewRounds(ctx context.Context, req *agentv1.ListReviewRoundsRequest) (*agentv1.ListReviewRoundsResponse, *fail.Error) {
	rounds, err := s.review.ListRounds(ctx, req.GetTask(), reviewAuthorFrom(ctx))
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the review rounds")
	}

	answered := xslice.Map(rounds, render.ReviewRound)

	return &agentv1.ListReviewRoundsResponse{Rounds: answered}, nil
}

// ListReviews reads every review given on a task.
func (s *Handler) ListReviews(ctx context.Context, req *agentv1.ListReviewsRequest) (*agentv1.ListReviewsResponse, *fail.Error) {
	reviews, err := s.review.ListReviews(ctx, req.GetTask(), reviewAuthorFrom(ctx))
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the reviews")
	}

	answered := xslice.Map(reviews, render.Review)

	return &agentv1.ListReviewsResponse{Reviews: answered}, nil
}

// GetReview reads one released review whole.
func (s *Handler) GetReview(ctx context.Context, req *agentv1.GetReviewRequest) (*agentv1.GetReviewResponse, *fail.Error) {
	review, err := s.review.GetReview(ctx, req.GetReviewId(), reviewAuthorFrom(ctx))
	if err != nil {
		return nil, rpcerr.From(err, "failed to get the review")
	}

	return &agentv1.GetReviewResponse{
		Review:  render.Review(review.Review),
		Round:   render.ReviewRound(review.Round),
		Threads: xslice.Map(review.Threads, render.ReviewThreadInContext),
	}, nil
}

// ReleaseReview releases the caller's open review, writing it out and mailing the path.
func (s *Handler) ReleaseReview(ctx context.Context, req *agentv1.ReleaseReviewRequest) (*agentv1.ReleaseReviewResponse, *fail.Error) {
	result, err := s.review.ReleaseReview(ctx, reviews.ReleaseReviewParams{
		Task:    req.GetTask(),
		Verdict: fromReviewVerdict(req.GetVerdict()),
		Body:    req.GetBody(),
		Author:  reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to release the review")
	}

	return &agentv1.ReleaseReviewResponse{
		Review:   render.Review(result.Review),
		Comments: int32(result.Comments),
		Path:     result.Path,
	}, nil
}

// SetReviewFileViewed marks one file of a diff read, or drops the mark.
func (s *Handler) SetReviewFileViewed(ctx context.Context, req *agentv1.SetReviewFileViewedRequest) (*agentv1.SetReviewFileViewedResponse, *fail.Error) {
	viewed, err := s.review.SetReviewFileViewed(ctx, reviews.SetReviewFileViewedParams{
		Task:        req.GetTask(),
		FilePath:    req.GetFilePath(),
		Fingerprint: req.GetFingerprint(),
		Viewed:      req.GetViewed(),
		Author:      reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to set the file viewed")
	}

	answered := &agentv1.SetReviewFileViewedResponse{}
	if viewed != nil {
		answered.Viewed = render.ReviewViewedFile(*viewed)
	}

	return answered, nil
}

// ListReviewViewedFiles reads which files of a task's diff the reviewer marked read.
func (s *Handler) ListReviewViewedFiles(ctx context.Context, req *agentv1.ListReviewViewedFilesRequest) (*agentv1.ListReviewViewedFilesResponse, *fail.Error) {
	files, err := s.review.ListReviewViewedFiles(ctx, reviews.ListReviewViewedFilesParams{
		Task:   req.GetTask(),
		Author: reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the viewed files")
	}

	answered := xslice.Map(files, render.ReviewViewedFile)

	return &agentv1.ListReviewViewedFilesResponse{Viewed: answered}, nil
}

// reviewAuthorFrom reads who a call belongs to from the identity on it, so an agent cannot post
// as the reviewer by asking to.
func reviewAuthorFrom(ctx context.Context) reviews.ReviewAuthor {
	from := caller.From(ctx)
	if from.AgentID == "" {
		return reviews.ReviewAuthor{Kind: reviewstore.AuthorHuman}
	}

	agentID := from.AgentID

	return reviews.ReviewAuthor{Kind: reviewstore.AuthorAgent, AgentID: &agentID}
}

func fromReviewVerdict(verdict agentv1.ReviewVerdict) reviewstore.Verdict {
	switch verdict {
	case agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED:
		return reviewstore.VerdictApproved
	case agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED:
		return reviewstore.VerdictChangesRequested
	case agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED:
		return reviewstore.VerdictCommented
	case agentv1.ReviewVerdict_REVIEW_VERDICT_UNSPECIFIED:
	}

	return ""
}
