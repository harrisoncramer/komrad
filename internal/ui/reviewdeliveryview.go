package ui

import (
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	agentv1 "komrad/proto/gen/agent/v1"
)

// reviewDeliveryBadge is where one comment stands with the writer, rendered under the comment
// itself and not at all for what an agent wrote.
func (r reviewScreen) reviewDeliveryBadge(m *model, thread *agentv1.ReviewThread, message *agentv1.ReviewMessage) string {
	rendered := reviewDeliveryOf(thread, message)
	if rendered.word == "" {
		return ""
	}

	badge := r.reviewDeliveryMark(m, rendered.word) + " " + rendered.word
	if when := reviewTimeOf(rendered.stamp); when != "" {
		badge += " " + when
	}

	return paint.Panel.Style(rendered.tone).Render("  "+badge) + "\n"
}

// reviewDeliveryMark is the glyph a badge opens with: a warning while a forge comment is held, a
// tick once the writer has the comment, and an arrow while it is on its way.
func (r reviewScreen) reviewDeliveryMark(m *model, word string) string {
	switch word {
	case "held":
		return m.glyphs.Spell(glyph.MessageWarning)
	case "read", "answered":
		return m.glyphs.Spell(glyph.Delivered)
	default:
		return glyph.Right
	}
}
