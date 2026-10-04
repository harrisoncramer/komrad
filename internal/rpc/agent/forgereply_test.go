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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
)

// fakeForgeReplier records what each forge reply call asked and answers with err or the reply it holds.
type fakeForgeReplier struct {
	err      error
	reply    reviewstore.ForgeReply
	authors  []reviews.ReviewAuthor
	approved []reviews.ApproveForgeReplyParams
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
			name: "an agent drafting on a thread that is not its own",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.ProposeForgeReply(ctx, connect.NewRequest(&agentv1.ProposeForgeReplyRequest{ThreadId: betaID, Body: "done"}))

				return err
			},
			err:      reviews.ErrReviewNotYours,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name: "an agent approving a draft",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.ApproveForgeReply(ctx, connect.NewRequest(&agentv1.ApproveForgeReplyRequest{Id: betaID}))

				return err
			},
			err:      reviews.ErrReviewHumanOnly,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name: "an agent replying on the forge",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.ReplyOnForge(ctx, connect.NewRequest(&agentv1.ReplyOnForgeRequest{ThreadId: betaID, Body: "done"}))

				return err
			},
			err:      reviews.ErrReviewHumanOnly,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name: "an agent retrying a reply",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.RetryForgeReply(ctx, connect.NewRequest(&agentv1.RetryForgeReplyRequest{Id: betaID}))

				return err
			},
			err:      reviews.ErrReviewHumanOnly,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name: "an agent discarding a draft",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.DiscardForgeReply(ctx, connect.NewRequest(&agentv1.DiscardForgeReplyRequest{Id: betaID}))

				return err
			},
			err:      reviews.ErrReviewHumanOnly,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name: "asking for a draft on a thread nothing holds",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.RequestForgeReply(ctx, connect.NewRequest(&agentv1.RequestForgeReplyRequest{ThreadId: betaID}))

				return err
			},
			err:      reviews.ErrReviewThreadNotFound,
			wantCode: connect.CodeNotFound,
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
		"proposing": func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
			_, err := svc.ProposeForgeReply(ctx, connect.NewRequest(&agentv1.ProposeForgeReplyRequest{ThreadId: betaID, Body: "done"}))

			return err
		},
		"replying": func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
			_, err := svc.ReplyOnForge(ctx, connect.NewRequest(&agentv1.ReplyOnForgeRequest{ThreadId: betaID, Body: "done"}))

			return err
		},
		"approving": func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
			_, err := svc.ApproveForgeReply(ctx, connect.NewRequest(&agentv1.ApproveForgeReplyRequest{Id: betaID}))

			return err
		},
		"retrying": func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
			_, err := svc.RetryForgeReply(ctx, connect.NewRequest(&agentv1.RetryForgeReplyRequest{Id: betaID}))

			return err
		},
		"discarding": func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
			_, err := svc.DiscardForgeReply(ctx, connect.NewRequest(&agentv1.DiscardForgeReplyRequest{Id: betaID}))

			return err
		},
		"requesting": func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
			_, err := svc.RequestForgeReply(ctx, connect.NewRequest(&agentv1.RequestForgeReplyRequest{ThreadId: betaID}))

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

// TestApproveForgeReplyPassesTheEdit pins that the person's edit reaches the service only when they made one.
func TestApproveForgeReplyPassesTheEdit(t *testing.T) {
	replier := &fakeForgeReplier{}
	svc := newForgeReplyHandler(replier)

	_, err := svc.ApproveForgeReply(t.Context(), connect.NewRequest(&agentv1.ApproveForgeReplyRequest{Id: betaID}))
	require.NoError(t, err)
	_, err = svc.ApproveForgeReply(t.Context(), connect.NewRequest(&agentv1.ApproveForgeReplyRequest{Id: betaID, Body: xptr.Ptr("thanks")}))
	require.NoError(t, err)

	require.Len(t, replier.approved, 2)
	assert.Nil(t, replier.approved[0].Body)
	assert.Equal(t, xptr.Ptr("thanks"), replier.approved[1].Body)
}

// TestForgeReplyDrawsWhatTheServiceHeld pins the reply a call answers with.
func TestForgeReplyDrawsWhatTheServiceHeld(t *testing.T) {
	svc := newForgeReplyHandler(&fakeForgeReplier{reply: reviewstore.ForgeReply{
		ID:                     "reply-1",
		ThreadID:               "thread-1",
		State:                  reviewstore.ForgeReplySent,
		AuthorKind:             reviewstore.AuthorAgent,
		AuthorAgentID:          xptr.Ptr(alphaID),
		ProposedBody:           xptr.Ptr("added the guard"),
		Body:                   "added the guard, thanks",
		ApprovedAt:             &reviewedAt,
		SentAt:                 &reviewedAt,
		ForgeCommentProviderID: xptr.Ptr("PRRC_posted"),
		CreatedAt:              reviewedAt,
	}})

	got, err := svc.ApproveForgeReply(t.Context(), connect.NewRequest(&agentv1.ApproveForgeReplyRequest{Id: "reply-1"}))
	require.NoError(t, err)

	want := &agentv1.ForgeReply{
		Id:             "reply-1",
		ThreadId:       "thread-1",
		State:          agentv1.ForgeReplyState_FORGE_REPLY_STATE_SENT,
		AuthorKind:     agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT,
		AuthorAgentId:  xptr.Ptr(alphaID),
		ProposedBody:   xptr.Ptr("added the guard"),
		Body:           "added the guard, thanks",
		ApprovedAt:     dates.Stamp(reviewedAt),
		SentAt:         dates.Stamp(reviewedAt),
		ForgeCommentId: xptr.Ptr("PRRC_posted"),
		CreatedAt:      dates.Stamp(reviewedAt),
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

// ProposeForgeReply records the author and answers the reply it holds.
func (f *fakeForgeReplier) ProposeForgeReply(_ context.Context, params reviews.ProposeForgeReplyParams) (*reviewstore.ForgeReply, error) {
	return f.answer(params.Author)
}

// ReplyOnForge records the author and answers the reply it holds.
func (f *fakeForgeReplier) ReplyOnForge(_ context.Context, params reviews.ReplyOnForgeParams) (*reviewstore.ForgeReply, error) {
	return f.answer(params.Author)
}

// ApproveForgeReply records the approval and answers the reply it holds.
func (f *fakeForgeReplier) ApproveForgeReply(_ context.Context, params reviews.ApproveForgeReplyParams) (*reviewstore.ForgeReply, error) {
	f.approved = append(f.approved, params)

	return f.answer(params.Author)
}

// RetryForgeReply records the author and answers the reply it holds.
func (f *fakeForgeReplier) RetryForgeReply(_ context.Context, _ string, author reviews.ReviewAuthor) (*reviewstore.ForgeReply, error) {
	return f.answer(author)
}

// DiscardForgeReply records the author and answers err.
func (f *fakeForgeReplier) DiscardForgeReply(_ context.Context, _ string, author reviews.ReviewAuthor) error {
	_, err := f.answer(author)

	return err
}

// RequestForgeReply records the author and answers err.
func (f *fakeForgeReplier) RequestForgeReply(_ context.Context, _ string, author reviews.ReviewAuthor) error {
	_, err := f.answer(author)

	return err
}
