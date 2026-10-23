package review_test

import (
	"testing"

	"komrad/internal/ui/paneltest"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"
)

// TestReviewForgeAccept covers passing a held forge comment on to the agent from the review screen.
func TestReviewForgeAccept(t *testing.T) {
	tests := []struct {
		name     string
		messages []*agentv1.ReviewMessage
		sent     []proto.Message
		said     string
	}{
		{
			name:     "the accept key passes on the held comment opening the thread",
			messages: []*agentv1.ReviewMessage{newHeldForgeComment("f1", "use a map"), newHeldForgeComment("f2", "agreed")},
			sent:     []proto.Message{&agentv1.AcceptForgeMessageRequest{MessageId: "f1"}},
			said:     "held, its author cannot push",
		},
		{
			name: "an accepted comment moves the key on to the next held one",
			messages: []*agentv1.ReviewMessage{
				newHeldForgeComment("f1", "use a map"),
				newAcceptedCopy("m1", "f1"),
				newHeldForgeComment("f2", "agreed"),
			},
			sent: []proto.Message{&agentv1.AcceptForgeMessageRequest{MessageId: "f2"}},
			said: "accepted, released to the writer",
		},
		{
			name:     "a thread with nothing held offers no accept",
			messages: []*agentv1.ReviewMessage{newAgentMessage("m9", "renamed it")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newReviewServer(t)
			server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(tt.messages...)}
			harness := openReview(t, server)

			if tt.said != "" {
				assert.Contains(t, paneltest.PopupProse(harness.Frame()), tt.said)
			}

			harness.Press("space", "A")

			harness.AssertSentTo(agentv1connect.AgentServiceAcceptForgeMessageProcedure, tt.sent...)
		})
	}
}

func newAcceptedCopy(id, from string) *agentv1.ReviewMessage {
	message := newReleasedComment("m1")
	message.Id = id
	message.AcceptedFrom = xptr.Ptr(from)

	return message
}
