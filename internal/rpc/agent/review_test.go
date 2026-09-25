package agent

import (
	"context"
	"testing"
	"time"

	"komrad/internal/agents"
	"komrad/internal/caller"
	"komrad/internal/mail"
	"komrad/internal/reviews"
	reviewstore "komrad/internal/store/review"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1komrad"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
)

var reviewedAt = time.Date(2026, time.September, 24, 9, 0, 0, 0, time.UTC)

// fakeReviewer answers every review call with err or with the rows it holds.
type fakeReviewer struct {
	err      error
	review   reviewstore.Review
	round    reviewstore.Round
	thread   reviewstore.Thread
	message  reviewstore.Message
	viewed   *reviewstore.ViewedFile
	listed   []reviews.ListReviewThreadsParams
	released []reviews.ReleaseReviewParams
}

// TestReviewRefusalsMapToCodes pins the code each refusal of the review service reaches the caller as.
func TestReviewRefusalsMapToCodes(t *testing.T) {
	tests := []struct {
		name     string
		call     func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error
		err      error
		asNobody bool
		wantCode connect.Code
	}{
		{
			name: "listing threads with no agent named and no caller identity",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.ListReviewThreads(ctx, connect.NewRequest(&agentv1.ListReviewThreadsRequest{}))

				return err
			},
			asNobody: true,
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name: "listing the threads of an agent that does not exist",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.ListReviewThreads(ctx, connect.NewRequest(&agentv1.ListReviewThreadsRequest{}))

				return err
			},
			err:      agents.ErrAgentNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name: "a comment read from a snapshot komrad never froze",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.CreateReviewThread(ctx, connect.NewRequest(&agentv1.CreateReviewThreadRequest{AgentId: betaID}))

				return err
			},
			err:      reviews.ErrReviewNotCurrent,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name: "a second review before the first was answered",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.CreateReviewThread(ctx, connect.NewRequest(&agentv1.CreateReviewThreadRequest{AgentId: betaID}))

				return err
			},
			err:      reviews.ErrReviewNotAnswered,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name: "a reply in a thread nothing holds",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.AddReviewMessage(ctx, connect.NewRequest(&agentv1.AddReviewMessageRequest{ThreadId: betaID}))

				return err
			},
			err:      reviews.ErrReviewThreadNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name: "rewriting somebody else's words",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.EditReviewMessage(ctx, connect.NewRequest(&agentv1.EditReviewMessageRequest{Id: betaID}))

				return err
			},
			err:      reviews.ErrReviewNotYours,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name: "an agent marking a thread read",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.MarkReviewThreadRead(ctx, connect.NewRequest(&agentv1.MarkReviewThreadReadRequest{Id: betaID}))

				return err
			},
			err:      reviews.ErrReviewHumanOnly,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name: "settling a comment on the caller's own work",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.SetReviewThreadResolved(ctx, connect.NewRequest(&agentv1.SetReviewThreadResolvedRequest{Id: betaID}))

				return err
			},
			err:      reviews.ErrReviewNotYours,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name: "a round of an agent with no directory",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.OpenReviewRound(ctx, connect.NewRequest(&agentv1.OpenReviewRoundRequest{AgentId: betaID}))

				return err
			},
			err:      reviews.ErrReviewNoRound,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name: "the rounds of an agent the caller is not reviewing",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.ListReviewRounds(ctx, connect.NewRequest(&agentv1.ListReviewRoundsRequest{AgentId: betaID}))

				return err
			},
			err:      reviews.ErrReviewNotEntitled,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name: "the reviews of an agent the caller is not reviewing",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.ListReviews(ctx, connect.NewRequest(&agentv1.ListReviewsRequest{AgentId: betaID}))

				return err
			},
			err:      reviews.ErrReviewNotEntitled,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name: "a review nothing holds",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.GetReview(ctx, connect.NewRequest(&agentv1.GetReviewRequest{ReviewId: betaID}))

				return err
			},
			err:      reviews.ErrReviewNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name: "releasing notes with nothing in them",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.ReleaseReview(ctx, connect.NewRequest(&agentv1.ReleaseReviewRequest{AgentId: betaID}))

				return err
			},
			err:      reviews.ErrReviewEmpty,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name: "releasing to an agent whose mailbox is full",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.ReleaseReview(ctx, connect.NewRequest(&agentv1.ReleaseReviewRequest{AgentId: betaID}))

				return err
			},
			err:      mail.ErrMailboxFull,
			wantCode: connect.CodeResourceExhausted,
		},
		{
			name: "an agent marking a file viewed",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.SetReviewFileViewed(ctx, connect.NewRequest(&agentv1.SetReviewFileViewedRequest{AgentId: betaID}))

				return err
			},
			err:      reviews.ErrReviewHumanOnly,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name: "an agent listing the files viewed",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.ListReviewViewedFiles(ctx, connect.NewRequest(&agentv1.ListReviewViewedFilesRequest{AgentId: betaID}))

				return err
			},
			err:      reviews.ErrReviewHumanOnly,
			wantCode: connect.CodePermissionDenied,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newReviewHandler(&fakeReviewer{err: tt.err})

			ctx := t.Context()
			if !tt.asNobody {
				ctx = caller.With(ctx, caller.Caller{AgentID: alphaID})
			}

			err := tt.call(ctx, svc)
			assert.Equal(t, tt.wantCode, connect.CodeOf(err))
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
			}
		})
	}
}

