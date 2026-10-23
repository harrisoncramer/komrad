package review

import (
	"time"

	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/lib/dates"
	agentv1 "komrad/proto/gen/agent/v1"
)

// delivery is where one of the person's comments stands with the writer, as a badge under
// the comment says it: its word, the moment it names, its colour and the glyph it opens with,
// which is an arrow while the comment is on its way.
type delivery struct {
	word string
	at   time.Time
	tone paint.Tone
	mark glyph.Glyph
}

// deliveryParams is a comment, the thread it sits in and the writer whose answer counts.
type deliveryParams struct {
	thread   *agentv1.ReviewThread
	message  *agentv1.ReviewMessage
	writerID string
}

// deliveryOf is where a comment stands: held for a forge comment the person has not
// accepted, then released, read or answered for the person's own, and nothing for an agent's.
func deliveryOf(params deliveryParams) delivery {
	message := params.message
	switch {
	case isHeldForge(message):
		return delivery{
			word: "held",
			tone: forgeHeldTone,
			mark: glyph.MessageWarning,
		}
	case message.GetAuthorKind() != agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN:
		return delivery{}
	}

	if answer := findReplyTo(params); answer != nil {
		return delivery{
			word: "answered",
			at:   dates.Read(answer.GetCreatedAt()),
			tone: readTone,
			mark: glyph.Delivered,
		}
	}
	if read := message.GetReadAt(); read != nil {
		return delivery{
			word: "read",
			at:   dates.Read(read),
			tone: readTone,
			mark: glyph.Delivered,
		}
	}

	return delivery{
		word: "released",
		at:   dates.Read(message.GetCreatedAt()),
		tone: releasedTone,
	}
}

// findReplyTo is the first thing the writer said in the thread after the person's comment.
func findReplyTo(params deliveryParams) *agentv1.ReviewMessage {
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
