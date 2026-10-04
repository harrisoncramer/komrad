package ui_test

import (
	"testing"

	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"
)

// TestReviewForgeReply covers replying on a forge thread, and retrying and discarding a reply the
// forge refused.
func TestReviewForgeReply(t *testing.T) {
	tests := []struct {
		name      string
		replies   []*agentv1.ForgeReply
		keys      []string
		procedure string
		sent      []proto.Message
	}{
		{
			name:      "the reply key posts what was typed",
			keys:      []string{"C", "o", "k", "enter"},
			procedure: agentv1connect.AgentServiceReplyOnForgeProcedure,
			sent:      []proto.Message{&agentv1.ReplyOnForgeRequest{ThreadId: "t1", Body: "ok"}},
		},
		{
			name:      "the reply key starts empty while the agent has a draft",
			replies:   []*agentv1.ForgeReply{newAgentDraft()},
			keys:      []string{"C", "o", "k", "enter"},
			procedure: agentv1connect.AgentServiceReplyOnForgeProcedure,
			sent:      []proto.Message{&agentv1.ReplyOnForgeRequest{ThreadId: "t1", Body: "ok"}},
		},
		{
			name:      "the post key retries a failed reply",
			replies:   []*agentv1.ForgeReply{newFailedReply()},
			keys:      []string{"p"},
			procedure: agentv1connect.AgentServiceRetryForgeReplyProcedure,
			sent:      []proto.Message{&agentv1.RetryForgeReplyRequest{Id: "r1"}},
		},
		{
			name:      "the post key does nothing with no failed reply",
			replies:   []*agentv1.ForgeReply{newAgentDraft()},
			keys:      []string{"p"},
			procedure: agentv1connect.AgentServiceRetryForgeReplyProcedure,
		},
		{
			name:      "the discard key throws a failed reply away",
			replies:   []*agentv1.ForgeReply{newFailedReply()},
			keys:      []string{"x"},
			procedure: agentv1connect.AgentServiceDiscardForgeReplyProcedure,
			sent:      []proto.Message{&agentv1.DiscardForgeReplyRequest{Id: "r1"}},
		},
		{
			name:      "the discard key leaves an agent draft alone",
			replies:   []*agentv1.ForgeReply{newAgentDraft()},
			keys:      []string{"x"},
			procedure: agentv1connect.AgentServiceDiscardForgeReplyProcedure,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newReviewServer(t)
			server.threads = []*agentv1.ReviewThread{newForgeThread(tt.replies...)}
			harness := openReview(t, server)

			harness.press(tt.keys...)

			harness.assertSentTo(tt.procedure, tt.sent...)
		})
	}
}

// TestReviewForgeReplyRefused covers the forge refusing a reply, which the panel reports as an
// error and leaves on the thread to retry rather than opening the composer on it again.
func TestReviewForgeReplyRefused(t *testing.T) {
	tests := []struct {
		name      string
		replies   []*agentv1.ForgeReply
		keys      []string
		procedure string
		answer    connect.AnyResponse
	}{
		{
			name:      "a refused reply is reported",
			keys:      []string{"C", "o", "k", "enter"},
			procedure: agentv1connect.AgentServiceReplyOnForgeProcedure,
			answer:    connect.NewResponse(&agentv1.ReplyOnForgeResponse{Reply: newFailedReply()}),
		},
		{
			name:      "a refused retry is reported",
			replies:   []*agentv1.ForgeReply{newFailedReply()},
			keys:      []string{"p"},
			procedure: agentv1connect.AgentServiceRetryForgeReplyProcedure,
			answer:    connect.NewResponse(&agentv1.RetryForgeReplyResponse{Reply: newFailedReply()}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newReviewServer(t)
			server.threads = []*agentv1.ReviewThread{newForgeThread(tt.replies...)}
			harness := openReview(t, server)
			harness.answer(tt.procedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
				return tt.answer, nil
			})

			harness.press(tt.keys...)

			assert.NotContains(t, readPopupProse(harness.readFrame()), "reply on the forge")
			messages := readOpenMessages(harness)
			assert.Contains(t, messages, "rate limited")
			assert.NotContains(t, messages, "reply posting")
		})
	}
}