// TestReviewCallsAnswerWhatTheServiceDid pins that each call answers with the row the service handed back.
func TestReviewCallsAnswerWhatTheServiceDid(t *testing.T) {
	tests := []struct {
		name   string
		call   func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) (string, error)
		wantID string
	}{
		{
			name: "opening a thread",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) (string, error) {
				got, err := svc.CreateReviewThread(ctx, connect.NewRequest(&agentv1.CreateReviewThreadRequest{AgentId: betaID}))
				if err != nil {
					return "", err
				}

				return got.Msg.GetThread().GetId(), nil
			},
			wantID: "thread-1",
		},
		{
			name: "replying in a thread",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) (string, error) {
				got, err := svc.AddReviewMessage(ctx, connect.NewRequest(&agentv1.AddReviewMessageRequest{ThreadId: "thread-1"}))
				if err != nil {
					return "", err
				}

				return got.Msg.GetMessage().GetId(), nil
			},
			wantID: "message-1",
		},
		{
			name: "rewriting a message",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) (string, error) {
				got, err := svc.EditReviewMessage(ctx, connect.NewRequest(&agentv1.EditReviewMessageRequest{Id: "message-1"}))
				if err != nil {
					return "", err
				}

				return got.Msg.GetMessage().GetId(), nil
			},
			wantID: "message-1",
		},
		{
			name: "marking a thread read",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) (string, error) {
				got, err := svc.MarkReviewThreadRead(ctx, connect.NewRequest(&agentv1.MarkReviewThreadReadRequest{Id: "thread-1"}))
				if err != nil {
					return "", err
				}

				return got.Msg.GetThread().GetId(), nil
			},
			wantID: "thread-1",
		},
		{
			name: "settling a thread",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) (string, error) {
				got, err := svc.SetReviewThreadResolved(ctx, connect.NewRequest(&agentv1.SetReviewThreadResolvedRequest{Id: "thread-1"}))
				if err != nil {
					return "", err
				}

				return got.Msg.GetThread().GetId(), nil
			},
			wantID: "thread-1",
		},
		{
			name: "opening a round",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) (string, error) {
				got, err := svc.OpenReviewRound(ctx, connect.NewRequest(&agentv1.OpenReviewRoundRequest{AgentId: betaID}))
				if err != nil {
					return "", err
				}

				return got.Msg.GetRound().GetId(), nil
			},
			wantID: "round-1",
		},
		{
			name: "listing the rounds",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) (string, error) {
				got, err := svc.ListReviewRounds(ctx, connect.NewRequest(&agentv1.ListReviewRoundsRequest{AgentId: betaID}))
				if err != nil {
					return "", err
				}

				if len(got.Msg.GetRounds()) == 0 {
					return "", nil
				}

				return got.Msg.GetRounds()[0].GetId(), nil
			},
			wantID: "round-1",
		},
		{
			name: "listing the reviews",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) (string, error) {
				got, err := svc.ListReviews(ctx, connect.NewRequest(&agentv1.ListReviewsRequest{AgentId: betaID}))
				if err != nil {
					return "", err
				}

				if len(got.Msg.GetReviews()) == 0 {
					return "", nil
				}

				return got.Msg.GetReviews()[0].GetId(), nil
			},
			wantID: "review-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newReviewHandler(&fakeReviewer{
				review:  reviewstore.Review{ID: "review-1"},
				round:   reviewstore.Round{ID: "round-1"},
				thread:  reviewstore.Thread{ID: "thread-1"},
				message: reviewstore.Message{ID: "message-1"},
			})

			id, err := tt.call(t.Context(), svc)
			require.NoError(t, err)
			assert.Equal(t, tt.wantID, id)
		})
	}
}

