package ui

import (
	"fmt"
	"strings"

	"komrad/internal/ui/paint"
	agentv1 "komrad/proto/gen/agent/v1"
)

// reviewDeliveryOrder is the three states a comment can stand in, ordered by how much they are
// owed: what never went, what is waiting, what landed.
var reviewDeliveryOrder = []agentv1.ReviewDeliveryState{
	agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT,
	agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_SENT,
	agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_DELIVERED,
}

// reviewDelivery is how one delivery state is drawn: what it is called, the glyph a badge opens
// with, and the colour both of those carry.
type reviewDelivery struct {
	word string
	mark string
	tone paint.Tone
}

// reviewDeliveryOf is how a state is drawn wherever it appears, so the count at the top of the
// sidebar, the badge under a comment and the gutter's own mark read as one thing.
func reviewDeliveryOf(state agentv1.ReviewDeliveryState) reviewDelivery {
	switch state {
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT:
		return reviewDelivery{
			word: "unsent",
			mark: "◇",
			tone: reviewUnsentTone,
		}
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_SENT:
		return reviewDelivery{
			word: "waiting",
			mark: "→",
			tone: reviewSentTone,
		}
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_DELIVERED:
		return reviewDelivery{
			word: "delivered",
			mark: "✓",
			tone: reviewDeliveredTone,
		}
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSPECIFIED:
		return reviewDelivery{}
	}

	return reviewDelivery{}
}

// reviewDeliveryBadge is how far one comment has got towards being read, drawn under the comment
// itself and not at all for what the agent wrote.
func reviewDeliveryBadge(palette paint.Palette, message *agentv1.ReviewMessage) string {
	drawn := reviewDeliveryOf(message.GetDelivery())
	if drawn.word == "" {
		return ""
	}

	badge := drawn.mark + " " + drawn.word
	if when := reviewTimeOf(reviewDeliveryStamp(message)); when != "" {
		badge += " " + when
	}

	return palette.Style(drawn.tone).Render("  "+badge) + "\n"
}

// reviewDeliveryStamp is the moment a badge names: the last thing that happened to the comment,
// not when it was written.
func reviewDeliveryStamp(message *agentv1.ReviewMessage) string {
	if delivered := message.GetDeliveredAt(); delivered != "" {
		return delivered
	}

	return message.GetSentAt()
}

// deliveryCountView is how the person's own comments stand, counted rather than summarised.
func (t reviewThreads) deliveryCountView() string {
	parts := make([]string, 0, len(reviewDeliveryOrder))
	for _, state := range reviewDeliveryOrder {
		count := t.delivery[deliverySlot(state)]
		if count == 0 {
			continue
		}

		drawn := reviewDeliveryOf(state)
		parts = append(parts, paint.Panel.Style(drawn.tone).Render(fmt.Sprintf("%d %s", count, drawn.word)))
	}

	return strings.Join(parts, dimStyle.Render(" · "))
}
