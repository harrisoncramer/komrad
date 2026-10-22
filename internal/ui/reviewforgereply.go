package ui

import (
	"context"
	"errors"
	"fmt"

	"komrad/internal/forge"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
)

// reviewForgeCompose is the composer open on a reply that goes to the forge rather than to the
// agent.
type reviewForgeCompose struct {
	title string
}

// failedForgeReply is the reply in a thread the forge refused, which waits on the person to retry
// or discard it.
func failedForgeReply(thread *agentv1.ReviewThread) (*agentv1.ForgeReply, bool) {
	for _, reply := range thread.GetForgeReplies() {
		switch reply.GetState() {
		case agentv1.ForgeReplyState_FORGE_REPLY_STATE_FAILED:
			return reply, true
		case agentv1.ForgeReplyState_FORGE_REPLY_STATE_UNSPECIFIED,
			agentv1.ForgeReplyState_FORGE_REPLY_STATE_APPROVED,
			agentv1.ForgeReplyState_FORGE_REPLY_STATE_SENDING,
			agentv1.ForgeReplyState_FORGE_REPLY_STATE_SENT:
		}
	}

	return nil, false
}

// mirroredCommentIDs is the forge comment ids of the messages in a thread mirrored from the forge.
func mirroredCommentIDs(thread *agentv1.ReviewThread) map[string]bool {
	ids := map[string]bool{}
	for _, message := range thread.GetMessages() {
		if id := message.GetForge().GetCommentId(); id != "" {
			ids[id] = true
		}
	}

	return ids
}

// forgeThreadAtCursor is the thread under the cursor when it mirrors one on the forge.
func (r *reviewScreen) forgeThreadAtCursor() (*agentv1.ReviewThread, bool) {
	thread, ok := r.diff.threadAtCursor()
	if !ok || !thread.GetMirrorsForge() {
		return nil, false
	}

	return thread, true
}

// forgeName is the name of the forge that holds the pull request.
func (r reviewScreen) forgeName(m *model) string {
	pullRequest, ok := r.currentPullRequest(m)
	if !ok {
		return forge.Name("")
	}

	return forge.Name(pullRequest.Forge)
}

// forgeRef names the pull request that receives a reply.
func (r reviewScreen) forgeRef(m *model) string {
	pullRequest, ok := r.currentPullRequest(m)
	if !ok {
		return "the pull request"
	}

	return pullRequest.Ref()
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

	return r.writeMsgCmd(m, "retrying the reply", nil, func(ctx context.Context) (string, error) {
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

	return r.writeCmd(m, "discarding the reply", "reply discarded", func(ctx context.Context) error {
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

	r.threads.editing = thread.GetId()
	r.threads.composing = true
	r.threads.retrying = nil
	r.threads.forge = compose

	return r.openInput(m, newReviewInputParams{
		Title:       compose.title,
		Description: reviewInputHint(fixedKeys().Editor, fixedKeys().Submit, "post"),
		Placeholder: "what should " + r.forgeName(m) + " read?",
	})
}

// saveForgeReply posts what the composer holds to the forge as the person's own reply.
func (r *reviewScreen) saveForgeReply(m *model, compose *reviewForgeCompose, body string) tea.Cmd {
	agents, threadID := m.clients().agents, r.threads.editing
	kept := reviewUnsent{
		editing: threadID,
		body:    body,
		forge:   compose,
	}

	return r.writeMsgCmd(m, "replying on the forge", &kept, func(ctx context.Context) (string, error) {
		res, err := agents.ReplyOnForge(ctx, connect.NewRequest(&agentv1.ReplyOnForgeRequest{
			ThreadId: threadID,
			Body:     body,
		}))
		if err != nil {
			return "", err
		}

		return forgeReplyNotice(res.Msg.GetReply())
	})
}
