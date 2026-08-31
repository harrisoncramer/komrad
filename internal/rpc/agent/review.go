package agent

import (
	"context"
	"errors"
	"time"

	"weave/internal/agents"
	"weave/internal/caller"
	"weave/internal/rpcerr"
	reviewstore "weave/internal/store/review"
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
		AgentID:  agentID,
		MarkRead: req.Msg.GetMarkRead(),
		Author:   author,
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

// NotifyReviewThreads tells an agent its diff has been reviewed.
func (s *Service) NotifyReviewThreads(
	ctx context.Context,
	req *connect.Request[agentv1.NotifyReviewThreadsRequest],
) (*connect.Response[agentv1.NotifyReviewThreadsResponse], error) {
	result, err := s.review.NotifyReviewThreads(ctx, agents.NotifyReviewThreadsParams{
		AgentID:   req.Msg.GetAgentId(),
		Interrupt: req.Msg.GetInterrupt(),
		Author:    reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to notify the review threads")
	}

	return connect.NewResponse(&agentv1.NotifyReviewThreadsResponse{
		Unread:         int32(result.Unread),
		AlreadyWaiting: result.AlreadyWaiting,
	}), nil
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
		UnreadByAgent: thread.Thread.UnreadByAgent,
		UnreadByHuman: thread.Thread.UnreadByHuman,
		CreatedAt:     thread.Thread.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     thread.Thread.UpdatedAt.Format(time.RFC3339),
	}

	if at := thread.Thread.ResolvedAt; at != nil {
		answered.ResolvedAt = at.Format(time.RFC3339)
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

	return &agentv1.ReviewMessage{
		Id:            message.ID,
		ThreadId:      message.ThreadID,
		AuthorKind:    kind,
		AuthorAgentId: message.AuthorAgentID,
		Body:          message.Body,
		CreatedAt:     message.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     message.UpdatedAt.Format(time.RFC3339),
	}
}
