package agents

import (
	"context"
	"fmt"
	"testing"
	"time"

	"weave/internal/store"
	agentstore "weave/internal/store/agent"
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

// TestReviewOperationsTheAgentDoesNotGet pins which review calls are the reviewer's alone, so
// an agent cannot open or resolve a thread on its own work.
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
			name: "an agent cannot release a review to itself",
			call: func(ctx context.Context, service *ReviewService, author ReviewAuthor) error {
				_, err := service.ReleaseReviewMessages(ctx, ReleaseReviewMessagesParams{
					AgentID: reviewedAgentID, Author: author,
				})

				return err
			},
		},
		{
			name: "an agent cannot ring its own review again",
			call: func(ctx context.Context, service *ReviewService, author ReviewAuthor) error {
				_, err := service.RingReviewMessagesAgain(ctx, RingReviewMessagesAgainParams{
					AgentID: reviewedAgentID, Author: author,
				})

				return err
			},
		},
		{
			name: "an agent cannot claim a review thread as read for the person",
			call: func(ctx context.Context, service *ReviewService, author ReviewAuthor) error {
				_, err := service.MarkReviewThreadRead(ctx, MarkReviewThreadReadParams{
					ID: reviewThreadID, Author: author,
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

// TestOnlyTheAuthorRewritesAMessage pins that a message can be rewritten only by whoever
// wrote it.
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

// TestReadingAnotherAgentsReviewIsAllowed pins that reading a review changes nothing, so any
// agent may read any review and none of it is claimed by the looking.
func TestReadingAnotherAgentsReviewIsAllowed(t *testing.T) {
	service := reviewService(t, newFakeReviewStore(), &fakeNoticeSender{})

	threads, err := service.ListReviewThreads(t.Context(), reviewedAgentID)
	require.NoError(t, err)
	assert.Len(t, threads, 1)
}

// TestReleaseReviewMessages pins what letting go of a review does: one doorbell per comment the
// person has written and not sent, and nothing at all when there is nothing left to send.
func TestReleaseReviewMessages(t *testing.T) {
	tests := []struct {
		name           string
		unsent         int
		alreadyWaiting bool
		wantReleased   int
		wantRung       int
		wantSent       int
	}{
		{
			name:         "a review with nothing unsent rings nobody",
			unsent:       0,
			wantReleased: 0,
			wantRung:     0,
			wantSent:     0,
		},
		{
			name:         "every unsent comment gets a doorbell of its own",
			unsent:       3,
			wantReleased: 3,
			wantRung:     3,
			wantSent:     3,
		},
		{
			name:           "a comment already holding a doorbell is not given a second",
			unsent:         2,
			alreadyWaiting: true,
			wantReleased:   2,
			wantRung:       0,
			wantSent:       2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			written := unsentMessages(tt.unsent)

			reviews := newFakeReviewStore()
			reviews.unsent = written

			notices := &fakeNoticeSender{}
			if !tt.alreadyWaiting {
				notices.answer = &mailstore.Mail{ID: "mail-1"}
			}

			service := reviewService(t, reviews, notices)

			result, err := service.ReleaseReviewMessages(t.Context(), ReleaseReviewMessagesParams{
				AgentID: reviewedAgentID, Author: humanAuthor(),
			})
			require.NoError(t, err)
			require.NotNil(t, result)

			assert.Equal(t, tt.wantReleased, result.Released)
			assert.Equal(t, tt.wantRung, result.Rung)
			require.Len(t, notices.sent, tt.wantSent)

			for i, sent := range notices.sent {
				assert.Equal(t, mailstore.KindReviewActivity, sent.Kind)
				require.NotNil(t, sent.ReviewMessageID, "a doorbell names the comment it is for")
				assert.Equal(t, written[i].ID, *sent.ReviewMessageID)
				assert.NotContains(t, sent.Body, "needs a nil check", "a doorbell carries no feedback")
				assert.Contains(t, sent.Body, "list_review_threads")
			}
		})
	}
}

// TestRingReviewMessagesAgain pins that ringing again reaches only the comments whose doorbell
// the agent can no longer read, since one still waiting says what a second would.
func TestRingReviewMessagesAgain(t *testing.T) {
	reviews := newFakeReviewStore()
	reviews.expired = unsentMessages(2)

	notices := &fakeNoticeSender{answer: &mailstore.Mail{ID: "mail-1"}}
	service := reviewService(t, reviews, notices)

	result, err := service.RingReviewMessagesAgain(t.Context(), RingReviewMessagesAgainParams{
		AgentID: reviewedAgentID, Author: humanAuthor(),
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, 2, result.Expired)
	assert.Equal(t, 2, result.Rung)
	assert.Len(t, notices.sent, 2)

	reviews.expired = nil
	notices.sent = nil

	quiet, err := service.RingReviewMessagesAgain(t.Context(), RingReviewMessagesAgainParams{
		AgentID: reviewedAgentID, Author: humanAuthor(),
	})
	require.NoError(t, err)
	require.NotNil(t, quiet)

	assert.Zero(t, quiet.Expired)
	assert.Empty(t, notices.sent, "a review nothing has expired on rings nobody")
}

// TestReleasingToAnArchivedAgentIsRefused pins that a review is not stamped as sent to an agent
// that is not there to be told, since the stamp would say it had been.
func TestReleasingToAnArchivedAgentIsRefused(t *testing.T) {
	reviews := newFakeReviewStore()
	reviews.unsent = unsentMessages(1)

	notices := &fakeNoticeSender{answer: &mailstore.Mail{ID: "mail-1"}}
	service := reviewServiceForAgent(t, reviews, notices, archivedAgent)

	_, err := service.ReleaseReviewMessages(t.Context(), ReleaseReviewMessagesParams{
		AgentID: reviewedAgentID, Author: humanAuthor(),
	})
	require.ErrorIs(t, err, ErrAgentArchived)

	_, err = service.RingReviewMessagesAgain(t.Context(), RingReviewMessagesAgainParams{
		AgentID: reviewedAgentID, Author: humanAuthor(),
	})
	require.ErrorIs(t, err, ErrAgentArchived)

	assert.Empty(t, notices.sent, "nothing is rung")
	assert.Empty(t, reviews.released, "and nothing is stamped as sent")
}

func humanAuthor() ReviewAuthor {
	return ReviewAuthor{Kind: reviewstore.AuthorHuman}
}

func agentAuthor(id string) ReviewAuthor {
	return ReviewAuthor{Kind: reviewstore.AuthorAgent, AgentID: &id}
}

func reviewService(t *testing.T, reviews *fakeReviewStore, notices *fakeNoticeSender) *ReviewService {
	t.Helper()

	return reviewServiceForAgent(t, reviews, notices, liveAgent)
}

func reviewServiceForAgent(
	t *testing.T,
	reviews *fakeReviewStore,
	notices *fakeNoticeSender,
	find fakeAgentReaderFunc,
) *ReviewService {
	t.Helper()

	return NewReviewService(ReviewServiceParams{
		Threads:  reviews,
		Messages: reviews,
		Viewed:   reviews,
		Agents:   find,
		Mailer:   notices,
	})
}

func liveAgent(_ context.Context, id string) (*agentstore.Agent, error) {
	return &agentstore.Agent{ID: id, Name: "worker"}, nil
}

func archivedAgent(_ context.Context, id string) (*agentstore.Agent, error) {
	return &agentstore.Agent{
		ID:         id,
		Name:       "worker",
		ArchivedAt: &settledAt,
	}, nil
}

// unsentMessages is n comments of the person's, which is what the store hands back from a
// release and from a listing of what has expired.
func unsentMessages(n int) []reviewstore.Message {
	messages := make([]reviewstore.Message, 0, n)
	for i := range n {
		messages = append(messages, reviewstore.Message{
			ID:         fmt.Sprintf("66666666-6666-6666-6666-66666666666%d", i),
			ThreadID:   reviewThreadID,
			AuthorKind: reviewstore.AuthorHuman,
			Body:       "this needs a nil check",
		})
	}

	return messages
}

// TestViewedFilesAreTheReviewersOwn pins that which files have been read is a record of the
// person's own progress: they set it and read it, and the agent whose work it is cannot.
func TestViewedFilesAreTheReviewersOwn(t *testing.T) {
	reviews := newFakeReviewStore()
	service := reviewService(t, reviews, &fakeNoticeSender{})
	ctx := t.Context()

	_, err := service.SetReviewFileViewed(ctx, SetReviewFileViewedParams{
		AgentID:     reviewedAgentID,
		FilePath:    "greet.go",
		Fingerprint: "shape",
		Viewed:      true,
		Author:      humanAuthor(),
	})
	require.NoError(t, err)

	viewed, err := service.ListReviewViewedFiles(ctx, ListReviewViewedFilesParams{
		AgentID: reviewedAgentID,
		Author:  humanAuthor(),
	})
	require.NoError(t, err)
	require.Len(t, viewed, 1)
	assert.Equal(t, "greet.go", viewed[0].FilePath)

	_, err = service.SetReviewFileViewed(ctx, SetReviewFileViewedParams{
		AgentID:  reviewedAgentID,
		FilePath: "greet.go",
		Viewed:   true,
		Author:   agentAuthor(reviewedAgentID),
	})
	require.ErrorIs(t, err, ErrReviewHumanOnly)

	_, err = service.ListReviewViewedFiles(ctx, ListReviewViewedFilesParams{
		AgentID: reviewedAgentID,
		Author:  agentAuthor(reviewedAgentID),
	})
	require.ErrorIs(t, err, ErrReviewHumanOnly)

	_, err = service.SetReviewFileViewed(ctx, SetReviewFileViewedParams{
		AgentID:  reviewedAgentID,
		FilePath: "greet.go",
		Author:   humanAuthor(),
	})
	require.NoError(t, err)

	viewed, err = service.ListReviewViewedFiles(ctx, ListReviewViewedFilesParams{
		AgentID: reviewedAgentID,
		Author:  humanAuthor(),
	})
	require.NoError(t, err)
	assert.Empty(t, viewed)
}

type fakeReviewStore struct {
	thread   reviewstore.Thread
	messages map[string]reviewstore.Message
	viewed   map[string]string
	unsent   []reviewstore.Message
	expired  []reviewstore.Message
	released []reviewstore.Message
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

func (f *fakeReviewStore) ListThreads(context.Context, string) ([]reviewstore.Thread, error) {
	return []reviewstore.Thread{f.thread}, nil
}

func (f *fakeReviewStore) GetThread(_ context.Context, id string) (*reviewstore.Thread, error) {
	if id != f.thread.ID {
		return nil, fmt.Errorf("%w: review thread %q", store.ErrNotFound, id)
	}

	thread := f.thread

	return &thread, nil
}

func (f *fakeReviewStore) MarkThreadRead(_ context.Context, _ string) (*reviewstore.Thread, error) {
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

func (f *fakeReviewStore) ReleaseUnsentMessages(context.Context, string) ([]reviewstore.Message, error) {
	f.released = f.unsent
	f.unsent = nil

	return f.released, nil
}

func (f *fakeReviewStore) ListExpiredMessages(context.Context, string) ([]reviewstore.Message, error) {
	return f.expired, nil
}

func (f *fakeReviewStore) DeleteThread(context.Context, string) error { return nil }

func (f *fakeReviewStore) MarkFileViewed(
	_ context.Context,
	params reviewstore.MarkFileViewedParams,
) (*reviewstore.ViewedFile, error) {
	if f.viewed == nil {
		f.viewed = map[string]string{}
	}
	f.viewed[params.FilePath] = params.Fingerprint

	return &reviewstore.ViewedFile{
		AgentID:     params.AgentID,
		FilePath:    params.FilePath,
		Fingerprint: params.Fingerprint,
	}, nil
}

func (f *fakeReviewStore) UnmarkFileViewed(_ context.Context, _, filePath string) error {
	delete(f.viewed, filePath)

	return nil
}

func (f *fakeReviewStore) ListViewedFiles(_ context.Context, agentID string) ([]reviewstore.ViewedFile, error) {
	files := make([]reviewstore.ViewedFile, 0, len(f.viewed))
	for path, fingerprint := range f.viewed {
		files = append(files, reviewstore.ViewedFile{
			AgentID:     agentID,
			FilePath:    path,
			Fingerprint: fingerprint,
		})
	}

	return files, nil
}

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
	sent   []SendNoticeParams
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