// TestListReviewThreadsNamesTheCaller pins whose review a listing reads and as whom.
func TestListReviewThreadsNamesTheCaller(t *testing.T) {
	tests := []struct {
		name       string
		callerID   string
		agentID    *string
		wantListed reviews.ListReviewThreadsParams
	}{
		{
			name:     "an agent naming nobody reads its own",
			callerID: alphaID,
			wantListed: reviews.ListReviewThreadsParams{
				AgentID: alphaID,
				Author: reviews.ReviewAuthor{
					Kind:    reviewstore.AuthorAgent,
					AgentID: xptr.Ptr(alphaID),
				},
			},
		},
		{
			name:     "an agent reading another's",
			callerID: alphaID,
			agentID:  xptr.Ptr(betaID),
			wantListed: reviews.ListReviewThreadsParams{
				AgentID: betaID,
				Author: reviews.ReviewAuthor{
					Kind:    reviewstore.AuthorAgent,
					AgentID: xptr.Ptr(alphaID),
				},
			},
		},
		{
			name:    "the panel reads as the person",
			agentID: xptr.Ptr(betaID),
			wantListed: reviews.ListReviewThreadsParams{
				AgentID: betaID,
				Author:  reviews.ReviewAuthor{Kind: reviewstore.AuthorHuman},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reviewer := &fakeReviewer{}
			svc := newReviewHandler(reviewer)

			ctx := t.Context()
			if tt.callerID != "" {
				ctx = caller.With(ctx, caller.Caller{AgentID: tt.callerID})
			}

			_, err := svc.ListReviewThreads(ctx, connect.NewRequest(&agentv1.ListReviewThreadsRequest{AgentId: tt.agentID}))
			require.NoError(t, err)
			assert.Equal(t, []reviews.ListReviewThreadsParams{tt.wantListed}, reviewer.listed)
		})
	}
}

// TestReleaseReviewPassesTheVerdict pins which verdict each wire value is released as.
func TestReleaseReviewPassesTheVerdict(t *testing.T) {
	tests := []struct {
		name        string
		verdict     agentv1.ReviewVerdict
		wantVerdict reviewstore.Verdict
	}{
		{
			name:        "no verdict at all is a comment",
			verdict:     agentv1.ReviewVerdict_REVIEW_VERDICT_UNSPECIFIED,
			wantVerdict: reviewstore.VerdictCommented,
		},
		{
			name:        "a comment",
			verdict:     agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED,
			wantVerdict: reviewstore.VerdictCommented,
		},
		{
			name:        "changes requested",
			verdict:     agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED,
			wantVerdict: reviewstore.VerdictChangesRequested,
		},
		{
			name:        "an approval",
			verdict:     agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED,
			wantVerdict: reviewstore.VerdictApproved,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reviewer := &fakeReviewer{}
			svc := newReviewHandler(reviewer)

			_, err := svc.ReleaseReview(t.Context(), connect.NewRequest(&agentv1.ReleaseReviewRequest{
				AgentId:   betaID,
				Verdict:   tt.verdict,
				Body:      "two things before it goes",
				Interrupt: true,
			}))
			require.NoError(t, err)

			assert.Equal(t, []reviews.ReleaseReviewParams{{
				AgentID:   betaID,
				Verdict:   tt.wantVerdict,
				Body:      "two things before it goes",
				Interrupt: true,
				Author:    reviews.ReviewAuthor{Kind: reviewstore.AuthorHuman},
			}}, reviewer.released)
		})
	}
}

