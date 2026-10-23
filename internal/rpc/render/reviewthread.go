package render

import (
	reviewstore "komrad/internal/store/review"
	"komrad/lib/dates"
	"komrad/lib/xslice"
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
		Id:              thread.ID.String(),
		TaskId:          thread.TaskID.String(),
		ReviewId:        formatOptionalID(thread.ReviewID),
		AuthorKind:      thread.AuthorKind,
		AuthorAgentId:   formatOptionalID(thread.AuthorAgentID),
		SnapshotId:      thread.SnapshotID.String(),
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
		Id:             message.ID.String(),
		ThreadId:       message.ThreadID.String(),
		AuthorKind:     message.AuthorKind,
		AuthorAgentId:  formatOptionalID(message.AuthorAgentID),
		ReviewId:       formatOptionalID(message.ReviewID),
		SnapshotId:     formatOptionalID(message.SnapshotID),
		Body:           message.Body,
		Suggestion:     message.Suggestion,
		Held:           message.Held,
		ReadSnapshotId: formatOptionalID(message.ReadSnapshotID),
		AcceptedFrom:   formatOptionalID(message.AcceptedFrom),
		CreatedAt:      dates.Stamp(message.CreatedAt),
		UpdatedAt:      dates.Stamp(message.UpdatedAt),
	}

	if at := message.ReadAt; at != nil {
		answered.ReadAt = dates.Stamp(*at)
	}
	if external := message.External; external != nil {
		answered.Forge = &agentv1.ForgeOrigin{
			Login:     external.Login,
			CommentId: external.CommentID.String(),
			EditedAt:  dates.StampPtr(external.EditedAt),
		}
	}

	return answered
}
