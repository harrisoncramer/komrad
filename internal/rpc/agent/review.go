package agent

import (
	"context"

	"weave/internal/caller"
	"weave/internal/fail"
	"weave/internal/reviews"
	"weave/internal/rpc/render"
	"weave/internal/rpcerr"
	reviewstore "weave/internal/store/review"
	"weave/lib/dates"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"
)

type reviewConversation interface {
	ListReviewThreads(
		ctx context.Context,
		params reviews.ListReviewThreadsParams,
	) ([]reviews.ReviewThreadWithMessages, error)
	CreateReviewThread(ctx context.Context, params reviews.CreateReviewThreadParams) (*reviews.ReviewThreadWithMessages, error)
	AddReviewMessage(ctx context.Context, params reviews.AddReviewMessageParams) (*reviewstore.Message, error)
	EditReviewMessage(ctx context.Context, params reviews.EditReviewMessageParams) (*reviewstore.Message, error)
	MarkReviewThreadRead(ctx context.Context, params reviews.MarkReviewThreadReadParams) (*reviewstore.Thread, error)
	SetReviewThreadResolved(ctx context.Context, params reviews.SetReviewThreadResolvedParams) (*reviewstore.Thread, error)
	OpenRound(ctx context.Context, agentID string, author reviews.ReviewAuthor) (*reviewstore.Round, error)
	ListRounds(ctx context.Context, agentID string, author reviews.ReviewAuthor) ([]reviewstore.Round, error)
	ListReviews(ctx context.Context, agentID string, author reviews.ReviewAuthor) ([]reviewstore.Review, error)
	GetReview(ctx context.Context, reviewID string, author reviews.ReviewAuthor) (*reviews.ReviewInFull, error)
	ReleaseReview(ctx context.Context, params reviews.ReleaseReviewParams) (*reviews.ReleaseReviewResult, error)
	SetReviewFileViewed(ctx context.Context, params reviews.SetReviewFileViewedParams) (*reviewstore.ViewedFile, error)
	ListReviewViewedFiles(ctx context.Context, params reviews.ListReviewViewedFilesParams) ([]reviewstore.ViewedFile, error)
}

// errUnnamedReview is a request to read a review without saying whose, from something that is
// not an agent with a review of its own.
var errUnnamedReview = fail.InvalidArgument("name the agent whose review you are reading")

