package diffview

import (
	"slices"

	"komrad/internal/ui/paint"
	agentv1 "komrad/proto/gen/agent/v1"
)

// ForgeTone is the tone that renders comments mirrored from the forge.
const ForgeTone = paint.ToneUserGithub

// holdsFailedForgeReply reports whether a thread has a reply the forge refused, which waits on the
// person to retry or discard it.
func holdsFailedForgeReply(thread *agentv1.ReviewThread) bool {
	return slices.ContainsFunc(thread.GetForgeReplies(), func(reply *agentv1.ForgeReply) bool {
		switch reply.GetState() {
		case agentv1.ForgeReplyState_FORGE_REPLY_STATE_FAILED:
			return true
		case agentv1.ForgeReplyState_FORGE_REPLY_STATE_UNSPECIFIED,
			agentv1.ForgeReplyState_FORGE_REPLY_STATE_APPROVED,
			agentv1.ForgeReplyState_FORGE_REPLY_STATE_SENDING,
			agentv1.ForgeReplyState_FORGE_REPLY_STATE_SENT:
		}

		return false
	})
}