// TestGetReviewRendersTheReview pins what a review read whole looks like on the wire.
func TestGetReviewRendersTheReview(t *testing.T) {
	reviewer := &fakeReviewer{
		review: reviewstore.Review{
			ID:            "review-1",
			AgentID:       betaID,
			RoundID:       "round-1",
			RoundOrdinal:  1,
			AuthorKind:    reviewstore.AuthorAgent,
			AuthorAgentID: xptr.Ptr(alphaID),
			Verdict:       xptr.Ptr(reviewstore.VerdictChangesRequested),
			ReleasedAt:    &reviewedAt,
			DeliveredAt:   &reviewedAt,
			Body:          "two things before it goes",
			OpenThreads:   1,
			CreatedAt:     reviewedAt,
			UpdatedAt:     reviewedAt,
		},
		round: reviewstore.Round{
			ID:          "round-1",
			AgentID:     betaID,
			Ordinal:     1,
			BaseSHA:     "base-sha",
			SnapshotSHA: "snapshot-sha",
			CreatedAt:   reviewedAt,
		},
		thread: reviewstore.Thread{
			ID:       "thread-1",
			AgentID:  betaID,
			FilePath: "greet.go",
		},
	}
	svc := newReviewHandler(reviewer)

	got, err := svc.GetReview(caller.With(t.Context(), caller.Caller{AgentID: betaID}), connect.NewRequest(&agentv1.GetReviewRequest{
		ReviewId: "review-1",
	}))
	require.NoError(t, err)

	assert.Empty(t, cmp.Diff(&agentv1.GetReviewResponse{
		Review: &agentv1.Review{
			Id:            "review-1",
			AgentId:       betaID,
			RoundId:       "round-1",
			RoundOrdinal:  1,
			AuthorKind:    agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT,
			AuthorAgentId: xptr.Ptr(alphaID),
			Verdict:       agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED,
			Body:          "two things before it goes",
			OpenThreads:   1,
			ReleasedAt:    "2026-09-24T09:00:00Z",
			DeliveredAt:   "2026-09-24T09:00:00Z",
			CreatedAt:     "2026-09-24T09:00:00Z",
			UpdatedAt:     "2026-09-24T09:00:00Z",
		},
		Round: &agentv1.ReviewRound{
			Id:          "round-1",
			AgentId:     betaID,
			Ordinal:     1,
			BaseSha:     "base-sha",
			SnapshotSha: "snapshot-sha",
			CreatedAt:   "2026-09-24T09:00:00Z",
		},
		Threads: []*agentv1.ReviewThreadInContext{{
			Thread: &agentv1.ReviewThread{
				Id:       "thread-1",
				AgentId:  betaID,
				FilePath: "greet.go",
			},
			Context: []string{">> 1  package greet"},
		}},
	}, got.Msg, protocmp.Transform()))
}

// TestSetReviewFileViewedRendersTheMark pins that a mark taken back answers with no file.
func TestSetReviewFileViewedRendersTheMark(t *testing.T) {
	tests := []struct {
		name       string
		viewed     *reviewstore.ViewedFile
		wantViewed *agentv1.ReviewViewedFile
	}{
		{
			name: "a mark taken back",
		},
		{
			name: "a file marked read",
			viewed: &reviewstore.ViewedFile{
				AgentID:     betaID,
				FilePath:    "greet.go",
				Fingerprint: "abc123",
				ViewedAt:    reviewedAt,
			},
			wantViewed: &agentv1.ReviewViewedFile{
				AgentId:     betaID,
				FilePath:    "greet.go",
				Fingerprint: "abc123",
				ViewedAt:    "2026-09-24T09:00:00Z",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newReviewHandler(&fakeReviewer{viewed: tt.viewed})

			got, err := svc.SetReviewFileViewed(t.Context(), connect.NewRequest(&agentv1.SetReviewFileViewedRequest{
				AgentId:  betaID,
				FilePath: "greet.go",
				Viewed:   tt.viewed != nil,
			}))
			require.NoError(t, err)
			assert.Empty(t, cmp.Diff(tt.wantViewed, got.Msg.GetViewed(), protocmp.Transform()))
		})
	}
}

func newReviewHandler(reviewer *fakeReviewer) agentv1komrad.AgentServiceAdapter {
	return agentv1komrad.AgentServiceAdapter{Impl: NewHandler(NewHandlerParams{
		Review: reviewer,
	})}
}

