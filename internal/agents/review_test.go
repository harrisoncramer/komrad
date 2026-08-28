package agents

import (
	"context"
	"fmt"
	"testing"
	"time"

	"weave/internal/store"
	mailstore "weave/internal/store/mail"
	reviewstore "weave/internal/store/review"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	reviewedAgentID = "11111111-1111-1111-1111-111111111111"
	otherAgentID    = "22222222-2222-2222-2222-222222222222"
	humanMessageID  = "33333333-3333-3333-3333-333333333333"
	agentMessageID  = "44444444-4444-4444-4444-444444444444"
	reviewThreadID  = "55555555-5555-5555-5555-555555555555"
)

var settledAt = time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)

// TestOnlyTheTargetAgentWritesToAReview covers the hole where any agent could reply in or
// claim a conversation addressed to a different agent purely by naming its thread id.
func TestOnlyTheTargetAgentWritesToAReview(t *testing.T) {
	tests := []struct {
		name    string
		author  ReviewAuthor
		call    func(ctx context.Context, service *ReviewService, author ReviewAuthor) error
		wantErr error
	}{
		{
			name:   "another agent cannot reply in a review it is not the subject of",
			author: agentAuthor(otherAgentID),
			call: func(ctx context.Context, service *ReviewService, author ReviewAuthor) error {
				_, err := service.AddReviewMessage(ctx, AddReviewMessageParams{
					ThreadID: reviewThreadID, Body: "done", Author: author,
				})

				return err
			},
			wantErr: ErrReviewNotYours,
		},
		{
			name:   "another agent cannot claim a review it is not the subject of",
			author: agentAuthor(otherAgentID),
			call: func(ctx context.Context, service *ReviewService, author ReviewAuthor) error {
				_, err := service.MarkReviewThreadRead(ctx, MarkReviewThreadReadParams{
					ID: reviewThreadID, Author: author,
				})

				return err
			},
			wantErr: ErrReviewNotYours,
		},
		{
			name:   "the agent the review is about may reply in it",
			author: agentAuthor(reviewedAgentID),
			call: func(ctx context.Context, service *ReviewService, author ReviewAuthor) error {
				_, err := service.AddReviewMessage(ctx, AddReviewMessageParams{
					ThreadID: reviewThreadID, Body: "done", Author: author,
				})

				return err
			},
		},
		{
			name:   "the person reviewing may reply in it",
			author: humanAuthor(),
			call: func(ctx context.Context, service *ReviewService, author ReviewAuthor) error {
				_, err := service.AddReviewMessage(ctx, AddReviewMessageParams{
					ThreadID: reviewThreadID, Body: "and another thing", Author: author,
				})

				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := reviewService(t, newFakeReviewStore(), &fakeNoticeSender{})

			err := tt.call(t.Context(), service, tt.author)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			assert.NoError(t, err)
		})
	}
}

func TestReviewOperationsTheAgentDoesNotGet(t *testing.T) {
	tests := []struct {
		name string
		call func(ctx context.Context, service *ReviewService, author ReviewAuthor) error
	}{
		{
			name: "an agent cannot open a thread on its own work",
			call: func(ctx context.Context, service *ReviewService, author ReviewAuthor) error {
				_, err := service.CreateReviewThread(ctx, CreateReviewThreadParams{
					AgentID: reviewedAgentID, FilePath: "a.go", Body: "looks fine to me", Author: author,
				})

				return err
			},
		},
		{
			name: "an agent cannot delete the feedback on its own work",
			call: func(ctx context.Context, service *ReviewService, author ReviewAuthor) error {
				return service.DeleteReviewThread(ctx, DeleteReviewThreadParams{ID: reviewThreadID, Author: author})
			},
		},
		{
			name: "an agent cannot settle the feedback on its own work",
			call: func(ctx context.Context, service *ReviewService, author ReviewAuthor) error {
				_, err := service.SetReviewThreadResolved(ctx, SetReviewThreadResolvedParams{
					ID: reviewThreadID, Resolved: true, Author: author,
				})

				return err
			},
		},
		{
			name: "an agent cannot announce a review to itself",
			call: func(ctx context.Context, service *ReviewService, author ReviewAuthor) error {
				_, err := service.NotifyReviewThreads(ctx, NotifyReviewThreadsParams{
					AgentID: reviewedAgentID, Author: author,
				})

				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := reviewService(t, newFakeReviewStore(), &fakeNoticeSender{})

			err := tt.call(t.Context(), service, agentAuthor(reviewedAgentID))
			require.ErrorIs(t, err, ErrReviewHumanOnly)

			assert.NoError(t, tt.call(t.Context(), service, humanAuthor()))
		})
	}
}

