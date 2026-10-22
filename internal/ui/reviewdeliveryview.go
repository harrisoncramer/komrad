package ui

import (
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	agentv1 "komrad/proto/gen/agent/v1"
)

// reviewDeliveryBadge is where one comment stands with the writer, rendered under the comment
// itself and not at all for what an agent wrote.
func (r reviewScreen) reviewDeliveryBadge(thread *agentv1.ReviewThread, message *agentv1.ReviewMessage) string {
	rendered := reviewDeliveryOf(reviewDeliveryParams{thread: thread, message: message, writerID: r.agentID})
	if rendered.word == "" {
		return ""
	}

	mark := glyph.Right
	if rendered.mark != glyph.None {
		mark = r.deps.glyphs.Spell(rendered.mark)
	}

	badge := mark + " " + rendered.word
	if when := reviewTimeOf(rendered.stamp); when != "" {
		badge += " " + when
	}

	return paint.Panel.Style(rendered.tone).Render("  "+badge) + "\n"
}
