package agents

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"weave/config"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	reviewstore "weave/internal/store/review"
	"weave/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	reviewedAgentID = "11111111-1111-1111-1111-111111111111"
	otherAgentID    = "22222222-2222-2222-2222-222222222222"
	humanMessageID  = "33333333-3333-3333-3333-333333333333"
	agentMessageID  = "44444444-4444-4444-4444-444444444444"
	reviewThreadID  = "55555555-5555-5555-5555-555555555555"
	draftMessageID  = "66666666-6666-6666-6666-666666666666"
)

var settledAt = time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)

var writtenAt = time.Date(2026, time.August, 27, 11, 0, 0, 0, time.UTC)

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

	threads, err := service.ListReviewThreads(t.Context(), ListReviewThreadsParams{
		AgentID: reviewedAgentID,
		Author:  agentAuthor(otherAgentID),
	})
	require.NoError(t, err)
	assert.Len(t, threads, 1)
}

// TestAnAgentReadsOnlyWhatTheReviewerLetGoOf covers the hole where every comment the person
// had written and not released was handed over the moment an agent made the call the doorbell
// asks for, so a review could be read as it was typed rather than once it was a review.
func TestAnAgentReadsOnlyWhatTheReviewerLetGoOf(t *testing.T) {
	tests := []struct {
		name       string
		reader     ReviewAuthor
		wantBodies []string
	}{
		{
			name:   "the person reviewing reads their own draft back",
			reader: humanAuthor(),
			wantBodies: []string{
				"this needs a nil check",
				"done",
				"and I have not decided about this one",
			},
		},
		{
			name:       "the agent under review reads only the released comment and its own reply",
			reader:     agentAuthor(reviewedAgentID),
			wantBodies: []string{"this needs a nil check", "done"},
		},
		{
			name:       "an agent reading somebody else's review is filtered the same way",
			reader:     agentAuthor(otherAgentID),
			wantBodies: []string{"this needs a nil check", "done"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reviews := newFakeReviewStore()
			reviews.messages[draftMessageID] = draftMessage()
			service := reviewService(t, reviews, &fakeNoticeSender{})

			threads, err := service.ListReviewThreads(t.Context(), ListReviewThreadsParams{
				AgentID: reviewedAgentID,
				Author:  tt.reader,
			})
			require.NoError(t, err)
			require.Len(t, threads, 1)

			bodies := xslice.Map(threads[0].Messages, func(m reviewstore.Message) string { return m.Body })
			assert.Equal(t, tt.wantBodies, bodies)

			if tt.reader.IsHuman() {
				assert.True(t, threads[0].Thread.UnreadByHuman, "the panel still draws its own unread mark")
				assert.Equal(t, &settledAt, threads[0].Messages[0].SentAt)
				assert.Equal(t, reviewstore.DeliverySent, threads[0].Messages[0].Delivery)

				return
			}

			for _, message := range threads[0].Messages {
				assert.Nil(t, message.SentAt, "when the person let a comment go is theirs to know")
				assert.Nil(t, message.DeliveredAt)
				assert.Equal(t, reviewstore.DeliveryNone, message.Delivery)
			}
			assert.False(t, threads[0].Thread.UnreadByHuman,
				"whether the person has read the reply is their own progress")
		})
	}
}

// TestAThreadWithNothingReleasedIsHiddenFromTheAgent pins that where the person is looking is
// theirs until they let a comment go, since the file, the lines and the diff they quoted say
// what is being written about even with none of the words read.
func TestAThreadWithNothingReleasedIsHiddenFromTheAgent(t *testing.T) {
	reviews := newFakeReviewStore()
	reviews.messages = map[string]reviewstore.Message{draftMessageID: draftMessage()}
	service := reviewService(t, reviews, &fakeNoticeSender{})

	forHuman, err := service.ListReviewThreads(t.Context(), ListReviewThreadsParams{
		AgentID: reviewedAgentID,
		Author:  humanAuthor(),
	})
	require.NoError(t, err)
	require.Len(t, forHuman, 1)
	assert.Equal(t, "greet.go", forHuman[0].Thread.FilePath)

	forAgent, err := service.ListReviewThreads(t.Context(), ListReviewThreadsParams{
		AgentID: reviewedAgentID,
		Author:  agentAuthor(reviewedAgentID),
	})
	require.NoError(t, err)
	assert.Empty(t, forAgent)
}

