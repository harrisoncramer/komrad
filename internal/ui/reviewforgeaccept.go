package ui

import (
	"context"

	"komrad/internal/ui/screen"
	agentv1 "komrad/proto/gen/agent/v1"

	"connectrpc.com/connect"
)

// heldForgeTarget is the held forge comment the accept key would pass on in a thread, the earliest
// one with no copy, so a thread is always accepted from the comment that opens it.
func heldForgeTarget(thread *agentv1.ReviewThread) (*agentv1.ReviewMessage, bool) {
	for _, message := range thread.GetMessages() {
		if isHeldForge(message) && !isAcceptedForge(thread, message) {
			return message, true
		}
	}

	return nil, false
}

func isHeldForge(message *agentv1.ReviewMessage) bool {
	return message.GetAuthorKind() == agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_FORGE && message.GetHeld()
}

// isAcceptedForge says whether the person has already accepted a held forge comment.
func isAcceptedForge(thread *agentv1.ReviewThread, message *agentv1.ReviewMessage) bool {
	for _, other := range thread.GetMessages() {
		if other.GetAcceptedFrom() == message.GetId() {
			return true
		}
	}

	return false
}

// heldForgeSaid is what is said beside a held forge comment, whether it still waits on the person.
func heldForgeSaid(thread *agentv1.ReviewThread, message *agentv1.ReviewMessage) string {
	if !isHeldForge(message) {
		return ""
	}

	if isAcceptedForge(thread, message) {
		return "accepted, released to the writer"
	}

	return "held, its author cannot push"
}

// doesOfferAccept says whether the thread under the cursor holds a forge comment to accept.
func (r *reviewScreen) doesOfferAccept() bool {
	thread, ok := r.diff.threadAtCursor()
	if !ok {
		return false
	}

	_, ok = heldForgeTarget(thread)

	return ok
}

// acceptForge accepts the held forge comment under the cursor, which releases it to the writer as
// the person's own comment.
func (r *reviewScreen) acceptForge(m *model) screen.Request {
	thread, ok := r.diff.threadAtCursor()
	if !ok {
		return nil
	}

	target, ok := heldForgeTarget(thread)
	if !ok {
		return nil
	}

	agents, id := m.clients().agents, target.GetId()

	return r.write(m, "accepting the comment", "comment accepted", func(ctx context.Context) error {
		_, err := agents.AcceptForgeMessage(ctx, connect.NewRequest(&agentv1.AcceptForgeMessageRequest{
			MessageId: id,
		}))

		return err
	})
}
