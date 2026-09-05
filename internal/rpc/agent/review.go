package agent

import (
	"context"
	"errors"

	"weave/internal/agents"
	"weave/internal/caller"
	"weave/internal/rpcerr"
	reviewstore "weave/internal/store/review"
	"weave/lib/dates"
	agentv1 "weave/proto/gen/agent/v1"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

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
		FilePath:    req.Msg.GetFilePath(),
		OldStart:    req.Msg.GetOldStart(),
		OldEnd:      req.Msg.GetOldEnd(),
		NewStart:    req.Msg.GetNewStart(),
		NewEnd:      req.Msg.GetNewEnd(),
		QuotedLines: req.Msg.GetQuotedLines(),
		Body:        req.Msg.GetBody(),
		Author:      reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to create the review thread")
	}

	return connect.NewResponse(&agentv1.CreateReviewThreadResponse{Thread: toReviewThread(*thread)}), nil
}

// AddReviewMessage says something in an existing thread, as whoever is calling.
func (s *Service) AddReviewMessage(
	ctx context.Context,
	req *connect.Request[agentv1.AddReviewMessageRequest],
) (*connect.Response[agentv1.AddReviewMessageResponse], error) {
	message, err := s.review.AddReviewMessage(ctx, agents.AddReviewMessageParams{
		ThreadID: req.Msg.GetThreadId(),
		Body:     req.Msg.GetBody(),
		Author:   reviewAuthorFrom(ctx),
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
		ID:     req.Msg.GetId(),
		Body:   req.Msg.GetBody(),
		Author: reviewAuthorFrom(ctx),
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

// DeleteReviewThread removes a thread and everything said in it.
func (s *Service) DeleteReviewThread(
	ctx context.Context,
	req *connect.Request[agentv1.DeleteReviewThreadRequest],
) (*connect.Response[emptypb.Empty], error) {
	err := s.review.DeleteReviewThread(ctx, agents.DeleteReviewThreadParams{
		ID:     req.Msg.GetId(),
		Author: reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to delete the review thread")
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// DeleteReviewMessage removes one message, leaving the thread it was in.
func (s *Service) DeleteReviewMessage(
	ctx context.Context,
	req *connect.Request[agentv1.DeleteReviewMessageRequest],
) (*connect.Response[emptypb.Empty], error) {
	err := s.review.DeleteReviewMessage(ctx, agents.DeleteReviewMessageParams{
		ID:     req.Msg.GetId(),
		Author: reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to delete the review message")
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// ReleaseReviewMessages lets go of the comments the person has written and not sent, raising a
// doorbell for each.
func (s *Service) ReleaseReviewMessages(
	ctx context.Context,
	req *connect.Request[agentv1.ReleaseReviewMessagesRequest],
) (*connect.Response[agentv1.ReleaseReviewMessagesResponse], error) {
	result, err := s.review.ReleaseReviewMessages(ctx, agents.ReleaseReviewMessagesParams{
		AgentID:   req.Msg.GetAgentId(),
		Interrupt: req.Msg.GetInterrupt(),
		Author:    reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to release the review")
	}

	return connect.NewResponse(&agentv1.ReleaseReviewMessagesResponse{
		Released: int32(result.Released),
	}), nil
}

// RingReviewMessagesAgain raises a doorbell again for the released comments whose own expired
// unclaimed.
func (s *Service) RingReviewMessagesAgain(
	ctx context.Context,
	req *connect.Request[agentv1.RingReviewMessagesAgainRequest],
) (*connect.Response[agentv1.RingReviewMessagesAgainResponse], error) {
	result, err := s.review.RingReviewMessagesAgain(ctx, agents.RingReviewMessagesAgainParams{
		AgentID:   req.Msg.GetAgentId(),
		Interrupt: req.Msg.GetInterrupt(),
		Author:    reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to ring the review again")
	}

	return connect.NewResponse(&agentv1.RingReviewMessagesAgainResponse{
		Rung: int32(result.Rung),
	}), nil
}

// SetReviewFileViewed marks one file of a diff read, or drops the mark.
func (s *Service) SetReviewFileViewed(
	ctx context.Context,
	req *connect.Request[agentv1.SetReviewFileViewedRequest],
) (*connect.Response[agentv1.SetReviewFileViewedResponse], error) {
	viewed, err := s.review.SetReviewFileViewed(ctx, agents.SetReviewFileViewedParams{
		AgentID:     req.Msg.GetAgentId(),
		FilePath:    req.Msg.GetFilePath(),
		Fingerprint: req.Msg.GetFingerprint(),
		Viewed:      req.Msg.GetViewed(),
		Author:      reviewAuthorFrom(ctx),
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
		AgentID: req.Msg.GetAgentId(),
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

// reviewAuthorFrom reads who a call belongs to from the identity on it, so an agent cannot
// post as the reviewer by asking to.
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
		FilePath:      thread.Thread.FilePath,
		OldStart:      thread.Thread.OldStart,
		OldEnd:        thread.Thread.OldEnd,
		NewStart:      thread.Thread.NewStart,
		NewEnd:        thread.Thread.NewEnd,
		QuotedLines:   thread.Thread.QuotedLines,
		UnreadByHuman: thread.Thread.UnreadByHuman,
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
	case reviewstore.DeliveryExpired:
		return agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_EXPIRED
	case reviewstore.DeliveryNone:
		return agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSPECIFIED
	default:
		return agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSPECIFIED
	}
}

func toReviewViewedFile(viewed reviewstore.ViewedFile) *agentv1.ReviewViewedFile {
	return &agentv1.ReviewViewedFile{
		AgentId:     viewed.AgentID,
		FilePath:    viewed.FilePath,
		Fingerprint: viewed.Fingerprint,
		ViewedAt:    dates.Stamp(viewed.ViewedAt),
	}
}
