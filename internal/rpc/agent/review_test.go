package agent

import (
	"context"
	"testing"
	"time"

	"komrad/internal/caller"
	"komrad/internal/mail"
	"komrad/internal/reviews"
	"komrad/internal/reviews/history"
	"komrad/internal/store"
	mailstore "komrad/internal/store/mail"
	reviewstore "komrad/internal/store/review"
	"komrad/lib/dates"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1komrad"
	"komrad/proto/gen/oneofs"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var reviewedAt = time.Date(2026, time.September, 24, 9, 0, 0, 0, time.UTC)

var betaTask = oneofs.Task{TaskId: xptr.Ptr(betaTaskID)}

var (
	betaTaskID       = uuid.MustParse("01a0d1d7-0000-7000-8000-0000000000fb")
	firstSnapshotID  = uuid.MustParse("01a0d1d7-0000-7000-8000-0000000000a1")
	secondSnapshotID = uuid.MustParse("01a0d1d7-0000-7000-8000-0000000000a2")
)

// fakeReviewer answers every review call with err or with the rows it holds.
type fakeReviewer struct {
	err      error
	review   reviewstore.Review
	snapshot reviewstore.Snapshot
	thread   reviewstore.Thread
	message  reviewstore.Message
	viewed   *reviewstore.ViewedFile
	listed   []reviews.ListReviewThreadsParams
	released []reviews.ReleaseReviewParams
	record   history.Record
	opened   []reviews.GetHistoryParams
	read     []uuid.UUID
	mail     []mailstore.Mail
}

// TestReviewRefusalsMapToCodes pins the code each refusal of the review service reaches the caller as.
func TestReviewRefusalsMapToCodes(t *testing.T) {
	tests := []struct {
		name     string
		call     func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error
		err      error
		wantCode connect.Code
	}{
		{
			name: "listing the threads of a task that does not exist",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.ListReviewThreads(ctx, connect.NewRequest(&agentv1.ListReviewThreadsRequest{}))

				return err
			},
			err:      store.ErrNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name: "a comment read from a snapshot komrad never froze",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.CreateReviewThread(ctx, connect.NewRequest(&agentv1.CreateReviewThreadRequest{Task: &agentv1.CreateReviewThreadRequest_TaskId{TaskId: betaTaskID.String()}}))

				return err
			},
			err:      reviews.ErrReviewNotCurrent,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name: "a reply in a thread nothing holds",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.AddReviewMessage(ctx, connect.NewRequest(&agentv1.AddReviewMessageRequest{ThreadId: betaID.String()}))

				return err
			},
			err:      reviews.ErrReviewThreadNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name: "rewriting somebody else's words",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.EditReviewMessage(ctx, connect.NewRequest(&agentv1.EditReviewMessageRequest{Id: betaID.String()}))

				return err
			},
			err:      reviews.ErrReviewNotYours,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name: "accepting a forge comment already accepted",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.AcceptForgeMessage(ctx, connect.NewRequest(&agentv1.AcceptForgeMessageRequest{MessageId: betaID.String()}))

				return err
			},
			err:      reviews.ErrForgeMessageAccepted,
			wantCode: connect.CodeAlreadyExists,
		},
		{
			name: "an agent marking a thread read",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.MarkReviewThreadRead(ctx, connect.NewRequest(&agentv1.MarkReviewThreadReadRequest{Id: betaID.String()}))

				return err
			},
			err:      reviews.ErrReviewHumanOnly,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name: "settling a comment on the caller's own work",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.SetReviewThreadResolved(ctx, connect.NewRequest(&agentv1.SetReviewThreadResolvedRequest{Id: betaID.String()}))

				return err
			},
			err:      reviews.ErrReviewNotYours,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name: "a snapshot of an agent with no directory",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.TakeReviewSnapshot(ctx, connect.NewRequest(&agentv1.TakeReviewSnapshotRequest{Task: &agentv1.TakeReviewSnapshotRequest_TaskId{TaskId: betaTaskID.String()}}))

				return err
			},
			err:      reviews.ErrReviewNoSnapshot,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name: "the snapshots of an agent the caller is not reviewing",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.ListReviewSnapshots(ctx, connect.NewRequest(&agentv1.ListReviewSnapshotsRequest{Task: &agentv1.ListReviewSnapshotsRequest_TaskId{TaskId: betaTaskID.String()}}))

				return err
			},
			err:      reviews.ErrReviewNotEntitled,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name: "the reviews of an agent the caller is not reviewing",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.ListReviews(ctx, connect.NewRequest(&agentv1.ListReviewsRequest{Task: &agentv1.ListReviewsRequest_TaskId{TaskId: betaTaskID.String()}}))

				return err
			},
			err:      reviews.ErrReviewNotEntitled,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name: "a review nothing holds",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.GetReview(ctx, connect.NewRequest(&agentv1.GetReviewRequest{ReviewId: betaID.String()}))

				return err
			},
			err:      reviews.ErrReviewNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name: "releasing notes with nothing in them",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.ReleaseReview(ctx, connect.NewRequest(&agentv1.ReleaseReviewRequest{Task: &agentv1.ReleaseReviewRequest_TaskId{TaskId: betaTaskID.String()}}))

				return err
			},
			err:      reviews.ErrReviewEmpty,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name: "releasing to an agent whose mailbox is full",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.ReleaseReview(ctx, connect.NewRequest(&agentv1.ReleaseReviewRequest{Task: &agentv1.ReleaseReviewRequest_TaskId{TaskId: betaTaskID.String()}}))

				return err
			},
			err:      mail.ErrMailboxFull,
			wantCode: connect.CodeResourceExhausted,
		},
		{
			name: "an agent marking a file viewed",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.SetReviewFileViewed(ctx, connect.NewRequest(&agentv1.SetReviewFileViewedRequest{Task: &agentv1.SetReviewFileViewedRequest_TaskId{TaskId: betaTaskID.String()}}))

				return err
			},
			err:      reviews.ErrReviewHumanOnly,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name: "an agent listing the files viewed",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
				_, err := svc.ListReviewViewedFiles(ctx, connect.NewRequest(&agentv1.ListReviewViewedFilesRequest{Task: &agentv1.ListReviewViewedFilesRequest_TaskId{TaskId: betaTaskID.String()}}))

				return err
			},
			err:      reviews.ErrReviewHumanOnly,
			wantCode: connect.CodePermissionDenied,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newReviewHandler(&fakeReviewer{err: tt.err})

			err := tt.call(caller.With(t.Context(), caller.Caller{AgentID: alphaID}), svc)
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
				got, err := svc.CreateReviewThread(ctx, connect.NewRequest(&agentv1.CreateReviewThreadRequest{Task: &agentv1.CreateReviewThreadRequest_TaskId{TaskId: betaTaskID.String()}}))
				if err != nil {
					return "", err
				}

				return got.Msg.GetThread().GetId(), nil
			},
			wantID: threadID.String(),
		},
		{
			name: "replying in a thread",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) (string, error) {
				got, err := svc.AddReviewMessage(ctx, connect.NewRequest(&agentv1.AddReviewMessageRequest{ThreadId: betaID.String()}))
				if err != nil {
					return "", err
				}

				return got.Msg.GetMessage().GetId(), nil
			},
			wantID: messageID.String(),
		},
		{
			name: "rewriting a message",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) (string, error) {
				got, err := svc.EditReviewMessage(ctx, connect.NewRequest(&agentv1.EditReviewMessageRequest{Id: betaID.String()}))
				if err != nil {
					return "", err
				}

				return got.Msg.GetMessage().GetId(), nil
			},
			wantID: messageID.String(),
		},
		{
			name: "marking a thread read",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) (string, error) {
				got, err := svc.MarkReviewThreadRead(ctx, connect.NewRequest(&agentv1.MarkReviewThreadReadRequest{Id: betaID.String()}))
				if err != nil {
					return "", err
				}

				return got.Msg.GetThread().GetId(), nil
			},
			wantID: threadID.String(),
		},
		{
			name: "settling a thread",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) (string, error) {
				got, err := svc.SetReviewThreadResolved(ctx, connect.NewRequest(&agentv1.SetReviewThreadResolvedRequest{Id: betaID.String()}))
				if err != nil {
					return "", err
				}

				return got.Msg.GetThread().GetId(), nil
			},
			wantID: threadID.String(),
		},
		{
			name: "taking a snapshot",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) (string, error) {
				got, err := svc.TakeReviewSnapshot(ctx, connect.NewRequest(&agentv1.TakeReviewSnapshotRequest{Task: &agentv1.TakeReviewSnapshotRequest_TaskId{TaskId: betaTaskID.String()}}))
				if err != nil {
					return "", err
				}

				return got.Msg.GetSnapshot().GetId(), nil
			},
			wantID: firstSnapshotID.String(),
		},
		{
			name: "listing the snapshots",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) (string, error) {
				got, err := svc.ListReviewSnapshots(ctx, connect.NewRequest(&agentv1.ListReviewSnapshotsRequest{Task: &agentv1.ListReviewSnapshotsRequest_TaskId{TaskId: betaTaskID.String()}}))
				if err != nil {
					return "", err
				}

				if len(got.Msg.GetSnapshots()) == 0 {
					return "", nil
				}

				return got.Msg.GetSnapshots()[0].GetId(), nil
			},
			wantID: firstSnapshotID.String(),
		},
		{
			name: "listing the reviews",
			call: func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) (string, error) {
				got, err := svc.ListReviews(ctx, connect.NewRequest(&agentv1.ListReviewsRequest{Task: &agentv1.ListReviewsRequest_TaskId{TaskId: betaTaskID.String()}}))
				if err != nil {
					return "", err
				}

				if len(got.Msg.GetReviews()) == 0 {
					return "", nil
				}

				return got.Msg.GetReviews()[0].GetId(), nil
			},
			wantID: reviewID.String(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newReviewHandler(&fakeReviewer{
				review:   reviewstore.Review{ID: reviewID},
				snapshot: reviewstore.Snapshot{ID: firstSnapshotID},
				thread:   reviewstore.Thread{ID: threadID},
				message:  reviewstore.Message{ID: messageID},
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
		callerID   uuid.UUID
		task       *string
		wantListed reviews.ListReviewThreadsParams
	}{
		{
			name:     "an agent naming nobody reads its own",
			callerID: alphaID,
			wantListed: reviews.ListReviewThreadsParams{
				Author: reviews.ReviewAuthor{
					Kind:    reviewstore.AuthorAgent,
					AgentID: xptr.Ptr(alphaID),
				},
			},
		},
		{
			name:     "an agent reading another's",
			callerID: alphaID,
			task:     xptr.Ptr(betaTaskID.String()),
			wantListed: reviews.ListReviewThreadsParams{
				Task: betaTask,
				Author: reviews.ReviewAuthor{
					Kind:    reviewstore.AuthorAgent,
					AgentID: xptr.Ptr(alphaID),
				},
			},
		},
		{
			name: "the panel reads as the person",
			task: xptr.Ptr(betaTaskID.String()),
			wantListed: reviews.ListReviewThreadsParams{
				Task:   betaTask,
				Author: reviews.ReviewAuthor{Kind: reviewstore.AuthorHuman},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reviewer := &fakeReviewer{}
			svc := newReviewHandler(reviewer)

			ctx := t.Context()
			if tt.callerID != uuid.Nil {
				ctx = caller.With(ctx, caller.Caller{AgentID: tt.callerID})
			}

			request := &agentv1.ListReviewThreadsRequest{}
			if tt.task != nil {
				request.Task = &agentv1.ListReviewThreadsRequest_TaskId{TaskId: *tt.task}
			}

			_, err := svc.ListReviewThreads(ctx, connect.NewRequest(request))
			require.NoError(t, err)
			assert.Equal(t, []reviews.ListReviewThreadsParams{tt.wantListed}, reviewer.listed)
		})
	}
}

// TestOpenReviewHistoryPassesTheSpan pins the span a request names reaching the service.
func TestOpenReviewHistoryPassesTheSpan(t *testing.T) {
	tests := []struct {
		name     string
		request  *agentv1.OpenReviewHistoryRequest
		wantSpan history.Span
	}{
		{
			name:    "no span is the whole change",
			request: &agentv1.OpenReviewHistoryRequest{Task: &agentv1.OpenReviewHistoryRequest_TaskId{TaskId: betaTaskID.String()}},
		},
		{
			name: "a range of snapshots",
			request: &agentv1.OpenReviewHistoryRequest{
				Task: &agentv1.OpenReviewHistoryRequest_TaskId{TaskId: betaTaskID.String()},
				Span: &agentv1.OpenReviewHistoryRequest_Snapshots{Snapshots: &agentv1.ReviewSnapshotSpan{FromSnapshotId: new(firstSnapshotID.String()), ToSnapshotId: new(secondSnapshotID.String())}},
			},
			wantSpan: history.Span{Snapshots: &history.SnapshotSpan{FromSnapshotID: firstSnapshotID, ToSnapshotID: secondSnapshotID}},
		},
		{
			name: "an answer",
			request: &agentv1.OpenReviewHistoryRequest{
				Task: &agentv1.OpenReviewHistoryRequest_TaskId{TaskId: betaTaskID.String()},
				Span: &agentv1.OpenReviewHistoryRequest_Answer{Answer: &agentv1.ReviewAnswerSpan{FromSnapshotId: firstSnapshotID.String(), ToSnapshotId: secondSnapshotID.String()}},
			},
			wantSpan: history.Span{Answer: &history.AnswerSpan{FromSnapshotID: firstSnapshotID, ToSnapshotID: secondSnapshotID}},
		},
		{
			name: "a comment's origin",
			request: &agentv1.OpenReviewHistoryRequest{
				Task: &agentv1.OpenReviewHistoryRequest_TaskId{TaskId: betaTaskID.String()},
				Span: &agentv1.OpenReviewHistoryRequest_Origin{Origin: &agentv1.ReviewOriginSpan{SnapshotId: firstSnapshotID.String(), BaseSha: "a"}},
			},
			wantSpan: history.Span{Origin: &history.OriginSpan{SnapshotID: firstSnapshotID, BaseSHA: "a"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reviewer := &fakeReviewer{}
			svc := newReviewHandler(reviewer)

			_, err := svc.OpenReviewHistory(t.Context(), connect.NewRequest(tt.request))
			require.NoError(t, err)
			assert.Equal(t, []reviews.GetHistoryParams{{
				Task:   betaTask,
				Span:   tt.wantSpan,
				Author: reviews.ReviewAuthor{Kind: reviewstore.AuthorHuman},
			}}, reviewer.opened)
		})
	}
}

// TestOpenReviewHistoryRendersTheHistory pins the history the service built reaching the wire.
func TestOpenReviewHistoryRendersTheHistory(t *testing.T) {
	released := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	reviewer := &fakeReviewer{
		snapshot: reviewstore.Snapshot{ID: secondSnapshotID, TaskID: betaTaskID, Ordinal: 2},
		record: history.Record{
			Span: history.ResolvedSpan{
				BaseSHA:   "base",
				HeadSHA:   "head",
				Replayed:  true,
				Conflicts: []string{"greet.go"},
			},
			Widened: &history.Widening{
				Reason:    history.WidenedAnswerAcrossRebase,
				Conflicts: []string{"greet.go", "second.go"},
			},
			Earlier:             &history.Snapshot{ID: firstSnapshotID},
			EarlierReleasedAt:   &released,
			ChangedSinceEarlier: 3,
			Traced:              map[uuid.UUID]history.Place{threadID: {FilePath: "greet.go", NewStart: 8}},
		},
	}
	svc := newReviewHandler(reviewer)

	got, err := svc.OpenReviewHistory(t.Context(), connect.NewRequest(&agentv1.OpenReviewHistoryRequest{Task: &agentv1.OpenReviewHistoryRequest_TaskId{TaskId: betaTaskID.String()}}))
	require.NoError(t, err)

	want := &agentv1.OpenReviewHistoryResponse{
		Snapshot: &agentv1.ReviewSnapshot{
			Id:        secondSnapshotID.String(),
			TaskId:    betaTaskID.String(),
			Ordinal:   2,
			CreatedAt: dates.Stamp(time.Time{}),
		},
		Snapshots: []*agentv1.ReviewSnapshot{{
			Id:        secondSnapshotID.String(),
			TaskId:    betaTaskID.String(),
			Ordinal:   2,
			CreatedAt: dates.Stamp(time.Time{}),
		}},
		Resolved: &agentv1.ReviewResolvedSpan{
			BaseSha:   "base",
			HeadSha:   "head",
			Replayed:  true,
			Conflicts: []string{"greet.go"},
		},
		Widened: &agentv1.ReviewWidening{
			Reason:    agentv1.ReviewWidenedReason_REVIEW_WIDENED_REASON_ANSWER_ACROSS_REBASE,
			Conflicts: []string{"greet.go", "second.go"},
		},
		EarlierSnapshotId:   firstSnapshotID.String(),
		EarlierReleasedAt:   dates.Stamp(released),
		ChangedSinceEarlier: 3,
		Traced:              map[string]*agentv1.ReviewTracedPlace{threadID.String(): {FilePath: "greet.go", NewStart: 8}},
	}
	assert.Empty(t, cmp.Diff(want, got.Msg, protocmp.Transform()))
}

// TestReleaseReviewPassesTheVerdict pins which verdict the handler releases for each wire value.
func TestReleaseReviewPassesTheVerdict(t *testing.T) {
	tests := []struct {
		name        string
		verdict     agentv1.ReviewVerdict
		wantVerdict reviewstore.Verdict
	}{
		{
			name:        "no verdict at all is left for the service to settle",
			verdict:     agentv1.ReviewVerdict_REVIEW_VERDICT_UNSPECIFIED,
			wantVerdict: "",
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
				Task:    &agentv1.ReleaseReviewRequest_TaskId{TaskId: betaTaskID.String()},
				Verdict: tt.verdict,
				Body:    "two things before it goes",
			}))
			require.NoError(t, err)

			assert.Equal(t, []reviews.ReleaseReviewParams{{
				Task:    betaTask,
				Verdict: tt.wantVerdict,
				Body:    "two things before it goes",
				Author:  reviews.ReviewAuthor{Kind: reviewstore.AuthorHuman},
			}}, reviewer.released)
		})
	}
}

