package ui

import (
	"komrad/internal/ui/paint"
	agentv1 "komrad/proto/gen/agent/v1"
)

// reviewDelivery is where one of the person's comments stands with the writer, as a badge under
// the comment says it: its word, the moment it names and its colour.
type reviewDelivery struct {
	word  string
	stamp string
	tone  paint.Tone
}

// reviewDeliveryOf is where a comment stands: held for a forge comment the person has not
// accepted, then released, read or answered for the person's own, and nothing for an agent's.
func reviewDeliveryOf(thread *agentv1.ReviewThread, message *agentv1.ReviewMessage) reviewDelivery {
	switch {
	case isHeldForge(message):
		return reviewDelivery{word: "held", tone: reviewForgeHeldTone}
	case message.GetAuthorKind() != agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN:
		return reviewDelivery{}
	}

	if answer, ok := findReplyTo(thread, message); ok {
		return reviewDelivery{word: "answered", stamp: answer.GetCreatedAt(), tone: reviewReadTone}
	}
	if read := message.GetReadAt(); read != "" {
		return reviewDelivery{word: "read", stamp: read, tone: reviewReadTone}
	}

	return reviewDelivery{word: "released", stamp: message.GetCreatedAt(), tone: reviewReleasedTone}
}

// findReplyTo is the first thing an agent said in the thread after the person's comment.
func findReplyTo(thread *agentv1.ReviewThread, message *agentv1.ReviewMessage) (*agentv1.ReviewMessage, bool) {
	seen := false
	for _, said := range thread.GetMessages() {
		if said.GetId() == message.GetId() {
			seen = true

			continue
		}
		if seen && said.GetAuthorKind() == agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT {
			return said, true
		}
	}

	return nil, false
}