func TestOnlyTheAuthorRewritesAMessage(t *testing.T) {
	tests := []struct {
		name      string
		author    ReviewAuthor
		messageID string
		wantErr   error
	}{
		{
			name:      "an agent cannot rewrite what the person reviewing wrote",
			author:    agentAuthor(reviewedAgentID),
			messageID: humanMessageID,
			wantErr:   ErrReviewNotYours,
		},
		{
			name:      "the person reviewing cannot rewrite the agent's reply",
			author:    humanAuthor(),
			messageID: agentMessageID,
			wantErr:   ErrReviewNotYours,
		},
		{
			name:      "another agent cannot rewrite this agent's reply",
			author:    agentAuthor(otherAgentID),
			messageID: agentMessageID,
			wantErr:   ErrReviewNotYours,
		},
		{
			name:      "the agent rewrites its own reply",
			author:    agentAuthor(reviewedAgentID),
			messageID: agentMessageID,
		},
		{
			name:      "the person rewrites their own note",
			author:    humanAuthor(),
			messageID: humanMessageID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := reviewService(t, newFakeReviewStore(), &fakeNoticeSender{})

			_, err := service.EditReviewMessage(t.Context(), EditReviewMessageParams{
				ID: tt.messageID, Body: "rewritten", Author: tt.author,
			})
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			assert.NoError(t, err)
		})
	}
}

func TestClaimingAnotherAgentsReviewIsRefused(t *testing.T) {
	service := reviewService(t, newFakeReviewStore(), &fakeNoticeSender{})

	_, err := service.ListReviewThreads(t.Context(), ListReviewThreadsParams{
		AgentID: reviewedAgentID, MarkRead: true, Author: agentAuthor(otherAgentID),
	})
	require.ErrorIs(t, err, ErrReviewNotYours)

	_, err = service.ListReviewThreads(t.Context(), ListReviewThreadsParams{
		AgentID: reviewedAgentID, Author: agentAuthor(otherAgentID),
	})
	assert.NoError(t, err, "reading another agent's review without claiming it is allowed")
}

func TestNotifyReviewThreads(t *testing.T) {
	tests := []struct {
		name           string
		unread         int64
		alreadyWaiting bool
		wantUnread     int64
		wantWaiting    bool
		wantSent       int
	}{
		{
			name:       "an agent that is up to date is told nothing",
			unread:     0,
			wantUnread: 0,
			wantSent:   0,
		},
		{
			name:           "a doorbell still unread does not get a second copy",
			unread:         2,
			alreadyWaiting: true,
			wantUnread:     2,
			wantWaiting:    true,
			wantSent:       1,
		},
		{
			name:       "an agent with unread review is told once",
			unread:     2,
			wantUnread: 2,
			wantSent:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reviews := newFakeReviewStore()
			reviews.unread = tt.unread

			notices := &fakeNoticeSender{}
			if !tt.alreadyWaiting {
				notices.answer = &mailstore.Mail{ID: "mail-1"}
			}

			service := reviewService(t, reviews, notices)

			result, err := service.NotifyReviewThreads(t.Context(), NotifyReviewThreadsParams{
				AgentID: reviewedAgentID, Author: humanAuthor(),
			})
			require.NoError(t, err)
			require.NotNil(t, result)

			assert.Equal(t, tt.wantUnread, result.Unread)
			assert.Equal(t, tt.wantWaiting, result.AlreadyWaiting)
			assert.Len(t, notices.sent, tt.wantSent)

			if tt.wantSent > 0 {
				assert.Equal(t, mailstore.KindReviewActivity, notices.sent[0].Kind)
			}
		})
	}
}

func humanAuthor() ReviewAuthor {
	return ReviewAuthor{Kind: reviewstore.AuthorHuman}
}

func agentAuthor(id string) ReviewAuthor {
	return ReviewAuthor{Kind: reviewstore.AuthorAgent, AgentID: &id}
}

func reviewService(t *testing.T, reviews *fakeReviewStore, notices *fakeNoticeSender) *ReviewService {
	t.Helper()

	return NewReviewService(ReviewServiceParams{Threads: reviews, Messages: reviews, Mailer: notices})
}

type fakeReviewStore struct {
	thread   reviewstore.Thread
	messages map[string]reviewstore.Message

	unread int64
}