// TestGetReviewRendersTheReview pins what a review read whole looks like on the wire.
func TestGetReviewRendersTheReview(t *testing.T) {
	reviewer := &fakeReviewer{
		review: reviewstore.Review{
			ID:              reviewID,
			TaskID:          betaTaskID,
			SnapshotID:      firstSnapshotID,
			SnapshotOrdinal: 1,
			AuthorKind:      reviewstore.AuthorAgent,
			AuthorAgentID:   xptr.Ptr(alphaID),
			Verdict:         xptr.Ptr(reviewstore.VerdictChangesRequested),
			ReleasedAt:      &reviewedAt,
			Body:            "two things before it goes",
			OpenThreads:     1,
			CreatedAt:       reviewedAt,
			UpdatedAt:       reviewedAt,
		},
		snapshot: reviewstore.Snapshot{
			ID:        firstSnapshotID,
			TaskID:    betaTaskID,
			Ordinal:   1,
			BaseSHA:   "base-sha",
			SHA:       "snapshot-sha",
			CreatedAt: reviewedAt,
		},
		thread: reviewstore.Thread{
			ID:         threadID,
			TaskID:     betaTaskID,
			SnapshotID: firstSnapshotID,
			FilePath:   "greet.go",
		},
	}
	svc := newReviewHandler(reviewer)

	got, err := svc.GetReview(caller.With(t.Context(), caller.Caller{AgentID: betaID}), connect.NewRequest(&agentv1.GetReviewRequest{
		ReviewId: betaID.String(),
	}))
	require.NoError(t, err)

	assert.Empty(t, cmp.Diff(&agentv1.GetReviewResponse{
		Review: &agentv1.Review{
			Id:              reviewID.String(),
			TaskId:          betaTaskID.String(),
			SnapshotId:      firstSnapshotID.String(),
			SnapshotOrdinal: 1,
			AuthorKind:      agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT,
			AuthorAgentId:   xptr.Ptr(alphaID.String()),
			Verdict:         agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED,
			Body:            "two things before it goes",
			OpenThreads:     1,
			ReleasedAt:      timestamppb.New(time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)),
			CreatedAt:       timestamppb.New(time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)),
			UpdatedAt:       timestamppb.New(time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)),
		},
		Snapshot: &agentv1.ReviewSnapshot{
			Id:        firstSnapshotID.String(),
			TaskId:    betaTaskID.String(),
			Ordinal:   1,
			BaseSha:   "base-sha",
			Sha:       "snapshot-sha",
			CreatedAt: timestamppb.New(time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)),
		},
		Threads: []*agentv1.ReviewThreadInContext{{
			Thread: &agentv1.ReviewThread{
				Id:         threadID.String(),
				TaskId:     betaTaskID.String(),
				SnapshotId: firstSnapshotID.String(),
				FilePath:   "greet.go",
				AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
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
				TaskID:      betaTaskID,
				FilePath:    "greet.go",
				Fingerprint: "abc123",
				ViewedAt:    reviewedAt,
			},
			wantViewed: &agentv1.ReviewViewedFile{
				TaskId:      betaTaskID.String(),
				FilePath:    "greet.go",
				Fingerprint: "abc123",
				ViewedAt:    timestamppb.New(time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newReviewHandler(&fakeReviewer{viewed: tt.viewed})

			got, err := svc.SetReviewFileViewed(t.Context(), connect.NewRequest(&agentv1.SetReviewFileViewedRequest{
				Task:     &agentv1.SetReviewFileViewedRequest_TaskId{TaskId: betaTaskID.String()},
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
func (f *fakeReviewer) AddReviewMessage(context.Context, reviews.AddReviewMessageParams) (*reviews.AddReviewMessageResult, error) {
	if f.err != nil {
		return nil, f.err
	}

	return &reviews.AddReviewMessageResult{Message: f.message}, nil
}

// AcceptForgeMessage answers the message it holds.
func (f *fakeReviewer) AcceptForgeMessage(context.Context, reviews.AcceptForgeMessageParams) (*reviewstore.Message, error) {
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

// TakeSnapshot answers the snapshot it holds.
func (f *fakeReviewer) TakeSnapshot(context.Context, oneofs.Task, reviews.ReviewAuthor) (*reviewstore.Snapshot, error) {
	if f.err != nil {
		return nil, f.err
	}

	return &f.snapshot, nil
}

// ListSnapshots answers the snapshot it holds.
func (f *fakeReviewer) ListSnapshots(context.Context, oneofs.Task, reviews.ReviewAuthor) ([]reviewstore.Snapshot, error) {
	if f.err != nil {
		return nil, f.err
	}

	return []reviewstore.Snapshot{f.snapshot}, nil
}

// GetHistory records the opening and answers the snapshot and history it holds.
func (f *fakeReviewer) GetHistory(_ context.Context, params reviews.GetHistoryParams) (*reviews.ReviewHistory, error) {
	f.opened = append(f.opened, params)
	if f.err != nil {
		return nil, f.err
	}

	return &reviews.ReviewHistory{
		Record:    f.record,
		Snapshot:  f.snapshot,
		Snapshots: []reviewstore.Snapshot{f.snapshot},
	}, nil
}

// ReadMail records the mailbox and answers the mail it holds unless err refuses it.
func (f *fakeReviewer) ReadMail(_ context.Context, agentID uuid.UUID) ([]mailstore.Mail, error) {
	f.read = append(f.read, agentID)
	if f.err != nil {
		return nil, f.err
	}

	return f.mail, nil
}

// ListReviews answers the review it holds.
func (f *fakeReviewer) ListReviews(context.Context, oneofs.Task, reviews.ReviewAuthor) ([]reviewstore.Review, error) {
	if f.err != nil {
		return nil, f.err
	}

	return []reviewstore.Review{f.review}, nil
}

// GetReview answers the review, snapshot and thread it holds, the thread drawn over one line of code.
func (f *fakeReviewer) GetReview(context.Context, uuid.UUID, reviews.ReviewAuthor) (*reviews.ReviewInFull, error) {
	if f.err != nil {
		return nil, f.err
	}

	return &reviews.ReviewInFull{
		Review:   f.review,
		Snapshot: f.snapshot,
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

var (
	messageID = uuid.New()
	reviewID  = uuid.New()
)
