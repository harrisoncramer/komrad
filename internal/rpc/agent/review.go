package agent

import (
	"context"
	"errors"

	"weave/internal/agents"
	"weave/internal/caller"
	"weave/internal/review"
	"weave/internal/rpcerr"
	reviewstore "weave/internal/store/review"
	"weave/lib/dates"
	agentv1 "weave/proto/gen/agent/v1"

	"connectrpc.com/connect"
)

type reviewConversation interface {
	ListReviewThreads(
		ctx context.Context,
		params agents.ListReviewThreadsParams,
	) ([]agents.ReviewThreadWithMessages, error)
	CreateReviewThread(ctx context.Context, params agents.CreateReviewThreadParams) (*agents.ReviewThreadWithMessages, error)
	HarvestRoundEdits(ctx context.Context, params agents.HarvestRoundEditsParams) (*agents.HarvestResult, error)
	AddReviewMessage(ctx context.Context, params agents.AddReviewMessageParams) (*reviewstore.Message, error)
	EditReviewMessage(ctx context.Context, params agents.EditReviewMessageParams) (*reviewstore.Message, error)
	MarkReviewThreadRead(ctx context.Context, params agents.MarkReviewThreadReadParams) (*reviewstore.Thread, error)
	SetReviewThreadResolved(ctx context.Context, params agents.SetReviewThreadResolvedParams) (*reviewstore.Thread, error)
	OpenRound(ctx context.Context, agentID string, author agents.ReviewAuthor) (*reviewstore.Round, error)
	ListRounds(ctx context.Context, agentID string, author agents.ReviewAuthor) ([]reviewstore.Round, error)
	ListReviews(ctx context.Context, agentID string, author agents.ReviewAuthor) ([]reviewstore.Review, error)
	ListTallies(ctx context.Context) ([]reviewstore.Tally, error)
	ListLiveReviewers(ctx context.Context) ([]reviewstore.LiveReviewer, error)
	ReleaseReview(ctx context.Context, params agents.ReleaseReviewParams) (*agents.ReleaseReviewResult, error)
	SetReviewFileViewed(ctx context.Context, params agents.SetReviewFileViewedParams) (*reviewstore.ViewedFile, error)
	ListReviewViewedFiles(ctx context.Context, params agents.ListReviewViewedFilesParams) ([]reviewstore.ViewedFile, error)
}

// ListReviewThreads reads the review pinned to an agent's diff.
func (s *Service) ListReviewThreads(
	ctx context.Context,
	req *connect.Request[agentv1.ListReviewThreadsRequest],
) (*connect.Response[agentv1.ListReviewThreadsResponse], error) {
	author := reviewAuthorFrom(ctx)

	agentID := req.Msg.GetAgentId()
	if agentID == "" && author.AgentID != nil {
		agentID = *author.AgentID
	}
	if agentID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("name the agent whose review you are reading"))
	}

	threads, err := s.review.ListReviewThreads(ctx, agents.ListReviewThreadsParams{
		AgentID: agentID,
		Author:  author,
	})
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to list the review threads")
	}

	answered := make([]*agentv1.ReviewThread, 0, len(threads))
	for _, thread := range threads {
		answered = append(answered, toReviewThread(thread))
	}

	return connect.NewResponse(&agentv1.ListReviewThreadsResponse{Threads: answered}), nil
}

