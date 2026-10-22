package agent

import (
	"context"

	"komrad/internal/fail"
	"komrad/internal/reviews"
	"komrad/internal/rpc/render"
	"komrad/internal/rpcerr"
	reviewstore "komrad/internal/store/review"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1komrad"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/google/uuid"
)

type forgeReplier interface {
	ReplyOnForge(ctx context.Context, params reviews.ReplyOnForgeParams) (*reviewstore.ForgeReply, error)
	RetryForgeReply(ctx context.Context, id uuid.UUID, author reviews.ReviewAuthor) (*reviewstore.ForgeReply, error)
	DiscardForgeReply(ctx context.Context, id uuid.UUID, author reviews.ReviewAuthor) error
}

// ReplyOnForge posts the person's own reply on a mirrored thread.
func (s *Handler) ReplyOnForge(ctx context.Context, req *agentv1komrad.ReplyOnForgeRequest) (*agentv1.ReplyOnForgeResponse, *fail.Error) {
	reply, err := s.forgeReplies.ReplyOnForge(ctx, reviews.ReplyOnForgeParams{
		ThreadID: req.ThreadId,
		Author:   reviewAuthorFrom(ctx),
		Body:     req.Body,
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to reply on the forge")
	}

	return &agentv1.ReplyOnForgeResponse{Reply: render.ForgeReply(*reply)}, nil
}

// RetryForgeReply posts a failed reply again.
func (s *Handler) RetryForgeReply(ctx context.Context, req *agentv1komrad.RetryForgeReplyRequest) (*agentv1.RetryForgeReplyResponse, *fail.Error) {
	reply, err := s.forgeReplies.RetryForgeReply(ctx, req.Id, reviewAuthorFrom(ctx))
	if err != nil {
		return nil, rpcerr.From(err, "failed to retry the forge reply")
	}

	return &agentv1.RetryForgeReplyResponse{Reply: render.ForgeReply(*reply)}, nil
}

// DiscardForgeReply drops a failed reply without posting it.
func (s *Handler) DiscardForgeReply(ctx context.Context, req *agentv1komrad.DiscardForgeReplyRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.forgeReplies.DiscardForgeReply(ctx, req.Id, reviewAuthorFrom(ctx)); err != nil {
		return nil, rpcerr.From(err, "failed to discard the forge reply")
	}

	return &emptypb.Empty{}, nil
}