// TestReviewForgeReplyView covers how a forge thread and the replies on it are drawn.
func TestReviewForgeReplyView(t *testing.T) {
	tests := []struct {
		name     string
		replies  []*agentv1.ForgeReply
		mirrored []*agentv1.ReviewMessage
		keys     []string
		shows    []string
		hides    []string
	}{
		{
			name:  "each forge comment carries the forge mark",
			shows: []string{"◆ @dana"},
		},
		{
			name:    "an agent draft is not drawn",
			replies: []*agentv1.ForgeReply{newAgentDraft()},
			hides:   []string{"a map it is"},
		},
		{
			name:    "a failed reply says what the forge said",
			replies: []*agentv1.ForgeReply{newFailedReply()},
			shows:   []string{"not posted", "the forge said: rate limited"},
		},
		{
			name:    "an approved reply is posting",
			replies: []*agentv1.ForgeReply{newSentReply(agentv1.ForgeReplyState_FORGE_REPLY_STATE_APPROVED)},
			shows:   []string{"→ posting to the forge"},
		},
		{
			name:    "a sent reply reads as a forge comment",
			replies: []*agentv1.ForgeReply{newSentReply(agentv1.ForgeReplyState_FORGE_REPLY_STATE_SENT)},
			shows:   []string{"◆ you", "a map it is"},
			hides:   []string{"posting"},
		},
		{
			name:     "a sent reply mirrored back from the forge is drawn once",
			replies:  []*agentv1.ForgeReply{newMirroredReply()},
			mirrored: []*agentv1.ReviewMessage{newMirroredComment()},
			shows:    []string{"◆ @harrison", "a map it is"},
			hides:    []string{"◆ you"},
		},
		{
			name:  "the composer quotes the forge comment it answers",
			keys:  []string{"C"},
			shows: []string{"reply on the forge", "↳ @dana: use a map"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newReviewServer(t)
			thread := newForgeThread(tt.replies...)
			thread.Messages = append(thread.Messages, tt.mirrored...)
			server.threads = []*agentv1.ReviewThread{thread}
			harness := openReview(t, server)

			harness.press(tt.keys...)

			prose := readPopupProse(harness.readFrame())
			for _, want := range tt.shows {
				assert.Contains(t, prose, want)
			}
			for _, unwanted := range tt.hides {
				assert.NotContains(t, prose, unwanted)
			}
		})
	}
}

// TestReviewForgeReplyLocal covers a thread kept in komrad offering none of the forge keys.
func TestReviewForgeReplyLocal(t *testing.T) {
	server := newReviewServer(t)
	server.threads = []*agentv1.ReviewThread{newThreadOnFirstLine(newAgentMessage("m1", "renamed it"))}
	harness := openReview(t, server)

	harness.press("p", "x")

	assert.NotContains(t, readPopupProse(harness.readFrame()), "public")
	for _, procedure := range []string{
		agentv1connect.AgentServiceRetryForgeReplyProcedure,
		agentv1connect.AgentServiceDiscardForgeReplyProcedure,
	} {
		assert.Empty(t, harness.readSent(procedure))
	}
}

func newForgeThread(replies ...*agentv1.ForgeReply) *agentv1.ReviewThread {
	said := newHeldForgeComment("f1", "use a map")
	said.Forge = &agentv1.ForgeOrigin{Login: "dana", CommentId: "c1"}
	thread := newThreadOnFirstLine(said)
	thread.MirrorsForge = true
	thread.ForgeReplies = replies

	return thread
}

func newMirroredReply() *agentv1.ForgeReply {
	reply := newSentReply(agentv1.ForgeReplyState_FORGE_REPLY_STATE_SENT)
	reply.ForgeCommentId = xptr.Ptr("c2")

	return reply
}

func newMirroredComment() *agentv1.ReviewMessage {
	said := newHeldForgeComment("f2", "a map it is")
	said.Forge = &agentv1.ForgeOrigin{Login: "harrison", CommentId: "c2"}

	return said
}

func newAgentDraft() *agentv1.ForgeReply {
	return &agentv1.ForgeReply{
		Id:            "r1",
		ThreadId:      "t1",
		State:         agentv1.ForgeReplyState_FORGE_REPLY_STATE_DRAFT,
		AuthorKind:    agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT,
		AuthorAgentId: xptr.Ptr("a1"),
		ProposedBody:  xptr.Ptr("a map it is"),
		Body:          "a map it is",
	}
}

func newFailedReply() *agentv1.ForgeReply {
	return &agentv1.ForgeReply{
		Id:         "r1",
		ThreadId:   "t1",
		State:      agentv1.ForgeReplyState_FORGE_REPLY_STATE_FAILED,
		AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		Body:       "a map it is",
		Error:      xptr.Ptr("rate limited"),
	}
}

func newSentReply(state agentv1.ForgeReplyState) *agentv1.ForgeReply {
	return &agentv1.ForgeReply{
		Id:         "r1",
		ThreadId:   "t1",
		State:      state,
		AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		Body:       "a map it is",
		CreatedAt:  "2026-09-02T10:00:00Z",
	}
}
