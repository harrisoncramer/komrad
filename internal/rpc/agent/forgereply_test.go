package agent

import (
	"context"
	"testing"

	"komrad/internal/caller"
	"komrad/internal/reviews"
	reviewstore "komrad/internal/store/review"
	"komrad/lib/dates"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1komrad"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
)

// fakeForgeReplier records what each forge reply call asked and answers with err or the reply it holds.
type fakeForgeReplier struct {
	err     error
	reply   reviewstore.ForgeReply
	authors []reviews.ReviewAuthor
}

func newForgeReplyHandler(replier *fakeForgeReplier) agentv1komrad.AgentServiceAdapter {
	return agentv1komrad.AgentServiceAdapter{Impl: NewHandler(NewHandlerParams{
		ForgeReplies: replier,
	})}
}

// TestForgeReplyRefusalsMapToCodes pins the code each refusal of the forge reply service reaches the caller as.
func TestForgeReplyRefusalsMapToCodes(t *testing.T) {
	tests := []struct {
		name     string
		call     func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error
		err      error
		wantCode connect.Code
	}{
		{
			name: "an agent replying on the forge",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.ReplyOnForge(ctx, connect.NewRequest(&agentv1.ReplyOnForgeRequest{ThreadId: betaID.String(), Body: "done"}))

				return err
			},
			err:      reviews.ErrReviewHumanOnly,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name: "an agent retrying a reply",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.RetryForgeReply(ctx, connect.NewRequest(&agentv1.RetryForgeReplyRequest{Id: betaID.String()}))

				return err
			},
			err:      reviews.ErrReviewHumanOnly,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name: "an agent discarding a failed reply",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.DiscardForgeReply(ctx, connect.NewRequest(&agentv1.DiscardForgeReplyRequest{Id: betaID.String()}))

				return err
			},
			err:      reviews.ErrReviewHumanOnly,
			wantCode: connect.CodePermissionDenied,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newForgeReplyHandler(&fakeForgeReplier{err: tt.err})

			err := tt.call(caller.With(t.Context(), caller.Caller{AgentID: alphaID}), svc)
			assert.Equal(t, tt.wantCode, connect.CodeOf(err))
			require.ErrorIs(t, err, tt.err)
		})
	}
}

// TestForgeReplyCallsNameTheCaller pins who each forge reply call acts as.
func TestForgeReplyCallsNameTheCaller(t *testing.T) {
	person := reviews.ReviewAuthor{Kind: reviewstore.AuthorHuman}
	agent := reviews.ReviewAuthor{Kind: reviewstore.AuthorAgent, AgentID: xptr.Ptr(alphaID)}

	calls := map[string]func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error{
		"replying": func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
			_, err := svc.ReplyOnForge(ctx, connect.NewRequest(&agentv1.ReplyOnForgeRequest{ThreadId: betaID.String(), Body: "done"}))

			return err
		},
		"retrying": func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
			_, err := svc.RetryForgeReply(ctx, connect.NewRequest(&agentv1.RetryForgeReplyRequest{Id: betaID.String()}))

			return err
		},
		"discarding": func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
			_, err := svc.DiscardForgeReply(ctx, connect.NewRequest(&agentv1.DiscardForgeReplyRequest{Id: betaID.String()}))

			return err
		},
	}

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			replier := &fakeForgeReplier{}
			svc := newForgeReplyHandler(replier)

			require.NoError(t, call(t.Context(), svc))
			require.NoError(t, call(caller.With(t.Context(), caller.Caller{AgentID: alphaID}), svc))
			assert.Equal(t, []reviews.ReviewAuthor{person, agent}, replier.authors)
		})
	}
}

// TestForgeReplyDrawsWhatTheServiceHeld pins the reply a call answers with.
func TestForgeReplyDrawsWhatTheServiceHeld(t *testing.T) {
	svc := newForgeReplyHandler(&fakeForgeReplier{reply: reviewstore.ForgeReply{
		ID:                     replyID,
		ThreadID:               threadID,
		State:                  reviewstore.ForgeReplySent,
		Body:                   "added the guard, thanks",
		ApprovedAt:             &reviewedAt,
		SentAt:                 &reviewedAt,
		ForgeCommentProviderID: xptr.Ptr("PRRC_posted"),
		CreatedAt:              reviewedAt,
	}})

	got, err := svc.RetryForgeReply(t.Context(), connect.NewRequest(&agentv1.RetryForgeReplyRequest{Id: betaID.String()}))
	require.NoError(t, err)

	want := &agentv1.ForgeReply{
		Id:         "reply-1",
		ThreadId:   "thread-1",
		State:      agentv1.ForgeReplyState_FORGE_REPLY_STATE_SENT,
		Body:       "added the guard, thanks",
		ApprovedAt: dates.Stamp(reviewedAt),
		SentAt:     dates.Stamp(reviewedAt),
		CreatedAt:  dates.Stamp(reviewedAt),
	}
	assert.Empty(t, cmp.Diff(want, got.Msg.GetReply(), protocmp.Transform()))
}

func (f *fakeForgeReplier) answer(author reviews.ReviewAuthor) (*reviewstore.ForgeReply, error) {
	f.authors = append(f.authors, author)
	if f.err != nil {
		return nil, f.err
	}

	return &f.reply, nil
}

// ReplyOnForge records the author and answers the reply it holds.
func (f *fakeForgeReplier) ReplyOnForge(_ context.Context, params reviews.ReplyOnForgeParams) (*reviewstore.ForgeReply, error) {
	return f.answer(params.Author)
}

// RetryForgeReply records the author and answers the reply it holds.
func (f *fakeForgeReplier) RetryForgeReply(_ context.Context, _ uuid.UUID, author reviews.ReviewAuthor) (*reviewstore.ForgeReply, error) {
	return f.answer(author)
}

// DiscardForgeReply records the author and answers err.
func (f *fakeForgeReplier) DiscardForgeReply(_ context.Context, _ uuid.UUID, author reviews.ReviewAuthor) error {
	_, err := f.answer(author)

	return err
}

var (
	replyID  = uuid.New()
	threadID = uuid.New()
)
