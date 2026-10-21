package ui

import (
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	agentv1 "komrad/proto/gen/agent/v1"
)

// reviewDelivery is where one of the person's comments stands with the writer, as a badge under
// the comment says it: its word, the moment it names, its colour and the glyph it opens with,
// which is an arrow while the comment is on its way.
type reviewDelivery struct {
	word  string
	stamp string
	tone  paint.Tone
	mark  glyph.Glyph
}

// reviewDeliveryParams is a comment, the thread it sits in and the writer whose answer counts.
type reviewDeliveryParams struct {
	thread   *agentv1.ReviewThread
	message  *agentv1.ReviewMessage
	writerID string
}

// reviewDeliveryOf is where a comment stands: held for a forge comment the person has not
// accepted, then released, read or answered for the person's own, and nothing for an agent's.
func reviewDeliveryOf(params reviewDeliveryParams) reviewDelivery {
	message := params.message
	switch {
	case isHeldForge(message):
		return reviewDelivery{
			word: "held",
			tone: reviewForgeHeldTone,
			mark: glyph.MessageWarning,
		}
	case message.GetAuthorKind() != agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN:
		return reviewDelivery{}
	}

	if answer := findReplyTo(params); answer != nil {
		return reviewDelivery{
			word:  "answered",
			stamp: answer.GetCreatedAt(),
			tone:  reviewReadTone,
			mark:  glyph.Delivered,
		}
	}
	if read := message.GetReadAt(); read != "" {
		return reviewDelivery{
			word:  "read",
			stamp: read,
			tone:  reviewReadTone,
			mark:  glyph.Delivered,
		}
	}

	return reviewDelivery{
		word:  "released",
		stamp: message.GetCreatedAt(),
		tone:  reviewReleasedTone,
	}
}

// findReplyTo is the first thing the writer said in the thread after the person's comment.
func findReplyTo(params reviewDeliveryParams) *agentv1.ReviewMessage {
	seen := false
	for _, said := range params.thread.GetMessages() {
		if said.GetId() == params.message.GetId() {
			seen = true

			continue
		}
		if seen && said.GetAuthorAgentId() == params.writerID {
			return said
		}
	}

	return nil
}
