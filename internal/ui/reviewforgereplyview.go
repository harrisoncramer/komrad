package ui

import (
	"komrad/internal/glyph"
	"komrad/internal/ui/diffview"
	"komrad/internal/ui/paint"
	agentv1 "komrad/proto/gen/agent/v1"

	"charm.land/lipgloss/v2"
)

// forgeMark is the mark a comment that lives on the forge opens with.
func forgeMark(glyphs glyph.Set) string {
	return paint.Panel.Style(diffview.ForgeTone).Bold(true).Render(glyphs.Spell(glyph.ThreadForge)) + " "
}

// forgeRepliesView is what the thread has said back to the forge.
func (r reviewScreen) forgeRepliesView(m *model, thread *agentv1.ReviewThread, width int) string {
	mirrored := mirroredCommentIDs(thread)

	out := ""
	for _, reply := range thread.GetForgeReplies() {
		if reply.GetState() == agentv1.ForgeReplyState_FORGE_REPLY_STATE_SENT && mirrored[reply.GetForgeCommentId()] {
			continue
		}
		out += r.forgeReplyView(m, reply, width)
	}

	return out
}

func (r reviewScreen) forgeReplyView(m *model, reply *agentv1.ForgeReply, width int) string {
	name := r.forgeName(m)

	posting := ""
	switch reply.GetState() {
	case agentv1.ForgeReplyState_FORGE_REPLY_STATE_FAILED:
		head := paint.Panel.Style(paint.ToneDanger).Bold(true).Render("your reply" + separated + "not posted")
		refused := paint.Panel.Style(paint.ToneDanger).Render(m.glyphs.Spell(glyph.MessageError) + " " + name + " said: " + reply.GetError())

		return renderForgeBox(width, head, reply.GetBody()+"\n"+refused)
	case agentv1.ForgeReplyState_FORGE_REPLY_STATE_SENT:
	case agentv1.ForgeReplyState_FORGE_REPLY_STATE_UNSPECIFIED,
		agentv1.ForgeReplyState_FORGE_REPLY_STATE_APPROVED,
		agentv1.ForgeReplyState_FORGE_REPLY_STATE_SENDING:
		posting = dimStyle().Render("  "+glyph.Right+" posting to "+name) + "\n"
	}

	return forgeMark(m.glyphs) + renderCommentHead(reviewHumanTone, "you", reply.GetCreatedAt()) + "\n" +
		reviewWrap(width, reply.GetBody()) + "\n" + posting + "\n"
}

// renderForgeBox is a refused reply set apart in a red frame of its own, its head over its body.
func renderForgeBox(width int, head, body string) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(paint.Panel.Color(paint.ToneDanger)).
		Padding(0, 1).
		Width(max(width, 6)).
		Render(head+"\n"+body) + "\n"
}