// CreateReviewThread pins a conversation to a place in a diff and writes its first message.
func (s *Service) CreateReviewThread(
	ctx context.Context,
	req *connect.Request[agentv1.CreateReviewThreadRequest],
) (*connect.Response[agentv1.CreateReviewThreadResponse], error) {
	thread, err := s.review.CreateReviewThread(ctx, agents.CreateReviewThreadParams{
		AgentID:     req.Msg.GetAgentId(),
		BaseSHA:     req.Msg.GetBaseSha(),
		SnapshotSHA: req.Msg.GetSnapshotSha(),
		FilePath:    req.Msg.GetFilePath(),
		OldStart:    req.Msg.GetOldStart(),
		OldEnd:      req.Msg.GetOldEnd(),
		NewStart:    req.Msg.GetNewStart(),
		NewEnd:      req.Msg.GetNewEnd(),
		QuotedLines: req.Msg.GetQuotedLines(),
		Body:        req.Msg.GetBody(),
		Suggestion:  req.Msg.GetSuggestion(),
		Author:      reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to create the review thread")
	}

	return connect.NewResponse(&agentv1.CreateReviewThreadResponse{Thread: toReviewThread(*thread)}), nil
}

// HarvestRoundEdits turns what the person reviewing wrote in the round's checkout into threads,
// reconciling against the threads an earlier harvest of the same round wrote.
func (s *Service) HarvestRoundEdits(
	ctx context.Context,
	req *connect.Request[agentv1.HarvestRoundEditsRequest],
) (*connect.Response[agentv1.HarvestRoundEditsResponse], error) {
	result, err := s.review.HarvestRoundEdits(ctx, agents.HarvestRoundEditsParams{
		AgentID: req.Msg.GetAgentId(),
		RoundID: req.Msg.GetRoundId(),
		Runs:    runsFrom(req.Msg.GetRuns()),
		BaseSHA: req.Msg.GetBaseSha(),
		Force:   req.Msg.GetForce(),
	})
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to harvest what was written in the round")
	}

	return connect.NewResponse(&agentv1.HarvestRoundEditsResponse{
		Opened:  int32(result.Opened),
		Moved:   int32(result.Moved),
		Deleted: int32(result.Deleted),
		Skipped: toHarvestedRuns(result.Skipped),
	}), nil
}

// runsFrom reads the runs the panel anchored, which arrive already measured against the round.
func runsFrom(runs []*agentv1.HarvestedRun) []review.Run {
	out := make([]review.Run, 0, len(runs))
	for _, run := range runs {
		out = append(out, review.Run{
			FilePath:    run.GetFilePath(),
			OldStart:    int(run.GetOldStart()),
			OldEnd:      int(run.GetOldEnd()),
			NewStart:    int(run.GetNewStart()),
			NewEnd:      int(run.GetNewEnd()),
			QuotedLines: run.GetQuotedLines(),
			Lines:       run.GetLines(),
		})
	}

	return out
}

func toHarvestedRuns(runs []review.Run) []*agentv1.HarvestedRun {
	out := make([]*agentv1.HarvestedRun, 0, len(runs))
	for _, run := range runs {
		out = append(out, &agentv1.HarvestedRun{
			FilePath:    run.FilePath,
			OldStart:    int32(run.OldStart),
			OldEnd:      int32(run.OldEnd),
			NewStart:    int32(run.NewStart),
			NewEnd:      int32(run.NewEnd),
			QuotedLines: run.QuotedLines,
			Lines:       run.Lines,
		})
	}

	return out
}

