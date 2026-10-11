package ui

import (
	"fmt"
	"strings"

	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	agentv1 "komrad/proto/gen/agent/v1"
)

// reviewDeliveryBadge is how far one comment has got towards being read, rendered under the comment
// itself and not at all for what the agent wrote.
func reviewDeliveryBadge(palette paint.Palette, glyphs glyph.Set, message *agentv1.ReviewMessage) string {
	rendered := reviewDeliveryOf(message.GetDelivery())
	if rendered.word == "" {
		return ""
	}

	badge := reviewDeliveryMark(glyphs, message.GetDelivery()) + " " + rendered.word
	if when := reviewTimeOf(reviewDeliveryStamp(message)); when != "" {
		badge += " " + when
	}

	return palette.Style(rendered.tone).Render("  "+badge) + "\n"
}

// reviewDeliveryMark is the mark a badge opens with.
func reviewDeliveryMark(glyphs glyph.Set, state agentv1.ReviewDeliveryState) string {
	switch state {
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_HELD:
		return glyphs.Spell(glyph.MessageWarning)
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT:
		return glyphs.Spell(glyph.Held)
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_SENT:
		return glyph.Right
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_DELIVERED:
		return glyphs.Spell(glyph.Delivered)
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSPECIFIED:
		return ""
	}

	return ""
}

// deliveryCountView is how the person's own comments stand, counted rather than summarised.
func (t reviewThreads) deliveryCountView() string {
	parts := make([]string, 0, len(reviewDeliveryOrder))
	for _, state := range reviewDeliveryOrder {
		count := t.delivery[deliverySlot(state)]
		if count == 0 {
			continue
		}

		rendered := reviewDeliveryOf(state)
		parts = append(parts, paint.Panel.Style(rendered.tone).Render(fmt.Sprintf("%d %s", count, rendered.word)))
	}

	return strings.Join(parts, dimStyle().Render(separated))
}
