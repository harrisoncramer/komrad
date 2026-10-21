package ui

import (
	"testing"

	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
)

// TestReviewDeliveryOf covers the word under a comment and the moment it names: the last thing
// that happened to the comment, and nothing under what an agent wrote.
func TestReviewDeliveryOf(t *testing.T) {
	human := agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN
	agent := agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT
	const writer, reviewer = "writer", "reviewer"

	tests := []struct {
		name      string
		thread    *agentv1.ReviewThread
		message   *agentv1.ReviewMessage
		wantWord  string
		wantStamp string
	}{
		{
			name:     "an agent's reply carries nothing",
			message:  &agentv1.ReviewMessage{Id: "m1", AuthorKind: agent, AuthorAgentId: xptr.Ptr(writer)},
			wantWord: "",
		},
		{
			name:      "a comment just written is released the moment it was written",
			message:   &agentv1.ReviewMessage{Id: "m1", AuthorKind: human, CreatedAt: "2026-09-02T10:00:00Z"},
			wantWord:  "released",
			wantStamp: "2026-09-02T10:00:00Z",
		},
		{
			name: "a comment the writer read names when it was read",
			message: &agentv1.ReviewMessage{
				Id:         "m1",
				AuthorKind: human,
				CreatedAt:  "2026-09-02T10:00:00Z",
				ReadAt:     "2026-09-02T10:01:00Z",
			},
			wantWord:  "read",
			wantStamp: "2026-09-02T10:01:00Z",
		},
		{
			name: "a comment the writer answered names the answer",
			thread: &agentv1.ReviewThread{Messages: []*agentv1.ReviewMessage{
				{Id: "m0", AuthorKind: agent, AuthorAgentId: xptr.Ptr(writer), CreatedAt: "2026-09-02T09:00:00Z"},
				{Id: "m1", AuthorKind: human, CreatedAt: "2026-09-02T10:00:00Z", ReadAt: "2026-09-02T10:01:00Z"},
				{Id: "m2", AuthorKind: agent, AuthorAgentId: xptr.Ptr(writer), CreatedAt: "2026-09-02T10:02:00Z"},
			}},
			message:   &agentv1.ReviewMessage{Id: "m1", AuthorKind: human, ReadAt: "2026-09-02T10:01:00Z"},
			wantWord:  "answered",
			wantStamp: "2026-09-02T10:02:00Z",
		},
		{
			name: "a reviewer's words after the comment do not answer it",
			thread: &agentv1.ReviewThread{Messages: []*agentv1.ReviewMessage{
				{Id: "m0", AuthorKind: agent, AuthorAgentId: xptr.Ptr(reviewer), CreatedAt: "2026-09-02T09:00:00Z"},
				{Id: "m1", AuthorKind: human, CreatedAt: "2026-09-02T10:00:00Z", ReadAt: "2026-09-02T10:01:00Z"},
				{Id: "m2", AuthorKind: agent, AuthorAgentId: xptr.Ptr(reviewer), CreatedAt: "2026-09-02T10:02:00Z"},
			}},
			message:   &agentv1.ReviewMessage{Id: "m1", AuthorKind: human, ReadAt: "2026-09-02T10:01:00Z"},
			wantWord:  "read",
			wantStamp: "2026-09-02T10:01:00Z",
		},
		{
			name:     "a forge comment the person has not accepted is held",
			message:  &agentv1.ReviewMessage{Id: "m1", AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_FORGE, Held: true},
			wantWord: "held",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reviewDeliveryOf(reviewDeliveryParams{thread: tt.thread, message: tt.message, writerID: writer})
			assert.Equal(t, tt.wantWord, got.word)
			assert.Equal(t, tt.wantStamp, got.stamp)
		})
	}
}
