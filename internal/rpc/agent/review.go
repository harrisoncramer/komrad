package agent

import (
	"context"

	"komrad/internal/caller"
	"komrad/internal/fail"
	"komrad/internal/reviews"
	"komrad/internal/reviews/history"
	"komrad/internal/rpc/render"
	"komrad/internal/rpcerr"
	mailstore "komrad/internal/store/mail"
	reviewstore "komrad/internal/store/review"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1komrad"
	"komrad/proto/gen/oneofs"
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
	TakeSnapshot(ctx context.Context, task oneofs.Task, author reviews.ReviewAuthor) (*reviewstore.Snapshot, error)
	ReadMail(ctx context.Context, agentID string) ([]mailstore.Mail, error)
	ListSnapshots(ctx context.Context, task oneofs.Task, author reviews.ReviewAuthor) ([]reviewstore.Snapshot, error)
	GetHistory(ctx context.Context, params reviews.GetHistoryParams) (*reviews.ReviewHistory, error)
	ListReviews(ctx context.Context, task oneofs.Task, author reviews.ReviewAuthor) ([]reviewstore.Review, error)
	GetReview(ctx context.Context, reviewID string, author reviews.ReviewAuthor) (*reviews.ReviewInFull, error)
	ReleaseReview(ctx context.Context, params reviews.ReleaseReviewParams) (*reviews.ReleaseReviewResult, error)
	SetReviewFileViewed(ctx context.Context, params reviews.SetReviewFileViewedParams) (*reviewstore.ViewedFile, error)
	ListReviewViewedFiles(ctx context.Context, params reviews.ListReviewViewedFilesParams) ([]reviewstore.ViewedFile, error)
}

