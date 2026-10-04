package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"komrad/internal/forge"
	"komrad/internal/glyph"
	"komrad/internal/ui/diffview"
	"komrad/internal/ui/paint"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"connectrpc.com/connect"
	"github.com/charmbracelet/x/ansi"
)

// reviewForgeCompose is the composer open on a reply that goes to the forge rather than to the
// agent.
type reviewForgeCompose struct {
	title string
	// quote is the forge comment the reply answers, drawn over the field.
	quote string
}

// failedForgeReply is the reply in a thread the forge refused, which waits on the person to retry
// or discard it.
func failedForgeReply(thread *agentv1.ReviewThread) (*agentv1.ForgeReply, bool) {
	for _, reply := range thread.GetForgeReplies() {
		switch reply.GetState() {
		case agentv1.ForgeReplyState_FORGE_REPLY_STATE_FAILED:
			return reply, true
		case agentv1.ForgeReplyState_FORGE_REPLY_STATE_UNSPECIFIED,
			agentv1.ForgeReplyState_FORGE_REPLY_STATE_DRAFT,
			agentv1.ForgeReplyState_FORGE_REPLY_STATE_APPROVED,
			agentv1.ForgeReplyState_FORGE_REPLY_STATE_SENDING,
			agentv1.ForgeReplyState_FORGE_REPLY_STATE_SENT:
		}
	}

	return nil, false
}

// lastForgeMessage is the latest comment in a thread written on the forge.
func lastForgeMessage(thread *agentv1.ReviewThread) (*agentv1.ReviewMessage, bool) {
	messages := thread.GetMessages()
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].GetForge() != nil {
			return messages[i], true
		}
	}

	return nil, false
}

// forgeThreadAtCursor is the thread under the cursor when it mirrors one on the forge.
func (r *reviewScreen) forgeThreadAtCursor() (*agentv1.ReviewThread, bool) {
	thread, ok := r.diff.threadAtCursor()
	if !ok || !thread.GetMirrorsForge() {
		return nil, false
	}

	return thread, true
}

// forgeName is what the forge the pull request is open on is called.
func (r reviewScreen) forgeName(m *model) string {
	pullRequest, ok := r.currentPullRequest(m)
	if !ok {
		return forge.Name("")
	}

	return forge.Name(pullRequest.Forge)
}

// forgeRef names the pull request a reply is posted on.
func (r reviewScreen) forgeRef(m *model) string {
	pullRequest, ok := r.currentPullRequest(m)
	if !ok {
		return "the pull request"
	}

	return pullRequest.Ref()
}

// popupTone is the colour of the popup's border, the forge's own while it holds a thread mirrored
// from the forge or a reply on its way there.
func (r reviewScreen) popupTone() paint.Tone {
	switch {
	case r.threads.composing:
		if r.threads.forge != nil {
			return diffview.ForgeTone
		}

		return paint.ToneMuted
	case r.threads.releasing:
		return paint.ToneMuted
	}

	if thread, ok := r.diff.threadAtCursor(); ok && thread.GetMirrorsForge() {
		return diffview.ForgeTone
	}

	return paint.ToneMuted
}

func (r *reviewScreen) doesOfferForgeReply() bool {
	_, ok := r.forgeThreadAtCursor()

	return ok
}

// doesOfferForgeRetry says whether the thread under the cursor holds a reply the forge refused.
func (r *reviewScreen) doesOfferForgeRetry() bool {
	thread, ok := r.forgeThreadAtCursor()
	if !ok {
		return false
	}

	_, ok = failedForgeReply(thread)

	return ok
}

// retryForgeReply sends again the reply the forge refused in the thread under the cursor.
func (r *reviewScreen) retryForgeReply(m *model) tea.Cmd {
	thread, ok := r.forgeThreadAtCursor()
	if !ok {
		return nil
	}

	reply, ok := failedForgeReply(thread)
	if !ok {
		return nil
	}

	agents, id := m.clients().agents, reply.GetId()

	return r.writeMsgCmd(m, m.beginWrite("retrying the reply"), func(ctx context.Context) (string, error) {
		res, err := agents.RetryForgeReply(ctx, connect.NewRequest(&agentv1.RetryForgeReplyRequest{
			Id: id,
		}))
		if err != nil {
			return "", err
		}

		return forgeReplyNotice(res.Msg.GetReply())
	})
}

// errForgeRefused is a reply the forge turned down, which stays on its thread to retry, so the
// composer is not opened on it again.
var errForgeRefused = errors.New("the forge refused the reply")

// forgeReplyNotice says how the forge took a reply, reporting a refusal as an error.
func forgeReplyNotice(reply *agentv1.ForgeReply) (string, error) {
	switch reply.GetState() {
	case agentv1.ForgeReplyState_FORGE_REPLY_STATE_FAILED:
		return "", fmt.Errorf("%w: %s", errForgeRefused, reply.GetError())
	case agentv1.ForgeReplyState_FORGE_REPLY_STATE_SENT:
		return "reply posted", nil
	case agentv1.ForgeReplyState_FORGE_REPLY_STATE_UNSPECIFIED,
		agentv1.ForgeReplyState_FORGE_REPLY_STATE_DRAFT,
		agentv1.ForgeReplyState_FORGE_REPLY_STATE_APPROVED,
		agentv1.ForgeReplyState_FORGE_REPLY_STATE_SENDING:
		return "reply posting", nil
	}

	return "reply posting", nil
}

