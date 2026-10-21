package render

import (
	"komrad/lib/dates"
	"komrad/lib/xslice"

	reviewstore "komrad/internal/store/review"
	agentv1 "komrad/proto/gen/agent/v1"
)

// WatchedReviewThread draws a thread as the watch carries it.
func WatchedReviewThread(thread reviewstore.ThreadWithMessages) *agentv1.ReviewThread {
	drawn := ReviewThread(thread.Thread, thread.Messages)
	drawn.ForgeReplies = xslice.Map(thread.ForgeReplies, ForgeReply)

	return drawn
}

// ReviewThread draws a thread with everything said in it.
func ReviewThread(thread reviewstore.Thread, messages []reviewstore.Message) *agentv1.ReviewThread {
	answered := &agentv1.ReviewThread{
		Id:              thread.ID,
		TaskId:          thread.TaskID,
		ReviewId:        thread.ReviewID,
		AuthorKind:      reviewAuthorKindOf(thread.AuthorKind),
		AuthorAgentId:   thread.AuthorAgentID,
		SnapshotId:      thread.SnapshotID,
		SnapshotOrdinal: thread.SnapshotOrdinal,
		BaseSha:         thread.BaseSHA,
		FilePath:        thread.FilePath,
		OldStart:        thread.OldStart,
		OldEnd:          thread.OldEnd,
		NewStart:        thread.NewStart,
		NewEnd:          thread.NewEnd,
		QuotedLines:     thread.QuotedLines,
		UnreadByHuman:   thread.UnreadByHuman,
		MirrorsForge:    thread.ForgeThreadID != nil,
		CreatedAt:       dates.Stamp(thread.CreatedAt),
		UpdatedAt:       dates.Stamp(thread.UpdatedAt),
		Messages:        xslice.Map(messages, ReviewMessage),
	}

	if at := thread.ResolvedAt; at != nil {
		answered.ResolvedAt = dates.Stamp(*at)
	}

	return answered
}

// ReviewMessage draws one thing said in a thread.
func ReviewMessage(message reviewstore.Message) *agentv1.ReviewMessage {
	answered := &agentv1.ReviewMessage{
		Id:             message.ID,
		ThreadId:       message.ThreadID,
		AuthorKind:     reviewAuthorKindOf(message.AuthorKind),
		AuthorAgentId:  message.AuthorAgentID,
		ReviewId:       message.ReviewID,
		SnapshotId:     message.SnapshotID,
		Body:           message.Body,
		Suggestion:     message.Suggestion,
		Held:           message.Held,
		ReadSnapshotId: message.ReadSnapshotID,
		AcceptedFrom:   message.AcceptedFrom,
		CreatedAt:      dates.Stamp(message.CreatedAt),
		UpdatedAt:      dates.Stamp(message.UpdatedAt),
	}

	if at := message.ReadAt; at != nil {
		answered.ReadAt = dates.Stamp(*at)
	}
	if external := message.External; external != nil {
		answered.Forge = &agentv1.ForgeOrigin{
			Login:     external.Login,
			CommentId: external.CommentID,
			EditedAt:  dates.StampPtr(external.EditedAt),
		}
	}

	return answered
}