// TestReleaseReviewMessages pins what letting go of a review does: one doorbell however many
// comments the person has written and not sent, and nothing at all when there is nothing left
// to send.
func TestReleaseReviewMessages(t *testing.T) {
	tests := []struct {
		name         string
		unsent       int
		wantReleased int
		wantSent     int
	}{
		{
			name:         "a review with nothing unsent rings nobody",
			unsent:       0,
			wantReleased: 0,
			wantSent:     0,
		},
		{
			name:         "one comment rings once",
			unsent:       1,
			wantReleased: 1,
			wantSent:     1,
		},
		{
			name:         "three comments still ring once",
			unsent:       3,
			wantReleased: 3,
			wantSent:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reviews := newFakeReviewStore()
			reviews.unsent = unsentMessages(tt.unsent)

			notices := &fakeNoticeSender{answer: &mailstore.Mail{ID: "mail-1"}}
			service := reviewService(t, reviews, notices)

			result, err := service.ReleaseReviewMessages(t.Context(), ReleaseReviewMessagesParams{
				AgentID: reviewedAgentID, Author: humanAuthor(),
			})
			require.NoError(t, err)
			require.NotNil(t, result)

			assert.Equal(t, tt.wantReleased, result.Released)
			require.Len(t, notices.sent, tt.wantSent)

			if tt.wantSent == 0 {
				return
			}

			assert.Equal(t, "mail-1", reviews.doorbell, "the comments are stamped against the doorbell that rang")

			sent := notices.sent[0]
			assert.Equal(t, mailstore.KindReviewActivity, sent.Kind)
			assert.NotContains(t, sent.Body, "needs a nil check", "a doorbell carries no feedback")
			assert.Contains(t, sent.Body, "list_review_threads")
		})
	}
}

// TestNothingIsStampedWhenTheDoorbellCannotBeRaised pins the order a release runs in, since a
// comment stamped as sent with nothing raised for it reads in the panel as one the agent let
// run out of time rather than one it was never told about.
func TestNothingIsStampedWhenTheDoorbellCannotBeRaised(t *testing.T) {
	reviews := newFakeReviewStore()
	reviews.unsent = unsentMessages(3)

	notices := &fakeNoticeSender{err: ErrMailboxFull}
	service := reviewService(t, reviews, notices)

	_, err := service.ReleaseReviewMessages(t.Context(), ReleaseReviewMessagesParams{
		AgentID: reviewedAgentID, Author: humanAuthor(),
	})
	require.ErrorIs(t, err, ErrMailboxFull)

	assert.Empty(t, reviews.released, "a review that could not be rung is still the person's to send")
	assert.Len(t, reviews.unsent, 3)
}

// TestRingReviewMessagesAgain pins that ringing again raises one fresh doorbell and moves every
// comment beyond reading onto it, and that a review nothing has expired on rings nobody.
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

	assert.Equal(t, 2, result.Rung)
	assert.Len(t, notices.sent, 1, "both comments are heard through the one doorbell")
	assert.Equal(t, "mail-1", reviews.doorbell)

	reviews.expired = nil
	notices.sent = nil

	quiet, err := service.RingReviewMessagesAgain(t.Context(), RingReviewMessagesAgainParams{
		AgentID: reviewedAgentID, Author: humanAuthor(),
	})
	require.NoError(t, err)
	require.NotNil(t, quiet)

	assert.Zero(t, quiet.Rung)
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
			ID:         fmt.Sprintf("66666666-6666-6666-6666-%012d", i),
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
	doorbell string
}

func newFakeReviewStore() *fakeReviewStore {
	agentID := reviewedAgentID

	return &fakeReviewStore{
		thread: reviewstore.Thread{
			ID:            reviewThreadID,
			AgentID:       reviewedAgentID,
			FilePath:      "greet.go",
			QuotedLines:   []string{"+\tgreet(nil)"},
			UnreadByHuman: true,
		},
		messages: map[string]reviewstore.Message{
			humanMessageID: {
				ID:         humanMessageID,
				ThreadID:   reviewThreadID,
				AuthorKind: reviewstore.AuthorHuman,
				Body:       "this needs a nil check",
				SentAt:     &settledAt,
				Delivery:   reviewstore.DeliverySent,
				CreatedAt:  writtenAt,
			},
			agentMessageID: {
				ID:            agentMessageID,
				ThreadID:      reviewThreadID,
				AuthorKind:    reviewstore.AuthorAgent,
				AuthorAgentID: &agentID,
				Body:          "done",
				CreatedAt:     writtenAt.Add(time.Minute),
			},
		},
	}
}