// ListReviewThreads reads the review pinned to a task's diff.
func (s *Handler) ListReviewThreads(ctx context.Context, req *agentv1komrad.ListReviewThreadsRequest) (*agentv1.ListReviewThreadsResponse, *fail.Error) {
	threads, err := s.review.ListReviewThreads(ctx, reviews.ListReviewThreadsParams{
		Task:   req.Task,
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
func (s *Handler) CreateReviewThread(ctx context.Context, req *agentv1komrad.CreateReviewThreadRequest) (*agentv1.CreateReviewThreadResponse, *fail.Error) {
	thread, err := s.review.CreateReviewThread(ctx, reviews.CreateReviewThreadParams{
		Task:        req.Task,
		BaseSHA:     req.BaseSha,
		SnapshotSHA: req.SnapshotSha,
		FilePath:    req.FilePath,
		OldStart:    req.OldStart,
		OldEnd:      req.OldEnd,
		NewStart:    req.NewStart,
		NewEnd:      req.NewEnd,
		QuotedLines: req.QuotedLines,
		Body:        req.Body,
		Suggestion:  req.Suggestion,
		Author:      reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to create the review thread")
	}

	return &agentv1.CreateReviewThreadResponse{Thread: render.ReviewThread(thread.Thread, thread.Messages)}, nil
}

// AddReviewMessage says something in an existing thread, as whoever is calling.
func (s *Handler) AddReviewMessage(ctx context.Context, req *agentv1komrad.AddReviewMessageRequest) (*agentv1.AddReviewMessageResponse, *fail.Error) {
	added, err := s.review.AddReviewMessage(ctx, reviews.AddReviewMessageParams{
		ThreadID:   req.ThreadId.String(),
		Body:       req.Body,
		Suggestion: req.Suggestion,
		Author:     reviewAuthorFrom(ctx),
		Addressed:  req.Addressed,
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
func (s *Handler) EditReviewMessage(ctx context.Context, req *agentv1komrad.EditReviewMessageRequest) (*agentv1.EditReviewMessageResponse, *fail.Error) {
	message, err := s.review.EditReviewMessage(ctx, reviews.EditReviewMessageParams{
		ID:         req.Id.String(),
		Body:       req.Body,
		Suggestion: req.Suggestion,
		Author:     reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to edit the review message")
	}

	return &agentv1.EditReviewMessageResponse{Message: render.ReviewMessage(*message)}, nil
}

// AcceptForgeMessage copies a held forge comment into the person's open review.
func (s *Handler) AcceptForgeMessage(ctx context.Context, req *agentv1komrad.AcceptForgeMessageRequest) (*agentv1.AcceptForgeMessageResponse, *fail.Error) {
	message, err := s.review.AcceptForgeMessage(ctx, reviews.AcceptForgeMessageParams{
		MessageID: req.MessageId.String(),
		Author:    reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to accept the forge comment")
	}

	return &agentv1.AcceptForgeMessageResponse{Message: render.ReviewMessage(*message)}, nil
}

// MarkReviewThreadRead catches the calling side up on one conversation.
func (s *Handler) MarkReviewThreadRead(ctx context.Context, req *agentv1komrad.MarkReviewThreadReadRequest) (*agentv1.MarkReviewThreadReadResponse, *fail.Error) {
	thread, err := s.review.MarkReviewThreadRead(ctx, reviews.MarkReviewThreadReadParams{
		ID:     req.Id.String(),
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
func (s *Handler) SetReviewThreadResolved(ctx context.Context, req *agentv1komrad.SetReviewThreadResolvedRequest) (*agentv1.SetReviewThreadResolvedResponse, *fail.Error) {
	thread, err := s.review.SetReviewThreadResolved(ctx, reviews.SetReviewThreadResolvedParams{
		ID:       req.Id.String(),
		Resolved: req.Resolved,
		Author:   reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to set the review thread resolved")
	}

	return &agentv1.SetReviewThreadResolvedResponse{
		Thread: render.ReviewThread(*thread, nil),
	}, nil
}

// TakeReviewSnapshot freezes a task's checkout and records it as the task's next snapshot.
func (s *Handler) TakeReviewSnapshot(ctx context.Context, req *agentv1komrad.TakeReviewSnapshotRequest) (*agentv1.TakeReviewSnapshotResponse, *fail.Error) {
	snapshot, err := s.review.TakeSnapshot(ctx, req.Task, reviewAuthorFrom(ctx))
	if err != nil {
		return nil, rpcerr.From(err, "failed to take a review snapshot")
	}

	return &agentv1.TakeReviewSnapshotResponse{Snapshot: render.ReviewSnapshot(*snapshot)}, nil
}

// OpenReviewHistory opens a task's review as the panel draws it, freezing a snapshot when none holds the worktree.
func (s *Handler) OpenReviewHistory(ctx context.Context, req *agentv1komrad.OpenReviewHistoryRequest) (*agentv1.OpenReviewHistoryResponse, *fail.Error) {
	opened, err := s.review.GetHistory(ctx, reviews.GetHistoryParams{
		Task:   req.Task,
		Span:   historySpanOf(req),
		Author: reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to open the review history")
	}

	return render.ReviewHistory(*opened), nil
}

func historySpanOf(req *agentv1komrad.OpenReviewHistoryRequest) history.Span {
	switch span := req.Span.(type) {
	case *agentv1komrad.OpenReviewHistoryRequest_Snapshots:
		return history.Span{Snapshots: &history.SnapshotSpan{
			FromSnapshotID: xptr.Deref(optionalID(span.Snapshots.FromSnapshotId)),
			ToSnapshotID:   xptr.Deref(optionalID(span.Snapshots.ToSnapshotId)),
		}}
	case *agentv1komrad.OpenReviewHistoryRequest_Answer:
		return history.Span{Answer: &history.AnswerSpan{
			FromSnapshotID: span.Answer.FromSnapshotId.String(),
			ToSnapshotID:   span.Answer.ToSnapshotId.String(),
		}}
	case *agentv1komrad.OpenReviewHistoryRequest_Origin:
		return history.Span{Origin: &history.OriginSpan{
			SnapshotID: span.Origin.SnapshotId.String(),
			BaseSHA:    span.Origin.BaseSha,
		}}
	}

	return history.Span{}
}

// ListReviewSnapshots reads every snapshot taken of a task.
func (s *Handler) ListReviewSnapshots(ctx context.Context, req *agentv1komrad.ListReviewSnapshotsRequest) (*agentv1.ListReviewSnapshotsResponse, *fail.Error) {
	snapshots, err := s.review.ListSnapshots(ctx, req.Task, reviewAuthorFrom(ctx))
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the review snapshots")
	}

	answered := xslice.Map(snapshots, render.ReviewSnapshot)

	return &agentv1.ListReviewSnapshotsResponse{Snapshots: answered}, nil
}

// ListReviews reads every review given on a task.
func (s *Handler) ListReviews(ctx context.Context, req *agentv1komrad.ListReviewsRequest) (*agentv1.ListReviewsResponse, *fail.Error) {
	reviews, err := s.review.ListReviews(ctx, req.Task, reviewAuthorFrom(ctx))
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the reviews")
	}

	answered := xslice.Map(reviews, render.Review)

	return &agentv1.ListReviewsResponse{Reviews: answered}, nil
}

// GetReview reads one released review whole.
func (s *Handler) GetReview(ctx context.Context, req *agentv1komrad.GetReviewRequest) (*agentv1.GetReviewResponse, *fail.Error) {
	review, err := s.review.GetReview(ctx, req.ReviewId.String(), reviewAuthorFrom(ctx))
	if err != nil {
		return nil, rpcerr.From(err, "failed to get the review")
	}

	return &agentv1.GetReviewResponse{
		Review:   render.Review(review.Review),
		Snapshot: render.ReviewSnapshot(review.Snapshot),
		Threads:  xslice.Map(review.Threads, render.ReviewThreadInContext),
	}, nil
}

// ReleaseReview releases the caller's open review, writing it out and mailing the path.
func (s *Handler) ReleaseReview(ctx context.Context, req *agentv1komrad.ReleaseReviewRequest) (*agentv1.ReleaseReviewResponse, *fail.Error) {
	result, err := s.review.ReleaseReview(ctx, reviews.ReleaseReviewParams{
		Task:    req.Task,
		Verdict: fromReviewVerdict(req.Verdict),
		Body:    req.Body,
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
func (s *Handler) SetReviewFileViewed(ctx context.Context, req *agentv1komrad.SetReviewFileViewedRequest) (*agentv1.SetReviewFileViewedResponse, *fail.Error) {
	viewed, err := s.review.SetReviewFileViewed(ctx, reviews.SetReviewFileViewedParams{
		Task:        req.Task,
		FilePath:    req.FilePath,
		Fingerprint: req.Fingerprint,
		Viewed:      req.Viewed,
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
func (s *Handler) ListReviewViewedFiles(ctx context.Context, req *agentv1komrad.ListReviewViewedFilesRequest) (*agentv1.ListReviewViewedFilesResponse, *fail.Error) {
	files, err := s.review.ListReviewViewedFiles(ctx, reviews.ListReviewViewedFilesParams{
		Task:   req.Task,
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