func newFakeReviewStore() *fakeReviewStore {
	agentID := reviewedAgentID

	return &fakeReviewStore{
		thread: reviewstore.Thread{ID: reviewThreadID, AgentID: reviewedAgentID},
		messages: map[string]reviewstore.Message{
			humanMessageID: {
				ID: humanMessageID, ThreadID: reviewThreadID, AuthorKind: reviewstore.AuthorHuman,
			},
			agentMessageID: {
				ID:            agentMessageID,
				ThreadID:      reviewThreadID,
				AuthorKind:    reviewstore.AuthorAgent,
				AuthorAgentID: &agentID,
			},
		},
	}
}

func (f *fakeReviewStore) CreateThread(
	context.Context,
	reviewstore.CreateThreadParams,
) (*reviewstore.Thread, error) {
	thread := f.thread

	return &thread, nil
}

func (f *fakeReviewStore) ListThreads(
	context.Context,
	reviewstore.ListThreadsParams,
) ([]reviewstore.Thread, error) {
	return []reviewstore.Thread{f.thread}, nil
}

func (f *fakeReviewStore) GetThread(_ context.Context, id string) (*reviewstore.Thread, error) {
	if id != f.thread.ID {
		return nil, fmt.Errorf("%w: review thread %q", store.ErrNotFound, id)
	}

	thread := f.thread

	return &thread, nil
}

func (f *fakeReviewStore) MarkThreadRead(
	_ context.Context,
	_ string,
	_ reviewstore.AuthorKind,
) (*reviewstore.Thread, error) {
	thread := f.thread

	return &thread, nil
}

func (f *fakeReviewStore) SetThreadResolved(
	_ context.Context,
	_ string,
	resolved bool,
) (*reviewstore.Thread, error) {
	f.thread.ResolvedAt = nil
	if resolved {
		f.thread.ResolvedAt = &settledAt
	}

	thread := f.thread

	return &thread, nil
}

func (f *fakeReviewStore) CountUnreadForAgent(context.Context, string) (int64, error) {
	return f.unread, nil
}

func (f *fakeReviewStore) DeleteThread(context.Context, string) error { return nil }

func (f *fakeReviewStore) AddMessage(
	_ context.Context,
	params reviewstore.AddMessageParams,
) (*reviewstore.Message, error) {
	return &reviewstore.Message{
		ThreadID:      params.ThreadID,
		AuthorKind:    params.AuthorKind,
		AuthorAgentID: params.AuthorAgentID,
		Body:          params.Body,
	}, nil
}

func (f *fakeReviewStore) EditMessage(_ context.Context, id, body string) (*reviewstore.Message, error) {
	message, ok := f.messages[id]
	if !ok {
		return nil, fmt.Errorf("%w: review message %q", store.ErrNotFound, id)
	}

	message.Body = body

	return &message, nil
}

func (f *fakeReviewStore) GetMessage(_ context.Context, id string) (*reviewstore.Message, error) {
	message, ok := f.messages[id]
	if !ok {
		return nil, fmt.Errorf("%w: review message %q", store.ErrNotFound, id)
	}

	return &message, nil
}

func (f *fakeReviewStore) ListMessages(context.Context, []string) ([]reviewstore.Message, error) {
	return nil, nil
}

func (f *fakeReviewStore) DeleteMessage(context.Context, string) error { return nil }

type fakeNoticeSender struct {
	sent []SendNoticeParams

	answer *mailstore.Mail
}

func (f *fakeNoticeSender) SendNotice(_ context.Context, params SendNoticeParams) (*mailstore.Mail, error) {
	f.sent = append(f.sent, params)

	if f.answer == nil {
		return nil, mailstore.ErrNoticePending
	}

	return f.answer, nil
}

// TestReviewThreadMustExist pins the lookup settling and deleting do first, since without it
// a bad id is a silent no-op.
func TestReviewThreadMustExist(t *testing.T) {
	service := reviewService(t, newFakeReviewStore(), &fakeNoticeSender{})
	human := ReviewAuthor{Kind: reviewstore.AuthorHuman}

	_, err := service.SetReviewThreadResolved(t.Context(), SetReviewThreadResolvedParams{
		ID: otherAgentID, Resolved: true, Author: human,
	})
	require.ErrorIs(t, err, ErrReviewThreadNotFound)

	err = service.DeleteReviewThread(t.Context(), DeleteReviewThreadParams{ID: otherAgentID, Author: human})
	assert.ErrorIs(t, err, ErrReviewThreadNotFound)
}