// ListReviewThreads reads the review pinned to an agent's diff.
func (s *Handler) ListReviewThreads(ctx context.Context, req *agentv1.ListReviewThreadsRequest) (*agentv1.ListReviewThreadsResponse, *fail.Error) {
	author := reviewAuthorFrom(ctx)

	agentID := req.GetAgentId()
	if agentID == "" && author.AgentID != nil {
		agentID = *author.AgentID
	}
	if agentID == "" {
		return nil, errUnnamedReview
	}

	threads, err := s.review.ListReviewThreads(ctx, reviews.ListReviewThreadsParams{
		AgentID: agentID,
		Author:  author,
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the review threads")
	}

	answered := xslice.Map(threads, renderReviewThread)

	return &agentv1.ListReviewThreadsResponse{Threads: answered}, nil
}

// CreateReviewThread pins a conversation to a place in a diff and writes its first message.
func (s *Handler) CreateReviewThread(ctx context.Context, req *agentv1.CreateReviewThreadRequest) (*agentv1.CreateReviewThreadResponse, *fail.Error) {
	thread, err := s.review.CreateReviewThread(ctx, reviews.CreateReviewThreadParams{
		AgentID:     req.GetAgentId(),
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

	return &agentv1.CreateReviewThreadResponse{Thread: renderReviewThread(*thread)}, nil
}

// AddReviewMessage says something in an existing thread, as whoever is calling.
func (s *Handler) AddReviewMessage(ctx context.Context, req *agentv1.AddReviewMessageRequest) (*agentv1.AddReviewMessageResponse, *fail.Error) {
	message, err := s.review.AddReviewMessage(ctx, reviews.AddReviewMessageParams{
		ThreadID:   req.GetThreadId(),
		Body:       req.GetBody(),
		Suggestion: req.GetSuggestion(),
		Author:     reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to add the review message")
	}

	return &agentv1.AddReviewMessageResponse{Message: renderReviewMessage(*message)}, nil
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

	return &agentv1.EditReviewMessageResponse{Message: renderReviewMessage(*message)}, nil
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
		Thread: renderReviewThread(reviews.ReviewThreadWithMessages{Thread: *thread}),
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
		Thread: renderReviewThread(reviews.ReviewThreadWithMessages{Thread: *thread}),
	}, nil
}

// OpenReviewRound freezes what an agent has written and records it as its next round.
func (s *Handler) OpenReviewRound(ctx context.Context, req *agentv1.OpenReviewRoundRequest) (*agentv1.OpenReviewRoundResponse, *fail.Error) {
	round, err := s.review.OpenRound(ctx, req.GetAgentId(), reviewAuthorFrom(ctx))
	if err != nil {
		return nil, rpcerr.From(err, "failed to open a review round")
	}

	return &agentv1.OpenReviewRoundResponse{Round: renderReviewRound(*round)}, nil
}

// ListReviewRounds reads every round taken of an agent.
func (s *Handler) ListReviewRounds(ctx context.Context, req *agentv1.ListReviewRoundsRequest) (*agentv1.ListReviewRoundsResponse, *fail.Error) {
	rounds, err := s.review.ListRounds(ctx, req.GetAgentId(), reviewAuthorFrom(ctx))
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the review rounds")
	}

	answered := xslice.Map(rounds, renderReviewRound)

	return &agentv1.ListReviewRoundsResponse{Rounds: answered}, nil
}

// ListReviews reads every review given on an agent.
func (s *Handler) ListReviews(ctx context.Context, req *agentv1.ListReviewsRequest) (*agentv1.ListReviewsResponse, *fail.Error) {
	reviews, err := s.review.ListReviews(ctx, req.GetAgentId(), reviewAuthorFrom(ctx))
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the reviews")
	}

	answered := xslice.Map(reviews, renderReview)

	return &agentv1.ListReviewsResponse{Reviews: answered}, nil
}

// GetReview reads one released review whole.
func (s *Handler) GetReview(ctx context.Context, req *agentv1.GetReviewRequest) (*agentv1.GetReviewResponse, *fail.Error) {
	review, err := s.review.GetReview(ctx, req.GetReviewId(), reviewAuthorFrom(ctx))
	if err != nil {
		return nil, rpcerr.From(err, "failed to get the review")
	}

	return &agentv1.GetReviewResponse{
		Review:  renderReview(review.Review),
		Round:   renderReviewRound(review.Round),
		Threads: xslice.Map(review.Threads, renderReviewThreadInContext),
	}, nil
}

// ReleaseReview releases the caller's open review, writing it out and mailing the path.
func (s *Handler) ReleaseReview(ctx context.Context, req *agentv1.ReleaseReviewRequest) (*agentv1.ReleaseReviewResponse, *fail.Error) {
	result, err := s.review.ReleaseReview(ctx, reviews.ReleaseReviewParams{
		AgentID:   req.GetAgentId(),
		Verdict:   fromReviewVerdict(req.GetVerdict()),
		Body:      req.GetBody(),
		Interrupt: req.GetInterrupt(),
		Author:    reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to release the review")
	}

	return &agentv1.ReleaseReviewResponse{
		Review:   renderReview(result.Review),
		Comments: int32(result.Comments),
		Path:     result.Path,
	}, nil
}

// SetReviewFileViewed marks one file of a diff read, or drops the mark.
func (s *Handler) SetReviewFileViewed(ctx context.Context, req *agentv1.SetReviewFileViewedRequest) (*agentv1.SetReviewFileViewedResponse, *fail.Error) {
	viewed, err := s.review.SetReviewFileViewed(ctx, reviews.SetReviewFileViewedParams{
		AgentID:     req.GetAgentId(),
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
		answered.Viewed = renderReviewViewedFile(*viewed)
	}

	return answered, nil
}

// ListReviewViewedFiles reads which files of an agent's diff have been marked read.
func (s *Handler) ListReviewViewedFiles(ctx context.Context, req *agentv1.ListReviewViewedFilesRequest) (*agentv1.ListReviewViewedFilesResponse, *fail.Error) {
	files, err := s.review.ListReviewViewedFiles(ctx, reviews.ListReviewViewedFilesParams{
		AgentID: req.GetAgentId(),
		Author:  reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the viewed files")
	}

	answered := xslice.Map(files, renderReviewViewedFile)

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

func renderReviewThread(thread reviews.ReviewThreadWithMessages) *agentv1.ReviewThread {
	answered := &agentv1.ReviewThread{
		Id:            thread.Thread.ID,
		AgentId:       thread.Thread.AgentID,
		ReviewId:      thread.Thread.ReviewID,
		RoundId:       thread.Thread.RoundID,
		RoundOrdinal:  thread.Thread.RoundOrdinal,
		BaseSha:       thread.Thread.BaseSHA,
		FilePath:      thread.Thread.FilePath,
		OldStart:      thread.Thread.OldStart,
		OldEnd:        thread.Thread.OldEnd,
		NewStart:      thread.Thread.NewStart,
		NewEnd:        thread.Thread.NewEnd,
		QuotedLines:   thread.Thread.QuotedLines,
		UnreadByHuman: thread.Thread.UnreadByHuman,
		CreatedAt:     dates.Stamp(thread.Thread.CreatedAt),
		UpdatedAt:     dates.Stamp(thread.Thread.UpdatedAt),
		Messages:      xslice.Map(thread.Messages, renderReviewMessage),
	}

	if at := thread.Thread.ResolvedAt; at != nil {
		answered.ResolvedAt = dates.Stamp(*at)
	}

	return answered
}

func renderReviewThreadInContext(thread reviews.ReviewThreadInContext) *agentv1.ReviewThreadInContext {
	return &agentv1.ReviewThreadInContext{
		Thread: renderReviewThread(reviews.ReviewThreadWithMessages{
			Thread:   thread.Thread,
			Messages: thread.Messages,
		}),
		Context: thread.Context,
	}
}

func renderReviewMessage(message reviewstore.Message) *agentv1.ReviewMessage {
	kind := agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN
	if message.AuthorKind == reviewstore.AuthorAgent {
		kind = agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT
	}

	answered := &agentv1.ReviewMessage{
		Id:            message.ID,
		ThreadId:      message.ThreadID,
		AuthorKind:    kind,
		AuthorAgentId: message.AuthorAgentID,
		ReviewId:      message.ReviewID,
		Body:          message.Body,
		Suggestion:    message.Suggestion,
		Delivery:      renderReviewDeliveryState(message.Delivery),
		CreatedAt:     dates.Stamp(message.CreatedAt),
		UpdatedAt:     dates.Stamp(message.UpdatedAt),
	}

	if at := message.SentAt; at != nil {
		answered.SentAt = dates.Stamp(*at)
	}
	if at := message.DeliveredAt; at != nil {
		answered.DeliveredAt = dates.Stamp(*at)
	}

	return answered
}

func renderReviewDeliveryState(state reviewstore.DeliveryState) agentv1.ReviewDeliveryState {
	switch state {
	case reviewstore.DeliveryUnsent:
		return agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT
	case reviewstore.DeliverySent:
		return agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_SENT
	case reviewstore.DeliveryDelivered:
		return agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_DELIVERED
	case reviewstore.DeliveryNone:
		return agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSPECIFIED
	default:
		return agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSPECIFIED
	}
}

func renderReviewViewedFile(viewed reviewstore.ViewedFile) *agentv1.ReviewViewedFile {
	return &agentv1.ReviewViewedFile{
		AgentId:     viewed.AgentID,
		FilePath:    viewed.FilePath,
		Fingerprint: viewed.Fingerprint,
		ViewedAt:    dates.Stamp(viewed.ViewedAt),
	}
}

func renderReviewRound(round reviewstore.Round) *agentv1.ReviewRound {
	return &agentv1.ReviewRound{
		Id:          round.ID,
		AgentId:     round.AgentID,
		Ordinal:     round.Ordinal,
		BaseSha:     round.BaseSHA,
		SnapshotSha: round.SnapshotSHA,
		CreatedAt:   dates.Stamp(round.CreatedAt),
	}
}

func renderReview(review reviewstore.Review) *agentv1.Review {
	kind := agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN
	if review.AuthorKind == reviewstore.AuthorAgent {
		kind = agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT
	}

	answered := &agentv1.Review{
		Id:            review.ID,
		AgentId:       review.AgentID,
		RoundId:       review.RoundID,
		RoundOrdinal:  review.RoundOrdinal,
		AuthorKind:    kind,
		AuthorAgentId: review.AuthorAgentID,
		Body:          review.Body,
		Path:          review.Path,
		OpenThreads:   review.OpenThreads,
		CreatedAt:     dates.Stamp(review.CreatedAt),
		UpdatedAt:     dates.Stamp(review.UpdatedAt),
	}

	if review.Verdict != nil {
		answered.Verdict = render.ReviewVerdictOf(*review.Verdict)
	}
	if at := review.ReleasedAt; at != nil {
		answered.ReleasedAt = dates.Stamp(*at)
	}
	if at := review.DeliveredAt; at != nil {
		answered.DeliveredAt = dates.Stamp(*at)
	}

	return answered
}

func fromReviewVerdict(verdict agentv1.ReviewVerdict) reviewstore.Verdict {
	switch verdict {
	case agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED:
		return reviewstore.VerdictApproved
	case agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED:
		return reviewstore.VerdictChangesRequested
	case agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED,
		agentv1.ReviewVerdict_REVIEW_VERDICT_UNSPECIFIED:
	}

	return reviewstore.VerdictCommented
}