// ListReviewThreads records the listing and answers the thread it holds.
func (f *fakeReviewer) ListReviewThreads(_ context.Context, params reviews.ListReviewThreadsParams) ([]reviews.ReviewThreadWithMessages, error) {
	f.listed = append(f.listed, params)
	if f.err != nil {
		return nil, f.err
	}

	return []reviews.ReviewThreadWithMessages{{Thread: f.thread}}, nil
}

// CreateReviewThread answers the thread it holds.
func (f *fakeReviewer) CreateReviewThread(context.Context, reviews.CreateReviewThreadParams) (*reviews.ReviewThreadWithMessages, error) {
	if f.err != nil {
		return nil, f.err
	}

	return &reviews.ReviewThreadWithMessages{Thread: f.thread}, nil
}

// AddReviewMessage answers the message it holds.
func (f *fakeReviewer) AddReviewMessage(context.Context, reviews.AddReviewMessageParams) (*reviewstore.Message, error) {
	if f.err != nil {
		return nil, f.err
	}

	return &f.message, nil
}

// EditReviewMessage answers the message it holds.
func (f *fakeReviewer) EditReviewMessage(context.Context, reviews.EditReviewMessageParams) (*reviewstore.Message, error) {
	if f.err != nil {
		return nil, f.err
	}

	return &f.message, nil
}

// MarkReviewThreadRead answers the thread it holds.
func (f *fakeReviewer) MarkReviewThreadRead(context.Context, reviews.MarkReviewThreadReadParams) (*reviewstore.Thread, error) {
	if f.err != nil {
		return nil, f.err
	}

	return &f.thread, nil
}

// SetReviewThreadResolved answers the thread it holds.
func (f *fakeReviewer) SetReviewThreadResolved(context.Context, reviews.SetReviewThreadResolvedParams) (*reviewstore.Thread, error) {
	if f.err != nil {
		return nil, f.err
	}

	return &f.thread, nil
}

// OpenRound answers the round it holds.
func (f *fakeReviewer) OpenRound(context.Context, string, reviews.ReviewAuthor) (*reviewstore.Round, error) {
	if f.err != nil {
		return nil, f.err
	}

	return &f.round, nil
}

// ListRounds answers the round it holds.
func (f *fakeReviewer) ListRounds(context.Context, string, reviews.ReviewAuthor) ([]reviewstore.Round, error) {
	if f.err != nil {
		return nil, f.err
	}

	return []reviewstore.Round{f.round}, nil
}

// ListReviews answers the review it holds.
func (f *fakeReviewer) ListReviews(context.Context, string, reviews.ReviewAuthor) ([]reviewstore.Review, error) {
	if f.err != nil {
		return nil, f.err
	}

	return []reviewstore.Review{f.review}, nil
}

// GetReview answers the review, round and thread it holds, the thread drawn over one line of code.
func (f *fakeReviewer) GetReview(context.Context, string, reviews.ReviewAuthor) (*reviews.ReviewInFull, error) {
	if f.err != nil {
		return nil, f.err
	}

	return &reviews.ReviewInFull{
		Review: f.review,
		Round:  f.round,
		Threads: []reviews.ReviewThreadInContext{{
			Thread:  f.thread,
			Context: []string{">> 1  package greet"},
		}},
	}, nil
}

// ReleaseReview records the release and answers the review it holds.
func (f *fakeReviewer) ReleaseReview(_ context.Context, params reviews.ReleaseReviewParams) (*reviews.ReleaseReviewResult, error) {
	f.released = append(f.released, params)
	if f.err != nil {
		return nil, f.err
	}

	return &reviews.ReleaseReviewResult{Review: f.review}, nil
}

// SetReviewFileViewed answers the mark it holds.
func (f *fakeReviewer) SetReviewFileViewed(context.Context, reviews.SetReviewFileViewedParams) (*reviewstore.ViewedFile, error) {
	if f.err != nil {
		return nil, f.err
	}

	return f.viewed, nil
}

// ListReviewViewedFiles answers nothing viewed.
func (f *fakeReviewer) ListReviewViewedFiles(context.Context, reviews.ListReviewViewedFilesParams) ([]reviewstore.ViewedFile, error) {
	if f.err != nil {
		return nil, f.err
	}

	return nil, nil
}