// draftMessage is a comment the person has written down and not let go of, which is the state
// every comment passes through while a review is being composed.
func draftMessage() reviewstore.Message {
	return reviewstore.Message{
		ID:         draftMessageID,
		ThreadID:   reviewThreadID,
		AuthorKind: reviewstore.AuthorHuman,
		Body:       "and I have not decided about this one",
		Delivery:   reviewstore.DeliveryUnsent,
		CreatedAt:  writtenAt.Add(2 * time.Minute),
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

func (f *fakeReviewStore) CountUnsentMessages(context.Context, string) (int, error) {
	return len(f.unsent), nil
}

func (f *fakeReviewStore) ReleaseUnsentMessages(_ context.Context, _, doorbellMailID string) ([]reviewstore.Message, error) {
	f.released = f.unsent
	f.unsent = nil
	f.doorbell = doorbellMailID

	return f.released, nil
}

func (f *fakeReviewStore) PointMessagesAtDoorbell(_ context.Context, ids []string, doorbellMailID string) ([]reviewstore.Message, error) {
	f.doorbell = doorbellMailID

	pointed := make([]reviewstore.Message, 0, len(ids))
	for _, id := range ids {
		pointed = append(pointed, reviewstore.Message{
			ID:         id,
			ThreadID:   reviewThreadID,
			AuthorKind: reviewstore.AuthorHuman,
		})
	}

	return pointed, nil
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

func (f *fakeReviewStore) ListMessages(_ context.Context, threadIDs []string) ([]reviewstore.Message, error) {
	wanted := map[string]bool{}
	for _, id := range threadIDs {
		wanted[id] = true
	}

	messages := make([]reviewstore.Message, 0, len(f.messages))
	for _, message := range f.messages {
		if wanted[message.ThreadID] {
			messages = append(messages, message)
		}
	}
	slices.SortFunc(messages, func(a, b reviewstore.Message) int {
		return a.CreatedAt.Compare(b.CreatedAt)
	})

	return messages, nil
}

func (f *fakeReviewStore) DeleteMessage(context.Context, string) error { return nil }

type fakeNoticeSender struct {
	sent   []SendNoticeParams
	answer *mailstore.Mail
	err    error
}

func (f *fakeNoticeSender) SendNotice(_ context.Context, params SendNoticeParams) (*mailstore.Mail, error) {
	f.sent = append(f.sent, params)

	if f.err != nil {
		return nil, f.err
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

// TestReleasingAReviewCostsTheMailboxOneDoorbell pins the mailbox price of letting go of a
// review, since every doorbell says the same thing and a review of any size that spends the
// whole mailbox on them leaves no room for the mail other agents are waiting to have read.
func TestReleasingAReviewCostsTheMailboxOneDoorbell(t *testing.T) {
	for _, comments := range []int{1, 21, 140} {
		t.Run(fmt.Sprintf("%d comments", comments), func(t *testing.T) {
			reviews := newFakeReviewStore()
			reviews.unsent = unsentMessages(comments)

			mailbox := &countingMailbox{}
			service := reviewServiceWithRealMail(t, reviews, mailbox)

			result, err := service.ReleaseReviewMessages(t.Context(), ReleaseReviewMessagesParams{
				AgentID: reviewedAgentID, Author: humanAuthor(),
			})
			require.NoError(t, err, "a review is let go of whole however many comments it holds")
			require.NotNil(t, result)

			assert.Equal(t, comments, result.Released)
			assert.Equal(t, 1, mailbox.pendingCount(), "one doorbell says what any number of them say")
		})
	}
}

// TestAReleasedReviewLeavesRoomForPeerMail pins that a release does not crowd out the mail
// other agents send, since read_mail hands over a bounded batch and doorbells filling it
// would bury an answer an agent is waiting on.
func TestAReleasedReviewLeavesRoomForPeerMail(t *testing.T) {
	reviews := newFakeReviewStore()
	reviews.unsent = unsentMessages(30)

	mailbox := &countingMailbox{}
	service := reviewServiceWithRealMail(t, reviews, mailbox)

	_, err := service.ReleaseReviewMessages(t.Context(), ReleaseReviewMessagesParams{
		AgentID: reviewedAgentID, Author: humanAuthor(),
	})
	require.NoError(t, err)

	mail := mailService(mailbox)
	_, err = mail.SendMail(t.Context(), SendMailParams{
		From: otherAgentID, ToAgentID: reviewedAgentID, Body: "here is the answer you asked for",
	})
	require.NoError(t, err, "a peer can still reach an agent whose review was just released")

	claimed, err := mail.ClaimMail(t.Context(), reviewedAgentID)
	require.NoError(t, err)

	bodies := make([]string, 0, len(claimed))
	for _, held := range claimed {
		bodies = append(bodies, held.Body)
	}

	assert.Contains(t, bodies, "here is the answer you asked for", "the peer's mail is in the batch the agent reads")
}

func reviewServiceWithRealMail(t *testing.T, reviews *fakeReviewStore, mailbox *countingMailbox) *ReviewService {
	t.Helper()

	return NewReviewService(ReviewServiceParams{
		Threads:  reviews,
		Messages: reviews,
		Viewed:   reviews,
		Agents:   fakeAgentReaderFunc(liveAgent),
		Mailer:   mailService(mailbox),
	})
}

func mailService(mailbox *countingMailbox) *MailService {
	return NewMailService(NewMailServiceParams{
		MailReadWriter: mailbox,
		AgentFinder:    fakeAgentReaderFunc(liveAgent),
		Config:         fakeMailConfig{maxHops: config.DefaultMaxHops},
	})
}

// countingMailbox counts what is waiting the way the real store does, off the mail it holds
// that nobody has claimed, so the ceiling a send is refused by is the real one.
type countingMailbox struct {
	held []mailstore.Mail
}

func (f *countingMailbox) Send(_ context.Context, params mailstore.SendParams) (*mailstore.Mail, error) {
	sent := mailstore.Mail{
		ID:          fmt.Sprintf("mail-%d", len(f.held)+1),
		FromAgentID: params.FromAgentID,
		FromName:    params.FromName,
		ToAgentID:   params.ToAgentID,
		Body:        params.Body,
		Kind:        params.Kind,
		ReplyTo:     params.ReplyTo,
		Hops:        params.Hops,
	}
	f.held = append(f.held, sent)

	return &sent, nil
}

func (f *countingMailbox) pendingCount() int {
	pending := 0
	for _, held := range f.held {
		if held.ClaimedAt == nil {
			pending++
		}
	}

	return pending
}

func (f *countingMailbox) GetByID(_ context.Context, id string) (*mailstore.Mail, error) {
	for _, held := range f.held {
		if held.ID == id {
			return &held, nil
		}
	}

	return nil, fmt.Errorf("%w: mail %q", store.ErrNotFound, id)
}

func (f *countingMailbox) Claim(_ context.Context, toAgentID string, limit int32) ([]mailstore.Mail, error) {
	var claimed []mailstore.Mail
	for i := range f.held {
		if len(claimed) == int(limit) {
			break
		}
		if f.held[i].ToAgentID != toAgentID || f.held[i].ClaimedAt != nil {
			continue
		}

		f.held[i].ClaimedAt = &settledAt
		claimed = append(claimed, f.held[i])
	}

	return claimed, nil
}

func (f *countingMailbox) GetPendingOfKind(_ context.Context, toAgentID string, kind mailstore.Kind) (*mailstore.Mail, error) {
	for i := range f.held {
		if f.held[i].ToAgentID == toAgentID && f.held[i].Kind == kind && f.held[i].ClaimedAt == nil {
			return &f.held[i], nil
		}
	}

	return nil, fmt.Errorf("%w: an unread %q notice", store.ErrNotFound, kind)
}

func (f *countingMailbox) CountPending(_ context.Context, toAgentID string) (int64, error) {
	pending := int64(0)
	for _, held := range f.held {
		if held.ToAgentID == toAgentID && held.ClaimedAt == nil {
			pending++
		}
	}

	return pending, nil
}

func (f *countingMailbox) ListPage(context.Context, string, int32, *mailstore.Cursor) ([]mailstore.Mail, error) {
	return f.held, nil
}

func (f *countingMailbox) ListByID(context.Context, []string) ([]mailstore.Mail, error) {
	return f.held, nil
}

func (f *countingMailbox) Delete(context.Context, []string) error { return nil }

func (f *countingMailbox) ListUnreadMailboxes(context.Context) ([]string, error) { return nil, nil }
