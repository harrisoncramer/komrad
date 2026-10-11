package ui

import (
	"komrad/internal/ui/paint"
	agentv1 "komrad/proto/gen/agent/v1"
)

// reviewDeliveryOrder is the states a comment can stand in, ordered by how much they are owed: what
// the forge holds back, what never went, what is waiting, what landed.
var reviewDeliveryOrder = []agentv1.ReviewDeliveryState{
	agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_HELD,
	agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT,
	agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_SENT,
	agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_DELIVERED,
}

// reviewDelivery is how one delivery state is rendered: what it is called, the glyph a badge opens
// with, and the colour both of those carry.
type reviewDelivery struct {
	word string
	tone paint.Tone
}

// reviewDeliveryOf is how a state is rendered wherever it appears, so the count at the top of the
// sidebar, the badge under a comment and the gutter's own mark read as one thing.
func reviewDeliveryOf(state agentv1.ReviewDeliveryState) reviewDelivery {
	switch state {
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT:
		return reviewDelivery{
			word: "unsent",
			tone: reviewUnsentTone,
		}
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_SENT:
		return reviewDelivery{
			word: "waiting",
			tone: reviewSentTone,
		}
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_DELIVERED:
		return reviewDelivery{
			word: "delivered",
			tone: reviewDeliveredTone,
		}
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_HELD:
		return reviewDelivery{
			word: "held",
			tone: reviewForgeHeldTone,
		}
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSPECIFIED:
		return reviewDelivery{}
	}

	return reviewDelivery{}
}

// reviewDeliveryStamp is the moment a badge names: the last thing that happened to the comment,
// not when it was written.
func reviewDeliveryStamp(message *agentv1.ReviewMessage) string {
	if delivered := message.GetDeliveredAt(); delivered != "" {
		return delivered
	}

	return message.GetSentAt()
}
