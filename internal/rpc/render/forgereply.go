package render

import (
	"komrad/lib/dates"

	reviewstore "komrad/internal/store/review"
	agentv1 "komrad/proto/gen/agent/v1"
)

// ForgeReply draws a reply komrad posts to the forge on a mirrored thread.
func ForgeReply(reply reviewstore.ForgeReply) *agentv1.ForgeReply {
	drawn := &agentv1.ForgeReply{
		Id:             reply.ID,
		ThreadId:       reply.ThreadID,
		State:          forgeReplyStateOf(reply.State),
		Body:           reply.Body,
		ForgeCommentId: reply.MirroredCommentID,
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

func forgeReplyStateOf(state reviewstore.ForgeReplyState) agentv1.ForgeReplyState {
	switch state {
	case reviewstore.ForgeReplyApproved:
		return agentv1.ForgeReplyState_FORGE_REPLY_STATE_APPROVED
	case reviewstore.ForgeReplySending:
		return agentv1.ForgeReplyState_FORGE_REPLY_STATE_SENDING
	case reviewstore.ForgeReplySent:
		return agentv1.ForgeReplyState_FORGE_REPLY_STATE_SENT
	case reviewstore.ForgeReplyFailed:
		return agentv1.ForgeReplyState_FORGE_REPLY_STATE_FAILED
	}

	return agentv1.ForgeReplyState_FORGE_REPLY_STATE_UNSPECIFIED
}
