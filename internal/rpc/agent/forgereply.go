package agent

import (
	"context"

	"komrad/internal/fail"
	"komrad/internal/reviews"
	"komrad/internal/rpc/render"
	"komrad/internal/rpcerr"
	reviewstore "komrad/internal/store/review"
	agentv1 "komrad/proto/gen/agent/v1"

	"google.golang.org/protobuf/types/known/emptypb"
)

type forgeReplier interface {
	ProposeForgeReply(ctx context.Context, params reviews.ProposeForgeReplyParams) (*reviewstore.ForgeReply, error)
	ReplyOnForge(ctx context.Context, params reviews.ReplyOnForgeParams) (*reviewstore.ForgeReply, error)
	ApproveForgeReply(ctx context.Context, params reviews.ApproveForgeReplyParams) (*reviewstore.ForgeReply, error)
	RetryForgeReply(ctx context.Context, id string, author reviews.ReviewAuthor) (*reviewstore.ForgeReply, error)
	DiscardForgeReply(ctx context.Context, id string, author reviews.ReviewAuthor) error
	RequestForgeReply(ctx context.Context, threadID string, author reviews.ReviewAuthor) error
}

// ProposeForgeReply drafts the calling agent's reply on a mirrored thread for the person to approve.
func (s *Handler) ProposeForgeReply(ctx context.Context, req *agentv1.ProposeForgeReplyRequest) (*agentv1.ProposeForgeReplyResponse, *fail.Error) {
	reply, err := s.forgeReplies.ProposeForgeReply(ctx, reviews.ProposeForgeReplyParams{
		ThreadID: req.GetThreadId(),
		Author:   reviewAuthorFrom(ctx),
		Body:     req.GetBody(),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to propose the forge reply")
	}

	return &agentv1.ProposeForgeReplyResponse{Reply: render.ForgeReply(*reply)}, nil
}

// ReplyOnForge posts the person's own reply on a mirrored thread.
func (s *Handler) ReplyOnForge(ctx context.Context, req *agentv1.ReplyOnForgeRequest) (*agentv1.ReplyOnForgeResponse, *fail.Error) {
	reply, err := s.forgeReplies.ReplyOnForge(ctx, reviews.ReplyOnForgeParams{
		ThreadID: req.GetThreadId(),
		Author:   reviewAuthorFrom(ctx),
		Body:     req.GetBody(),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to reply on the forge")
	}

	return &agentv1.ReplyOnForgeResponse{Reply: render.ForgeReply(*reply)}, nil
}

// ApproveForgeReply posts an agent's draft, with the person's edit when they made one.
func (s *Handler) ApproveForgeReply(ctx context.Context, req *agentv1.ApproveForgeReplyRequest) (*agentv1.ApproveForgeReplyResponse, *fail.Error) {
	reply, err := s.forgeReplies.ApproveForgeReply(ctx, reviews.ApproveForgeReplyParams{
		ID:     req.GetId(),
		Author: reviewAuthorFrom(ctx),
		Body:   req.Body,
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to approve the forge reply")
	}

	return &agentv1.ApproveForgeReplyResponse{Reply: render.ForgeReply(*reply)}, nil
}

// RetryForgeReply posts a failed reply again.
func (s *Handler) RetryForgeReply(ctx context.Context, req *agentv1.RetryForgeReplyRequest) (*agentv1.RetryForgeReplyResponse, *fail.Error) {
	reply, err := s.forgeReplies.RetryForgeReply(ctx, req.GetId(), reviewAuthorFrom(ctx))
	if err != nil {
		return nil, rpcerr.From(err, "failed to retry the forge reply")
	}

	return &agentv1.RetryForgeReplyResponse{Reply: render.ForgeReply(*reply)}, nil
}

// DiscardForgeReply drops a draft or failed reply without posting it.
func (s *Handler) DiscardForgeReply(ctx context.Context, req *agentv1.DiscardForgeReplyRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.forgeReplies.DiscardForgeReply(ctx, req.GetId(), reviewAuthorFrom(ctx)); err != nil {
		return nil, rpcerr.From(err, "failed to discard the forge reply")
	}

	return &emptypb.Empty{}, nil
}

// RequestForgeReply asks the agent behind a mirrored thread to draft a reply on it.
func (s *Handler) RequestForgeReply(ctx context.Context, req *agentv1.RequestForgeReplyRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.forgeReplies.RequestForgeReply(ctx, req.GetThreadId(), reviewAuthorFrom(ctx)); err != nil {
		return nil, rpcerr.From(err, "failed to request a forge reply")
	}

	return &emptypb.Empty{}, nil
}