// AddReviewMessage says something in an existing thread, as whoever is calling.
func (s *Service) AddReviewMessage(
	ctx context.Context,
	req *connect.Request[agentv1.AddReviewMessageRequest],
) (*connect.Response[agentv1.AddReviewMessageResponse], error) {
	message, err := s.review.AddReviewMessage(ctx, agents.AddReviewMessageParams{
		ThreadID:   req.Msg.GetThreadId(),
		Body:       req.Msg.GetBody(),
		Suggestion: req.Msg.GetSuggestion(),
		Author:     reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to add the review message")
	}

	return connect.NewResponse(&agentv1.AddReviewMessageResponse{Message: toReviewMessage(*message)}), nil
}

// EditReviewMessage rewrites what was said.
func (s *Service) EditReviewMessage(
	ctx context.Context,
	req *connect.Request[agentv1.EditReviewMessageRequest],
) (*connect.Response[agentv1.EditReviewMessageResponse], error) {
	message, err := s.review.EditReviewMessage(ctx, agents.EditReviewMessageParams{
		ID:         req.Msg.GetId(),
		Body:       req.Msg.GetBody(),
		Suggestion: req.Msg.GetSuggestion(),
		Author:     reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to edit the review message")
	}

	return connect.NewResponse(&agentv1.EditReviewMessageResponse{Message: toReviewMessage(*message)}), nil
}

// MarkReviewThreadRead catches the calling side up on one conversation.
func (s *Service) MarkReviewThreadRead(
	ctx context.Context,
	req *connect.Request[agentv1.MarkReviewThreadReadRequest],
) (*connect.Response[agentv1.MarkReviewThreadReadResponse], error) {
	thread, err := s.review.MarkReviewThreadRead(ctx, agents.MarkReviewThreadReadParams{
		ID:     req.Msg.GetId(),
		Author: reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to mark the review thread read")
	}

	return connect.NewResponse(&agentv1.MarkReviewThreadReadResponse{
		Thread: toReviewThread(agents.ReviewThreadWithMessages{Thread: *thread}),
	}), nil
}

// SetReviewThreadResolved settles one conversation, or reopens it.
func (s *Service) SetReviewThreadResolved(
	ctx context.Context,
	req *connect.Request[agentv1.SetReviewThreadResolvedRequest],
) (*connect.Response[agentv1.SetReviewThreadResolvedResponse], error) {
	thread, err := s.review.SetReviewThreadResolved(ctx, agents.SetReviewThreadResolvedParams{
		ID:       req.Msg.GetId(),
		Resolved: req.Msg.GetResolved(),
		Author:   reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to set the review thread resolved")
	}

	return connect.NewResponse(&agentv1.SetReviewThreadResolvedResponse{
		Thread: toReviewThread(agents.ReviewThreadWithMessages{Thread: *thread}),
	}), nil
}

// OpenReviewRound freezes what an agent has written and records it as its next round.
func (s *Service) OpenReviewRound(
	ctx context.Context,
	req *connect.Request[agentv1.OpenReviewRoundRequest],
) (*connect.Response[agentv1.OpenReviewRoundResponse], error) {
	round, err := s.review.OpenRound(ctx, req.Msg.GetAgentId(), reviewAuthorFrom(ctx))
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to open a review round")
	}

	return connect.NewResponse(&agentv1.OpenReviewRoundResponse{Round: toReviewRound(*round)}), nil
}

// ListReviewRounds reads every round taken of an agent.
func (s *Service) ListReviewRounds(
	ctx context.Context,
	req *connect.Request[agentv1.ListReviewRoundsRequest],
) (*connect.Response[agentv1.ListReviewRoundsResponse], error) {
	rounds, err := s.review.ListRounds(ctx, req.Msg.GetAgentId(), reviewAuthorFrom(ctx))
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to list the review rounds")
	}

	answered := make([]*agentv1.ReviewRound, 0, len(rounds))
	for _, round := range rounds {
		answered = append(answered, toReviewRound(round))
	}

	return connect.NewResponse(&agentv1.ListReviewRoundsResponse{Rounds: answered}), nil
}

// ListReviews reads every review given on an agent.
func (s *Service) ListReviews(
	ctx context.Context,
	req *connect.Request[agentv1.ListReviewsRequest],
) (*connect.Response[agentv1.ListReviewsResponse], error) {
	reviews, err := s.review.ListReviews(ctx, req.Msg.GetAgentId(), reviewAuthorFrom(ctx))
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to list the reviews")
	}

	answered := make([]*agentv1.Review, 0, len(reviews))
	for _, review := range reviews {
		answered = append(answered, toReview(review))
	}

	return connect.NewResponse(&agentv1.ListReviewsResponse{Reviews: answered}), nil
}

// ReleaseReview releases the caller's open review, writing it out and mailing the path.
func (s *Service) ReleaseReview(
	ctx context.Context,
	req *connect.Request[agentv1.ReleaseReviewRequest],
) (*connect.Response[agentv1.ReleaseReviewResponse], error) {
	result, err := s.review.ReleaseReview(ctx, agents.ReleaseReviewParams{
		AgentID:   req.Msg.GetAgentId(),
		Verdict:   fromReviewVerdict(req.Msg.GetVerdict()),
		Body:      req.Msg.GetBody(),
		Interrupt: req.Msg.GetInterrupt(),
		Author:    reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to release the review")
	}

	return connect.NewResponse(&agentv1.ReleaseReviewResponse{
		Review:   toReview(result.Review),
		Comments: int32(result.Comments),
		Path:     result.Path,
	}), nil
}

// SetReviewFileViewed marks one file of a diff read, or drops the mark.
func (s *Service) SetReviewFileViewed(
	ctx context.Context,
	req *connect.Request[agentv1.SetReviewFileViewedRequest],
) (*connect.Response[agentv1.SetReviewFileViewedResponse], error) {
	viewed, err := s.review.SetReviewFileViewed(ctx, agents.SetReviewFileViewedParams{
		RoundID:  req.Msg.GetRoundId(),
		FilePath: req.Msg.GetFilePath(),
		Viewed:   req.Msg.GetViewed(),
		Author:   reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to set the file viewed")
	}

	answered := &agentv1.SetReviewFileViewedResponse{}
	if viewed != nil {
		answered.Viewed = toReviewViewedFile(*viewed)
	}

	return connect.NewResponse(answered), nil
}

// ListReviewViewedFiles reads which files of an agent's diff have been marked read.
func (s *Service) ListReviewViewedFiles(
	ctx context.Context,
	req *connect.Request[agentv1.ListReviewViewedFilesRequest],
) (*connect.Response[agentv1.ListReviewViewedFilesResponse], error) {
	files, err := s.review.ListReviewViewedFiles(ctx, agents.ListReviewViewedFilesParams{
		RoundID: req.Msg.GetRoundId(),
		Author:  reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to list the viewed files")
	}

	answered := make([]*agentv1.ReviewViewedFile, 0, len(files))
	for _, file := range files {
		answered = append(answered, toReviewViewedFile(file))
	}

	return connect.NewResponse(&agentv1.ListReviewViewedFilesResponse{Viewed: answered}), nil
}

// reviewAuthorFrom reads who a call belongs to from the identity on it, so an agent cannot post
// as the reviewer by asking to.
func reviewAuthorFrom(ctx context.Context) agents.ReviewAuthor {
	from := caller.From(ctx)
	if from.AgentID == "" {
		return agents.ReviewAuthor{Kind: reviewstore.AuthorHuman}
	}

	agentID := from.AgentID

	return agents.ReviewAuthor{Kind: reviewstore.AuthorAgent, AgentID: &agentID}
}

func toReviewThread(thread agents.ReviewThreadWithMessages) *agentv1.ReviewThread {
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
		Harvested:     thread.Thread.Harvested,
		CreatedAt:     dates.Stamp(thread.Thread.CreatedAt),
		UpdatedAt:     dates.Stamp(thread.Thread.UpdatedAt),
	}

	if at := thread.Thread.ResolvedAt; at != nil {
		answered.ResolvedAt = dates.Stamp(*at)
	}

	for _, message := range thread.Messages {
		answered.Messages = append(answered.Messages, toReviewMessage(message))
	}

	return answered
}

func toReviewMessage(message reviewstore.Message) *agentv1.ReviewMessage {
	kind := agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN
	if message.AuthorKind == reviewstore.AuthorAgent {
		kind = agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT
	}

	answered := &agentv1.ReviewMessage{
		Id:            message.ID,
		ThreadId:      message.ThreadID,
		AuthorKind:    kind,
		AuthorAgentId: message.AuthorAgentID,
		Body:          message.Body,
		Suggestion:    message.Suggestion,
		Delivery:      toReviewDeliveryState(message.Delivery),
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

func toReviewDeliveryState(state reviewstore.DeliveryState) agentv1.ReviewDeliveryState {
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

func toReviewViewedFile(viewed reviewstore.ViewedFile) *agentv1.ReviewViewedFile {
	return &agentv1.ReviewViewedFile{
		RoundId:  viewed.RoundID,
		FilePath: viewed.FilePath,
		ViewedAt: dates.Stamp(viewed.ViewedAt),
	}
}

func toReviewRound(round reviewstore.Round) *agentv1.ReviewRound {
	return &agentv1.ReviewRound{
		Id:          round.ID,
		AgentId:     round.AgentID,
		Ordinal:     round.Ordinal,
		BaseSha:     round.BaseSHA,
		SnapshotSha: round.SnapshotSHA,
		CreatedAt:   dates.Stamp(round.CreatedAt),
	}
}

func toReview(review reviewstore.Review) *agentv1.Review {
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
		CreatedAt:     dates.Stamp(review.CreatedAt),
		UpdatedAt:     dates.Stamp(review.UpdatedAt),
	}

	if review.Verdict != nil {
		answered.Verdict = toReviewVerdict(*review.Verdict)
	}
	if at := review.ReleasedAt; at != nil {
		answered.ReleasedAt = dates.Stamp(*at)
	}
	if at := review.DeliveredAt; at != nil {
		answered.DeliveredAt = dates.Stamp(*at)
	}

	return answered
}

func toReviewVerdict(verdict reviewstore.Verdict) agentv1.ReviewVerdict {
	switch verdict {
	case reviewstore.VerdictApproved:
		return agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED
	case reviewstore.VerdictChangesRequested:
		return agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED
	case reviewstore.VerdictCommented:
		return agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED
	default:
		return agentv1.ReviewVerdict_REVIEW_VERDICT_UNSPECIFIED
	}
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
