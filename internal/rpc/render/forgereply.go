package render

import (
	reviewstore "komrad/internal/store/review"
	"komrad/lib/dates"
	agentv1 "komrad/proto/gen/agent/v1"
)

// ForgeReply draws a reply komrad posts to the forge on a mirrored thread.
func ForgeReply(reply reviewstore.ForgeReply) *agentv1.ForgeReply {
	drawn := &agentv1.ForgeReply{
		Id:             reply.ID.String(),
		ThreadId:       reply.ThreadID.String(),
		State:          reply.State,
		Body:           reply.Body,
		ForgeCommentId: formatOptionalID(reply.MirroredCommentID),
		Error:          reply.Error,
		CreatedAt:      dates.Stamp(reply.CreatedAt),
	}
	if at := reply.ApprovedAt; at != nil {
		drawn.ApprovedAt = dates.Stamp(*at)
	}
	if at := reply.SendingStartedAt; at != nil {
		drawn.SendingStartedAt = dates.Stamp(*at)
	}
	if at := reply.SentAt; at != nil {
		drawn.SentAt = dates.Stamp(*at)
	}

	return drawn
}