// discardForgeReply throws away the reply the forge refused in the thread under the cursor.
func (r *reviewScreen) discardForgeReply(m *model) tea.Cmd {
	thread, ok := r.forgeThreadAtCursor()
	if !ok {
		return nil
	}

	reply, ok := failedForgeReply(thread)
	if !ok {
		return nil
	}

	agents, id := m.clients().agents, reply.GetId()

	return r.writeCmd(m, m.beginWrite("discarding the reply"), "reply discarded", func(ctx context.Context) error {
		_, err := agents.DiscardForgeReply(ctx, connect.NewRequest(&agentv1.DiscardForgeReplyRequest{
			Id: id,
		}))

		return err
	})
}

// composeForge opens the composer on a reply to the forge.
func (r *reviewScreen) composeForge(m *model) tea.Cmd {
	thread, ok := r.forgeThreadAtCursor()
	if !ok {
		return nil
	}

	compose := &reviewForgeCompose{
		title: "reply on " + r.forgeName(m) + separated + "public on " + r.forgeRef(m),
	}
	if said, ok := lastForgeMessage(thread); ok {
		compose.quote = describeForgeLogin(said.GetForge().GetLogin()) + ": " + firstLine(plainForgeText(said.GetBody()))
	}

	r.threads.editing = thread.GetId()
	r.threads.composing = true
	r.threads.retrying = nil
	r.threads.forge = compose

	return r.openInput(m, newReviewInputParams{
		Title:       compose.title,
		Description: reviewInputHint(formKeys().Editor, formKeys().Submit, "post"),
		Placeholder: "what should " + r.forgeName(m) + " read?",
	})
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")

	return line
}

// saveForgeReply posts what the composer holds to the forge as the person's own reply.
func (r *reviewScreen) saveForgeReply(m *model, compose *reviewForgeCompose, body string) tea.Cmd {
	agents, threadID := m.clients().agents, r.threads.editing
	kept := reviewUnsent{editing: threadID, body: body, forge: compose}

	return keepUnsent(kept, r.writeMsgCmd(m, m.beginWrite("replying on the forge"), func(ctx context.Context) (string, error) {
		res, err := agents.ReplyOnForge(ctx, connect.NewRequest(&agentv1.ReplyOnForgeRequest{
			ThreadId: threadID,
			Body:     body,
		}))
		if err != nil {
			return "", err
		}

		return forgeReplyNotice(res.Msg.GetReply())
	}))
}

// forgeQuoteView is the forge comment drawn over the composer it is being answered in.
func (t reviewThreads) forgeQuoteView(width int) string {
	if t.forge == nil || t.forge.quote == "" {
		return ""
	}

	quote := ansi.Truncate("↳ "+t.forge.quote, max(width, 1), "…")

	return paint.Panel.Style(diffview.ForgeTone).Italic(true).Render(quote) + "\n"
}

// forgeTitleView is the row over a thread mirrored from the forge, saying where it is and that
// what is written in it is public.
func (r reviewScreen) forgeTitleView(m *model, thread *agentv1.ReviewThread) string {
	if !thread.GetMirrorsForge() {
		return ""
	}

	title := m.glyphs.Spell(glyph.ThreadForge) + " " + r.forgeName(m) + " thread" + separated +
		diffview.FormatThreadLocation(thread) + separated + "public"

	return paint.Panel.Style(diffview.ForgeTone).Bold(true).Render(title) + "\n\n"
}

// forgeRepliesView is what the thread has said back to the forge.
func (r reviewScreen) forgeRepliesView(m *model, thread *agentv1.ReviewThread, width int) string {
	out := ""
	for _, reply := range thread.GetForgeReplies() {
		out += r.forgeReplyView(m, reply, width)
	}

	return out
}

func (r reviewScreen) forgeReplyView(m *model, reply *agentv1.ForgeReply, width int) string {
	name := r.forgeName(m)

	said := "posting to " + name
	switch reply.GetState() {
	case agentv1.ForgeReplyState_FORGE_REPLY_STATE_DRAFT:
		return ""
	case agentv1.ForgeReplyState_FORGE_REPLY_STATE_FAILED:
		head := paint.Panel.Style(paint.ToneDanger).Bold(true).Render("your reply" + separated + "not posted")
		refused := paint.Panel.Style(paint.ToneDanger).Render(m.glyphs.Spell(glyph.MessageError) + " " + name + " said: " + reply.GetError())

		return drawForgeBox(width, head, reply.GetBody()+"\n"+refused)
	case agentv1.ForgeReplyState_FORGE_REPLY_STATE_SENT:
		said = "posted"
	case agentv1.ForgeReplyState_FORGE_REPLY_STATE_UNSPECIFIED,
		agentv1.ForgeReplyState_FORGE_REPLY_STATE_APPROVED,
		agentv1.ForgeReplyState_FORGE_REPLY_STATE_SENDING:
	}

	out := drawCommentHead(reviewHumanTone, "you", reply.GetCreatedAt()) + "\n" + reviewWrap(width, reply.GetBody()) + "\n"

	return out + dimStyle().Render(glyph.Right+" "+said) + "\n"
}

// drawForgeBox is a refused reply set apart in a red frame of its own, its head over its body.
func drawForgeBox(width int, head, body string) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(paint.Panel.Color(paint.ToneDanger)).
		Padding(0, 1).
		Width(max(width, 6)).
		Render(head+"\n"+body) + "\n"
}
