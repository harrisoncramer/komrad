package render

import (
	"komrade/lib/dates"
	"komrade/lib/xslice"

	reviewstore "komrade/internal/store/review"
	agentv1 "komrade/proto/gen/agent/v1"
)

// WatchedReviewThread draws a thread as the watch carries it.
func WatchedReviewThread(thread reviewstore.ThreadWithMessages) *agentv1.ReviewThread {
	return ReviewThread(thread.Thread, thread.Messages)
}

// ReviewThread draws a thread with everything said in it.
func ReviewThread(thread reviewstore.Thread, messages []reviewstore.Message) *agentv1.ReviewThread {
	answered := &agentv1.ReviewThread{
		Id:            thread.ID,
		AgentId:       thread.AgentID,
		ReviewId:      thread.ReviewID,
		RoundId:       thread.RoundID,
		RoundOrdinal:  thread.RoundOrdinal,
		BaseSha:       thread.BaseSHA,
		FilePath:      thread.FilePath,
		OldStart:      thread.OldStart,
		OldEnd:        thread.OldEnd,
		NewStart:      thread.NewStart,
		NewEnd:        thread.NewEnd,
		QuotedLines:   thread.QuotedLines,
		UnreadByHuman: thread.UnreadByHuman,
		CreatedAt:     dates.Stamp(thread.CreatedAt),
		UpdatedAt:     dates.Stamp(thread.UpdatedAt),
		Messages:      xslice.Map(messages, ReviewMessage),
	}

	if at := thread.ResolvedAt; at != nil {
		answered.ResolvedAt = dates.Stamp(*at)
	}

	return answered
}

// ReviewMessage draws one thing said in a thread.
func ReviewMessage(message reviewstore.Message) *agentv1.ReviewMessage {
	kind := agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN
	if message.AuthorKind == reviewstore.AuthorAgent {
		kind = agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT
	}

	answered := &agentv1.ReviewMessage{
		Id:            message.ID,
		ThreadId:      message.ThreadID,
		AuthorKind:    kind,
		AuthorAgentId: message.AuthorAgentID,
		ReviewId:      message.ReviewID,
		Body:          message.Body,
		Suggestion:    message.Suggestion,
		Delivery:      reviewDeliveryStateOf(message.Delivery),
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

func reviewDeliveryStateOf(state reviewstore.DeliveryState) agentv1.ReviewDeliveryState {
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
