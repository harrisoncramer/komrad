package agents

import (
	"context"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"weave/config"
	"weave/internal/git"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	reviewstore "weave/internal/store/review"
	workflowstore "weave/internal/store/workflow"
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
	reviewRoundID   = "77777777-7777-7777-7777-777777777777"
	reviewBatchID   = "88888888-8888-8888-8888-888888888888"
	reviewerAgentID = "99999999-9999-9999-9999-999999999999"
	openBatchID     = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
)

func ptr(s string) *string { return &s }

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
		want error
		call func(ctx context.Context, service *ReviewService, author ReviewAuthor) error
	}{
		{
			name: "an agent cannot open a thread on its own work",
			want: ErrReviewOwnWork,
			call: func(ctx context.Context, service *ReviewService, author ReviewAuthor) error {
				_, err := service.CreateReviewThread(ctx, CreateReviewThreadParams{
					AgentID: reviewedAgentID, FilePath: "a.go", Body: "looks fine to me", Author: author,
				})

				return err
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
			want: ErrReviewOwnWork,
			call: func(ctx context.Context, service *ReviewService, author ReviewAuthor) error {
				_, err := service.ReleaseReview(ctx, ReleaseReviewParams{
					AgentID: reviewedAgentID, Verdict: reviewstore.VerdictApproved, Author: author,
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
			reviews := newFakeReviewStore()
			reviews.comments = 1
			_, err := reviews.OpenReview(t.Context(), reviewstore.OpenReviewParams{
				AgentID: reviewedAgentID, RoundID: reviewRoundID, AuthorKind: reviewstore.AuthorHuman,
			})
			require.NoError(t, err)

			notices := &fakeNoticeSender{answer: &mailstore.Mail{ID: "mail-1"}}
			service := reviewService(t, reviews, notices)

			want := tt.want
			if want == nil {
				want = ErrReviewHumanOnly
			}

			err = tt.call(t.Context(), service, agentAuthor(reviewedAgentID))
			require.ErrorIs(t, err, want)

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

// TestReleasingAReviewWritesItOutAndRingsOnce pins the shape of a release, which is the whole
// batch written where the agent can read it and one piece of ordinary mail saying where that is,
// carrying no feedback of its own.
func TestReleasingAReviewWritesItOutAndRingsOnce(t *testing.T) {
	reviews := newFakeReviewStore()
	reviews.comments = 3
	_, err := reviews.OpenReview(t.Context(), reviewstore.OpenReviewParams{
		AgentID: reviewedAgentID, RoundID: reviewRoundID, AuthorKind: reviewstore.AuthorHuman,
	})
	require.NoError(t, err)

	notices := &fakeNoticeSender{answer: &mailstore.Mail{ID: "mail-1"}}
	service := reviewService(t, reviews, notices)

	result, err := service.ReleaseReview(t.Context(), ReleaseReviewParams{
		AgentID: reviewedAgentID,
		Verdict: reviewstore.VerdictChangesRequested,
		Author:  humanAuthor(),
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, 3, result.Comments)
	require.Len(t, notices.sent, 1, "however many comments, one piece of mail")
	assert.Equal(t, "mail-1", reviews.doorbell)

	sent := notices.sent[0]
	assert.Equal(t, mailstore.KindReviewActivity, sent.Kind)
	assert.NotContains(t, sent.Body, "needs a nil check", "the mail carries no feedback")
	assert.Contains(t, sent.Body, "changes requested")
	assert.Contains(t, sent.Body, result.Path, "the mail says where to read it")

	written, err := os.ReadFile(result.Path)
	require.NoError(t, err)
	assert.Contains(t, string(written), "Changes requested")
	assert.Contains(t, string(written), "round 1")
}

// TestReleasingAnEmptyReviewIsRefused pins that a batch with nothing in it rings nobody, since
// mail saying there is a review to read and a file with no comments in it wastes a turn.
func TestReleasingAnEmptyReviewIsRefused(t *testing.T) {
	reviews := newFakeReviewStore()
	notices := &fakeNoticeSender{answer: &mailstore.Mail{ID: "mail-1"}}
	service := reviewService(t, reviews, notices)

	_, err := service.ReleaseReview(t.Context(), ReleaseReviewParams{
		AgentID: reviewedAgentID,
		Verdict: reviewstore.VerdictCommented,
		Author:  humanAuthor(),
	})
	require.ErrorIs(t, err, ErrReviewEmpty)
	assert.Empty(t, notices.sent)
}

// TestApprovingWithNothingWrittenStillGoes pins that a verdict is worth sending on its own: an
// approval with no comments is the whole message, and it is what tells the agent it is done.
func TestApprovingWithNothingWrittenStillGoes(t *testing.T) {
	reviews := newFakeReviewStore()
	_, err := reviews.OpenReview(t.Context(), reviewstore.OpenReviewParams{
		AgentID: reviewedAgentID, RoundID: reviewRoundID, AuthorKind: reviewstore.AuthorHuman,
	})
	require.NoError(t, err)

	notices := &fakeNoticeSender{answer: &mailstore.Mail{ID: "mail-1"}}
	service := reviewService(t, reviews, notices)

	result, err := service.ReleaseReview(t.Context(), ReleaseReviewParams{
		AgentID: reviewedAgentID,
		Verdict: reviewstore.VerdictApproved,
		Author:  humanAuthor(),
	})
	require.NoError(t, err)
	require.Len(t, notices.sent, 1)
	assert.Contains(t, notices.sent[0].Body, "approved")

	written, err := os.ReadFile(result.Path)
	require.NoError(t, err)
	assert.Contains(t, string(written), "Approved")
}

// TestNothingIsReleasedWhenTheMailCannotBeSent pins the order a release runs in, since a batch
// stamped as released with nothing raised for it reads in the panel as one the agent has been
// told about.
func TestNothingIsReleasedWhenTheMailCannotBeSent(t *testing.T) {
	reviews := newFakeReviewStore()
	reviews.comments = 3
	_, err := reviews.OpenReview(t.Context(), reviewstore.OpenReviewParams{
		AgentID: reviewedAgentID, RoundID: reviewRoundID, AuthorKind: reviewstore.AuthorHuman,
	})
	require.NoError(t, err)

	notices := &fakeNoticeSender{err: ErrMailboxFull}
	service := reviewService(t, reviews, notices)

	_, err = service.ReleaseReview(t.Context(), ReleaseReviewParams{
		AgentID: reviewedAgentID,
		Verdict: reviewstore.VerdictChangesRequested,
		Author:  humanAuthor(),
	})
	require.ErrorIs(t, err, ErrMailboxFull)

	assert.Nil(t, reviews.released, "a review that could not be rung is still the author's to send")
	assert.NotNil(t, reviews.open)
}

// TestAChildMayReviewItsParentAndNothingElse pins the entitlement: reviewing is open to an
// agent, but only of the agent it is attached to, which is the rule that already decides who
// it may write to.
func TestAChildMayReviewItsParentAndNothingElse(t *testing.T) {
	reviews := newFakeReviewStore()
	service := reviewService(t, reviews, &fakeNoticeSender{})

	_, err := service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
		AgentID:  reviewedAgentID,
		FilePath: "greet.go",
		Body:     "this needs a nil check",
		Author:   agentAuthor(reviewerAgentID),
	})
	require.NoError(t, err, "a child may review its parent")

	_, err = service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
		AgentID:  otherAgentID,
		FilePath: "greet.go",
		Body:     "this needs a nil check",
		Author:   agentAuthor(reviewerAgentID),
	})
	require.ErrorIs(t, err, ErrReviewNotEntitled, "and nothing else")
}

// TestOpeningARoundTwiceOverAnUnmovedTreeIsTheSameRound pins that opening the diff does not walk
// the ordinal on, since a round the agent has not written anything into is the round that was
// already taken of it.
func TestOpeningARoundTwiceOverAnUnmovedTreeIsTheSameRound(t *testing.T) {
	reviews := newFakeReviewStore()
	service := reviewService(t, reviews, &fakeNoticeSender{})

	first, err := service.OpenRound(t.Context(), reviewedAgentID, humanAuthor())
	require.NoError(t, err)
	require.NotNil(t, first)

	second, err := service.OpenRound(t.Context(), reviewedAgentID, humanAuthor())
	require.NoError(t, err)
	require.NotNil(t, second)

	assert.Equal(t, first.ID, second.ID)
	assert.Equal(t, first.Ordinal, second.Ordinal)
}

// TestAReleasedReviewCarriesWhatTheReviewerWrote is the headline case, and it broke once: the
// file weave writes is the whole of what the agent is given, so a comment written by a reviewing
// agent has to appear in it exactly as the person's would.
func TestAReleasedReviewCarriesWhatTheReviewerWrote(t *testing.T) {
	reviews := newFakeReviewStore()
	reviews.comments = 1
	notices := &fakeNoticeSender{answer: &mailstore.Mail{ID: "mail-1"}}
	service := reviewService(t, reviews, notices)
	reviewer := agentAuthor(reviewerAgentID)

	opened, err := service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
		AgentID:     reviewedAgentID,
		FilePath:    "greet.go",
		NewStart:    41,
		NewEnd:      41,
		QuotedLines: []string{"+\tgreet(nil)"},
		Body:        "this needs a nil check",
		Author:      reviewer,
	})
	require.NoError(t, err)
	require.NotNil(t, opened)

	result, err := service.ReleaseReview(t.Context(), ReleaseReviewParams{
		AgentID: reviewedAgentID,
		Verdict: reviewstore.VerdictChangesRequested,
		Author:  reviewer,
	})
	require.NoError(t, err)

	written, err := os.ReadFile(result.Path)
	require.NoError(t, err)
	assert.Contains(t, string(written), "this needs a nil check",
		"a reviewing agent's own words have to reach the agent it reviewed")
	assert.Contains(t, string(written), "greet.go:41")
}

// TestAReleasedReviewCarriesTheCodeAroundEachComment pins what saves the agent hunting: the file
// as the round froze it, either side of the lines the comment was written about, so acting on a
// review is one read rather than a search through a file that has moved since.
func TestAReleasedReviewCarriesTheCodeAroundEachComment(t *testing.T) {
	source := make([]string, 0, 20)
	for line := 1; line <= 20; line++ {
		source = append(source, fmt.Sprintf("line %d of greet.go", line))
	}

	reviews := newFakeReviewStore()
	reviews.thread.FilePath = "greet.go"
	reviews.thread.NewStart, reviews.thread.NewEnd = 12, 13
	reviews.comments = 1

	notices := &fakeNoticeSender{answer: &mailstore.Mail{ID: "mail-1"}}
	service := reviewServiceHolding(t, reviews, notices, map[string][]string{"greet.go": source})

	_, err := reviews.OpenReview(t.Context(), reviewstore.OpenReviewParams{
		AgentID: reviewedAgentID, RoundID: reviewRoundID, AuthorKind: reviewstore.AuthorHuman,
	})
	require.NoError(t, err)

	result, err := service.ReleaseReview(t.Context(), ReleaseReviewParams{
		AgentID: reviewedAgentID,
		Verdict: reviewstore.VerdictChangesRequested,
		Author:  humanAuthor(),
	})
	require.NoError(t, err)

	written, err := os.ReadFile(result.Path)
	require.NoError(t, err)
	body := string(written)

	assert.Contains(t, body, ">> 12  line 12 of greet.go", "the commented lines are marked")
	assert.Contains(t, body, ">> 13  line 13 of greet.go")
	assert.Contains(t, body, "    6  line 6 of greet.go", "six lines of context above")
	assert.Contains(t, body, "   19  line 19 of greet.go", "and six below")
	assert.NotContains(t, body, "line 5 of greet.go", "and no more than that")
	assert.NotContains(t, body, "line 20 of greet.go")
}

// TestAReviewWithNoSourceFallsBackToTheQuotedLines pins that a comment on a file the round cannot
// hand back is still sent, since a comment is worth more than the code around it.
func TestAReviewWithNoSourceFallsBackToTheQuotedLines(t *testing.T) {
	reviews := newFakeReviewStore()
	reviews.thread.FilePath = "gone.go"
	reviews.comments = 1

	notices := &fakeNoticeSender{answer: &mailstore.Mail{ID: "mail-1"}}
	service := reviewServiceHolding(t, reviews, notices, nil)

	_, err := reviews.OpenReview(t.Context(), reviewstore.OpenReviewParams{
		AgentID: reviewedAgentID, RoundID: reviewRoundID, AuthorKind: reviewstore.AuthorHuman,
	})
	require.NoError(t, err)

	result, err := service.ReleaseReview(t.Context(), ReleaseReviewParams{
		AgentID: reviewedAgentID,
		Verdict: reviewstore.VerdictChangesRequested,
		Author:  humanAuthor(),
	})
	require.NoError(t, err)

	written, err := os.ReadFile(result.Path)
	require.NoError(t, err)
	assert.Contains(t, string(written), "+\tgreet(nil)", "the lines it was written against still go")
	assert.Contains(t, string(written), "this needs a nil check")
}

// TestAReleasedReviewNamesWhatItWasAbout is what keeping every review forever asks of the file:
// the rows naming it go when its agent does, so a reader coming to it long afterwards has only
// what is written in it to say whose work it was and who reviewed that work.
func TestAReleasedReviewNamesWhatItWasAbout(t *testing.T) {
	reviews := newFakeReviewStore()
	reviews.comments = 1
	notices := &fakeNoticeSender{answer: &mailstore.Mail{ID: "mail-1"}}
	service := reviewService(t, reviews, notices)
	reviewer := agentAuthor(reviewerAgentID)

	_, err := service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
		AgentID: reviewedAgentID, FilePath: "greet.go", Body: "this needs a nil check", Author: reviewer,
	})
	require.NoError(t, err)

	result, err := service.ReleaseReview(t.Context(), ReleaseReviewParams{
		AgentID: reviewedAgentID,
		Verdict: reviewstore.VerdictChangesRequested,
		Author:  reviewer,
	})
	require.NoError(t, err)

	written, err := os.ReadFile(result.Path)
	require.NoError(t, err)

	assert.Contains(t, string(written), "worker", "whose work it was")
	assert.Contains(t, string(written), reviewedAgentID, "and which agent that was")
	assert.Contains(t, string(written), "reviewer", "and who reviewed it")
}

// TestAReviewGivenByThePersonSaysSo keeps the person distinguishable from an agent in a file read
// after every row about it has gone.
func TestAReviewGivenByThePersonSaysSo(t *testing.T) {
	reviews := newFakeReviewStore()
	notices := &fakeNoticeSender{answer: &mailstore.Mail{ID: "mail-1"}}
	service := reviewService(t, reviews, notices)

	result, err := service.ReleaseReview(t.Context(), ReleaseReviewParams{
		AgentID: reviewedAgentID,
		Verdict: reviewstore.VerdictApproved,
		Author:  humanAuthor(),
	})
	require.NoError(t, err)

	written, err := os.ReadFile(result.Path)
	require.NoError(t, err)
	assert.Contains(t, string(written), "the person running weave")
}

// TestAnUnreleasedCommentIsHiddenFromTheAgentWhoeverWroteIt pins that a review is read once it is
// a review: a batch still being written is the author's alone, whether the person or another
// agent is writing it.
func TestAnUnreleasedCommentIsHiddenFromTheAgentWhoeverWroteIt(t *testing.T) {
	tests := []struct {
		name   string
		author ReviewAuthor
	}{
		{name: "written by the person", author: humanAuthor()},
		{name: "written by a reviewing agent", author: agentAuthor(reviewerAgentID)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reviews := newFakeReviewStore()
			reviews.thread = reviewstore.Thread{}
			reviews.messages = map[string]reviewstore.Message{}
			service := reviewService(t, reviews, &fakeNoticeSender{})

			_, err := service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
				AgentID:  reviewedAgentID,
				FilePath: "greet.go",
				Body:     "I have not decided about this yet",
				Author:   tt.author,
			})
			require.NoError(t, err)

			forAgent, err := service.ListReviewThreads(t.Context(), ListReviewThreadsParams{
				AgentID: reviewedAgentID,
				Author:  agentAuthor(reviewedAgentID),
			})
			require.NoError(t, err)
			assert.Empty(t, forAgent, "an unreleased batch is nobody's but its author's")

			forAuthor, err := service.ListReviewThreads(t.Context(), ListReviewThreadsParams{
				AgentID: reviewedAgentID,
				Author:  tt.author,
			})
			require.NoError(t, err)
			assert.Len(t, forAuthor, 1, "the author reads its own draft back")
		})
	}
}

// TestApprovingARoundNobodyCommentedOnGoes is the clean approval, which is the case the verdict
// matters most for since it is what gates opening a pull request, and it needs no batch to have
// been started first.
func TestApprovingARoundNobodyCommentedOnGoes(t *testing.T) {
	reviews := newFakeReviewStore()
	notices := &fakeNoticeSender{answer: &mailstore.Mail{ID: "mail-1"}}
	service := reviewService(t, reviews, notices)

	result, err := service.ReleaseReview(t.Context(), ReleaseReviewParams{
		AgentID: reviewedAgentID,
		Verdict: reviewstore.VerdictApproved,
		Author:  humanAuthor(),
	})
	require.NoError(t, err)
	require.Len(t, notices.sent, 1)

	written, err := os.ReadFile(result.Path)
	require.NoError(t, err)
	assert.Contains(t, string(written), "Approved")
}

// TestAReviewerGetsOneReviewPerRound is the bound on a reviewer child and its parent answering
// each other forever: a new round appears only when the parent writes something, so the loop is
// limited by real progress rather than by a count somebody has to tune.
func TestAReviewerGetsOneReviewPerRound(t *testing.T) {
	reviews := newFakeReviewStore()
	notices := &fakeNoticeSender{answer: &mailstore.Mail{ID: "mail-1"}}
	service := reviewService(t, reviews, notices)
	reviewer := agentAuthor(reviewerAgentID)

	_, err := service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
		AgentID: reviewedAgentID, FilePath: "greet.go", Body: "this needs a nil check", Author: reviewer,
	})
	require.NoError(t, err)

	_, err = service.ReleaseReview(t.Context(), ReleaseReviewParams{
		AgentID: reviewedAgentID, Verdict: reviewstore.VerdictChangesRequested, Author: reviewer,
	})
	require.NoError(t, err)

	_, err = service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
		AgentID: reviewedAgentID, FilePath: "greet.go", Body: "and another thing", Author: reviewer,
	})
	require.ErrorIs(t, err, ErrReviewAlreadyGiven, "the reviewer has had its say on this round")

	// The parent writing something is what opens the next round, and the reviewer may review that.
	_, err = reviews.CreateRound(t.Context(), reviewstore.CreateRoundParams{
		AgentID: reviewedAgentID, BaseSHA: "base-sha", SnapshotSHA: "moved-on",
	})
	require.NoError(t, err)

	_, err = service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
		AgentID: reviewedAgentID, FilePath: "greet.go", Body: "and another thing", Author: reviewer,
	})
	require.NoError(t, err)
}

// TestOnlyTheReviewedAgentAndItsReviewerReachARound pins who may look at a round at all, which is
// the agent whose work it is and the agent reviewing that work, and pins that being allowed to
// look is not the same as being allowed to review.
func TestOnlyTheReviewedAgentAndItsReviewerReachARound(t *testing.T) {
	reviews := newFakeReviewStore()
	service := reviewService(t, reviews, &fakeNoticeSender{})

	_, err := service.OpenRound(t.Context(), reviewedAgentID, agentAuthor(otherAgentID))
	require.ErrorIs(t, err, ErrReviewNotEntitled)

	_, err = service.ListReviews(t.Context(), reviewedAgentID, agentAuthor(otherAgentID))
	require.ErrorIs(t, err, ErrReviewNotEntitled)

	_, err = service.ListRounds(t.Context(), reviewedAgentID, agentAuthor(reviewerAgentID))
	require.NoError(t, err, "the reviewer may read the rounds it is reviewing")

	_, err = service.ListRounds(t.Context(), reviewedAgentID, agentAuthor(reviewedAgentID))
	require.NoError(t, err, "and the agent may read the rounds its own threads are anchored to")

	_, err = service.OpenRound(t.Context(), reviewedAgentID, agentAuthor(reviewedAgentID))
	require.NoError(t, err, "including taking one of its own work to diff")

	_, err = service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
		AgentID: reviewedAgentID, FilePath: "greet.go", Body: "looks fine to me", Author: agentAuthor(reviewedAgentID),
	})
	require.ErrorIs(t, err, ErrReviewOwnWork, "but reading its own work is not reviewing it")
}

// TestAnAgentIsNotToldWhereAnotherReviewWasWritten pins that the path on a review names a file on
// disk holding somebody else's words, so it is the person's own and nobody else's.
func TestAnAgentIsNotToldWhereAnotherReviewWasWritten(t *testing.T) {
	reviews := newFakeReviewStore()
	reviews.comments = 1
	notices := &fakeNoticeSender{answer: &mailstore.Mail{ID: "mail-1"}}
	service := reviewService(t, reviews, notices)

	_, err := reviews.OpenReview(t.Context(), reviewstore.OpenReviewParams{
		AgentID: reviewedAgentID, RoundID: reviewRoundID, AuthorKind: reviewstore.AuthorHuman,
	})
	require.NoError(t, err)

	_, err = service.ReleaseReview(t.Context(), ReleaseReviewParams{
		AgentID: reviewedAgentID,
		Verdict: reviewstore.VerdictChangesRequested,
		Author:  humanAuthor(),
	})
	require.NoError(t, err)

	forReviewer, err := service.ListReviews(t.Context(), reviewedAgentID, agentAuthor(reviewerAgentID))
	require.NoError(t, err)
	require.Len(t, forReviewer, 1)
	assert.Empty(t, forReviewer[0].Path)

	forHuman, err := service.ListReviews(t.Context(), reviewedAgentID, humanAuthor())
	require.NoError(t, err)
	require.Len(t, forHuman, 1)
	assert.NotEmpty(t, forHuman[0].Path)
}

// TestReleasingToAnArchivedAgentIsRefused pins that a review is not stamped as sent to an agent
// that is not there to be told, since the stamp would say it had been.
func TestReleasingToAnArchivedAgentIsRefused(t *testing.T) {
	reviews := newFakeReviewStore()
	reviews.comments = 1
	_, err := reviews.OpenReview(t.Context(), reviewstore.OpenReviewParams{
		AgentID: reviewedAgentID, RoundID: reviewRoundID, AuthorKind: reviewstore.AuthorHuman,
	})
	require.NoError(t, err)

	notices := &fakeNoticeSender{answer: &mailstore.Mail{ID: "mail-1"}}
	service := reviewServiceForAgent(t, reviews, notices, archivedAgent)

	_, err = service.ReleaseReview(t.Context(), ReleaseReviewParams{
		AgentID: reviewedAgentID, Verdict: reviewstore.VerdictChangesRequested, Author: humanAuthor(),
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

	return reviewServiceForAgent(t, reviews, notices, findLiveAgent)
}

// reviewServiceHolding is a service whose round holds the given files, which is what lets a
// released review quote the code around each comment.
func reviewServiceHolding(
	t *testing.T,
	reviews *fakeReviewStore,
	notices *fakeNoticeSender,
	files map[string][]string,
) *ReviewService {
	t.Helper()

	return NewReviewService(ReviewServiceParams{
		Threads:  reviews,
		Messages: reviews,
		Viewed:   reviews,
		Rounds:   reviews,
		Reviews:  reviews,
		Snapshots: &fakeSnapshotter{
			sha:   "snapshot-sha",
			base:  "base-sha",
			files: files,
		},
		Workflows: fakeWorkflowGetter{},
		Agents:    fakeAgentReaderFunc(findLiveAgent),
		Mailer:    notices,
		StateDir:  t.TempDir(),
	})
}

func reviewServiceForAgent(
	t *testing.T,
	reviews *fakeReviewStore,
	notices *fakeNoticeSender,
	find fakeAgentReaderFunc,
) *ReviewService {
	t.Helper()

	return NewReviewService(ReviewServiceParams{
		Threads:   reviews,
		Messages:  reviews,
		Viewed:    reviews,
		Rounds:    reviews,
		Reviews:   reviews,
		Snapshots: &fakeSnapshotter{sha: "snapshot-sha", base: "base-sha"},
		Workflows: fakeWorkflowGetter{},
		Agents:    find,
		Mailer:    notices,
		StateDir:  t.TempDir(),
	})
}

func findLiveAgent(_ context.Context, id string) (*agentstore.Agent, error) {
	if id == reviewerAgentID {
		parent := reviewedAgentID

		return &agentstore.Agent{
			ID:            id,
			Name:          "reviewer",
			Cwd:           "/tmp/tree",
			ParentAgentID: &parent,
		}, nil
	}

	return &agentstore.Agent{
		ID:   id,
		Name: "worker",
		Cwd:  "/tmp/tree",
	}, nil
}

func archivedAgent(_ context.Context, id string) (*agentstore.Agent, error) {
	return &agentstore.Agent{
		ID:         id,
		Name:       "worker",
		ArchivedAt: &settledAt,
	}, nil
}

// TestViewedFilesAreTheReviewersOwn pins that which files have been read is a record of the
// person's own progress: they set it and read it, and the agent whose work it is cannot.
func TestViewedFilesAreTheReviewersOwn(t *testing.T) {
	reviews := newFakeReviewStore()
	service := reviewService(t, reviews, &fakeNoticeSender{})
	ctx := t.Context()

	_, err := service.SetReviewFileViewed(ctx, SetReviewFileViewedParams{
		RoundID:  reviewRoundID,
		FilePath: "greet.go",
		Viewed:   true,
		Author:   humanAuthor(),
	})
	require.NoError(t, err)

	viewed, err := service.ListReviewViewedFiles(ctx, ListReviewViewedFilesParams{
		RoundID: reviewRoundID,
		Author:  humanAuthor(),
	})
	require.NoError(t, err)
	require.Len(t, viewed, 1)
	assert.Equal(t, "greet.go", viewed[0].FilePath)

	_, err = service.SetReviewFileViewed(ctx, SetReviewFileViewedParams{
		RoundID:  reviewRoundID,
		FilePath: "greet.go",
		Viewed:   true,
		Author:   agentAuthor(reviewedAgentID),
	})
	require.ErrorIs(t, err, ErrReviewHumanOnly)

	_, err = service.ListReviewViewedFiles(ctx, ListReviewViewedFilesParams{
		RoundID: reviewRoundID,
		Author:  agentAuthor(reviewedAgentID),
	})
	require.ErrorIs(t, err, ErrReviewHumanOnly)

	_, err = service.SetReviewFileViewed(ctx, SetReviewFileViewedParams{
		RoundID:  reviewRoundID,
		FilePath: "greet.go",
		Author:   humanAuthor(),
	})
	require.NoError(t, err)

	viewed, err = service.ListReviewViewedFiles(ctx, ListReviewViewedFilesParams{
		RoundID: reviewRoundID,
		Author:  humanAuthor(),
	})
	require.NoError(t, err)
	assert.Empty(t, viewed)
}

type fakeReviewStore struct {
	thread   reviewstore.Thread
	threads  []reviewstore.Thread
	messages map[string]reviewstore.Message
	viewed   map[string]bool
	rounds   []reviewstore.Round
	reviews  map[string]reviewstore.Review
	open     *reviewstore.Review
	comments int
	released *reviewstore.Review
	doorbell string
}

func newFakeReviewStore() *fakeReviewStore {
	agentID := reviewedAgentID

	round := reviewstore.Round{
		ID:          reviewRoundID,
		AgentID:     reviewedAgentID,
		Ordinal:     1,
		BaseSHA:     "base-sha",
		SnapshotSHA: "snapshot-sha",
	}

	return &fakeReviewStore{
		rounds:  []reviewstore.Round{round},
		reviews: map[string]reviewstore.Review{},
		thread: reviewstore.Thread{
			ID:               reviewThreadID,
			AgentID:          reviewedAgentID,
			ReviewID:         reviewBatchID,
			RoundID:          reviewRoundID,
			RoundOrdinal:     1,
			ReviewReleasedAt: &settledAt,
			FilePath:         "greet.go",
			QuotedLines:      []string{"+\tgreet(nil)"},
			UnreadByHuman:    true,
		},
		messages: map[string]reviewstore.Message{
			humanMessageID: {
				ID:         humanMessageID,
				ThreadID:   reviewThreadID,
				ReviewID:   ptr(reviewBatchID),
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
		ReviewID:   ptr(openBatchID),
		AuthorKind: reviewstore.AuthorHuman,
		Body:       "and I have not decided about this one",
		Delivery:   reviewstore.DeliveryUnsent,
		CreatedAt:  writtenAt.Add(2 * time.Minute),
	}
}

// CreateThread records what it was asked for rather than answering with the canned thread, since
// which batch a thread belongs to is the whole of what several of these tests are about.
func (f *fakeReviewStore) CreateThread(
	_ context.Context,
	params reviewstore.CreateThreadParams,
) (*reviewstore.Thread, error) {
	thread := reviewstore.Thread{
		ID:          fmt.Sprintf("thread-%d", len(f.threads)+1),
		AgentID:     params.AgentID,
		ReviewID:    params.ReviewID,
		RoundID:     roundOr(params.RoundID),
		Harvested:   params.Harvested,
		FilePath:    params.FilePath,
		OldStart:    params.OldStart,
		OldEnd:      params.OldEnd,
		NewStart:    params.NewStart,
		NewEnd:      params.NewEnd,
		QuotedLines: params.QuotedLines,
	}

	if review, ok := f.reviews[params.ReviewID]; ok {
		thread.ReviewAuthorAgentID = review.AuthorAgentID
		thread.ReviewReleasedAt = review.ReleasedAt
	}

	f.threads = append(f.threads, thread)
	f.record(reviewstore.Message{
		ID:            fmt.Sprintf("message-%d", len(f.messages)+1),
		ThreadID:      thread.ID,
		ReviewID:      &params.ReviewID,
		AuthorKind:    params.AuthorKind,
		AuthorAgentID: params.AuthorAgentID,
		Body:          params.Body,
		Suggestion:    params.Suggestion,
	})
	f.comments++

	return &thread, nil
}

// roundOr is the round a thread names, falling back to the fixture's own round for the tests
// written before a thread carried one.
func roundOr(roundID string) string {
	if roundID == "" {
		return reviewRoundID
	}

	return roundID
}

func (f *fakeReviewStore) ListHarvested(
	_ context.Context,
	agentID, roundID string,
) ([]reviewstore.HarvestedThread, error) {
	var harvested []reviewstore.HarvestedThread

	for _, thread := range f.threads {
		if thread.AgentID != agentID || thread.RoundID != roundID || !thread.Harvested {
			continue
		}

		if thread.ReviewReleasedAt != nil {
			continue
		}

		var count int64
		for _, message := range f.messages {
			if message.ThreadID == thread.ID {
				count++
			}
		}

		harvested = append(harvested, reviewstore.HarvestedThread{Thread: thread, MessageCount: count})
	}

	return harvested, nil
}

func (f *fakeReviewStore) MoveThread(
	_ context.Context,
	params reviewstore.MoveThreadParams,
) (*reviewstore.Thread, error) {
	for i, thread := range f.threads {
		if thread.ID != params.ID {
			continue
		}

		f.threads[i].OldStart = params.OldStart
		f.threads[i].OldEnd = params.OldEnd
		f.threads[i].NewStart = params.NewStart
		f.threads[i].NewEnd = params.NewEnd
		f.threads[i].QuotedLines = params.QuotedLines

		return &f.threads[i], nil
	}

	return nil, fmt.Errorf("no thread %q", params.ID)
}

func (f *fakeReviewStore) DeleteThread(_ context.Context, id string) error {
	for i, thread := range f.threads {
		if thread.ID != id {
			continue
		}

		f.threads = append(f.threads[:i], f.threads[i+1:]...)

		for messageID, message := range f.messages {
			if message.ThreadID == id {
				delete(f.messages, messageID)
			}
		}

		return nil
	}

	return nil
}

// record files a message and stamps it with the state of the batch it belongs to, which is what
// the store does with a join and the fake has to do by hand.
func (f *fakeReviewStore) record(message reviewstore.Message) {
	if message.ReviewID != nil {
		if review, ok := f.reviews[*message.ReviewID]; ok {
			message.SentAt = review.ReleasedAt
		}
	}

	if f.messages == nil {
		f.messages = map[string]reviewstore.Message{}
	}
	f.messages[message.ID] = message
}

func (f *fakeReviewStore) ListThreads(context.Context, string) ([]reviewstore.Thread, error) {
	if f.thread.ID == "" {
		return f.threads, nil
	}

	return append([]reviewstore.Thread{f.thread}, f.threads...), nil
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

func (f *fakeReviewStore) CreateRound(
	_ context.Context,
	params reviewstore.CreateRoundParams,
) (*reviewstore.Round, error) {
	round := reviewstore.Round{
		ID:          fmt.Sprintf("round-%d", len(f.rounds)+1),
		AgentID:     params.AgentID,
		Ordinal:     int32(len(f.rounds) + 1),
		BaseSHA:     params.BaseSHA,
		SnapshotSHA: params.SnapshotSHA,
	}
	f.rounds = append(f.rounds, round)

	return &round, nil
}

func (f *fakeReviewStore) GetRound(_ context.Context, id string) (*reviewstore.Round, error) {
	for _, round := range f.rounds {
		if round.ID == id {
			return &round, nil
		}
	}

	return nil, fmt.Errorf("%w: review round %q", store.ErrNotFound, id)
}

func (f *fakeReviewStore) LatestRound(_ context.Context, agentID string) (*reviewstore.Round, error) {
	if len(f.rounds) == 0 {
		return nil, fmt.Errorf("%w: review round of %q", store.ErrNotFound, agentID)
	}

	round := f.rounds[len(f.rounds)-1]

	return &round, nil
}

func (f *fakeReviewStore) ListRounds(context.Context, string) ([]reviewstore.Round, error) {
	return f.rounds, nil
}

func (f *fakeReviewStore) OpenReview(
	_ context.Context,
	params reviewstore.OpenReviewParams,
) (*reviewstore.Review, error) {
	review := reviewstore.Review{
		ID:            reviewBatchID,
		AgentID:       params.AgentID,
		RoundID:       params.RoundID,
		AuthorKind:    params.AuthorKind,
		AuthorAgentID: params.AuthorAgentID,
	}
	f.open = &review
	f.reviews[review.ID] = review

	return &review, nil
}

func (f *fakeReviewStore) GetReview(_ context.Context, id string) (*reviewstore.Review, error) {
	review, ok := f.reviews[id]
	if !ok {
		return nil, fmt.Errorf("%w: review %q", store.ErrNotFound, id)
	}

	return &review, nil
}

func (f *fakeReviewStore) OpenReviewOf(_ context.Context, params reviewstore.AuthorParams) (*reviewstore.Review, error) {
	if f.open == nil {
		return nil, fmt.Errorf("%w: open review on %q", store.ErrNotFound, params.AgentID)
	}

	return f.open, nil
}

func (f *fakeReviewStore) LastReleasedReviewOf(_ context.Context, params reviewstore.AuthorParams) (*reviewstore.Review, error) {
	if f.released == nil {
		return nil, fmt.Errorf("%w: released review on %q", store.ErrNotFound, params.AgentID)
	}

	return f.released, nil
}

func (f *fakeReviewStore) ListReviews(context.Context, string) ([]reviewstore.Review, error) {
	reviews := make([]reviewstore.Review, 0, len(f.reviews))
	for _, review := range f.reviews {
		reviews = append(reviews, review)
	}

	return reviews, nil
}

func (f *fakeReviewStore) CountOpenComments(context.Context, string) (int, error) {
	return f.comments, nil
}

func (f *fakeReviewStore) ReleaseReview(
	_ context.Context,
	params reviewstore.ReleaseReviewParams,
) (*reviewstore.Review, error) {
	review, ok := f.reviews[params.ID]
	if !ok {
		return nil, fmt.Errorf("%w: review %q", store.ErrNotFound, params.ID)
	}

	verdict := params.Verdict
	review.Verdict = &verdict
	review.ReleasedAt = &settledAt
	review.Path = params.Path
	review.DoorbellMailID = params.DoorbellMailID

	f.reviews[review.ID] = review
	f.released = &review
	f.open = nil
	if params.DoorbellMailID != nil {
		f.doorbell = *params.DoorbellMailID
	}

	return &review, nil
}

func (f *fakeReviewStore) DeleteReview(context.Context, string) error { return nil }

// fakeSnapshotter freezes nothing: it answers with whatever tree it was told to, so a test can
// say the worktree has moved or has not without running git.
type fakeSnapshotter struct {
	sha  string
	base string
	held []string
	// files is each path as the round holds it, which is what a review quotes around a comment.
	files map[string][]string
}

func (f *fakeSnapshotter) Take(context.Context, git.TakeSnapshotParams) (*git.Snapshot, error) {
	return &git.Snapshot{BaseSHA: f.base, SHA: f.sha}, nil
}

func (f *fakeSnapshotter) Hold(_ context.Context, _, ref, _ string) error {
	f.held = append(f.held, ref)

	return nil
}

func (f *fakeSnapshotter) FileAt(_ context.Context, _, _, path string) ([]string, error) {
	lines, ok := f.files[path]
	if !ok {
		return nil, fmt.Errorf("no such path %q", path)
	}

	return lines, nil
}

type fakeWorkflowGetter struct{}

func (fakeWorkflowGetter) Get(context.Context, string) (*workflowstore.Workflow, error) {
	return &workflowstore.Workflow{
		Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"},
	}, nil
}

func (f *fakeReviewStore) MarkFileViewed(
	_ context.Context,
	params reviewstore.MarkFileViewedParams,
) (*reviewstore.ViewedFile, error) {
	if f.viewed == nil {
		f.viewed = map[string]bool{}
	}
	f.viewed[params.FilePath] = true

	return &reviewstore.ViewedFile{
		RoundID:  params.RoundID,
		FilePath: params.FilePath,
	}, nil
}

func (f *fakeReviewStore) UnmarkFileViewed(_ context.Context, _, filePath string) error {
	delete(f.viewed, filePath)

	return nil
}

func (f *fakeReviewStore) ListViewedFiles(_ context.Context, roundID string) ([]reviewstore.ViewedFile, error) {
	files := make([]reviewstore.ViewedFile, 0, len(f.viewed))
	for path := range f.viewed {
		files = append(files, reviewstore.ViewedFile{RoundID: roundID, FilePath: path})
	}

	return files, nil
}

func (f *fakeReviewStore) AddMessage(
	_ context.Context,
	params reviewstore.AddMessageParams,
) (*reviewstore.Message, error) {
	message := reviewstore.Message{
		ID:            fmt.Sprintf("message-%d", len(f.messages)+1),
		ThreadID:      params.ThreadID,
		ReviewID:      params.ReviewID,
		AuthorKind:    params.AuthorKind,
		AuthorAgentID: params.AuthorAgentID,
		Body:          params.Body,
		Suggestion:    params.Suggestion,
	}
	f.record(message)

	return &message, nil
}

func (f *fakeReviewStore) EditMessage(
	_ context.Context,
	id, body string,
	suggestion []string,
) (*reviewstore.Message, error) {
	message, ok := f.messages[id]
	if !ok {
		return nil, fmt.Errorf("%w: review message %q", store.ErrNotFound, id)
	}

	message.Body = body
	message.Suggestion = suggestion
	f.messages[id] = message

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
		if !wanted[message.ThreadID] {
			continue
		}

		if message.ReviewID != nil {
			if review, ok := f.reviews[*message.ReviewID]; ok {
				message.SentAt = review.ReleasedAt
			}
		}

		messages = append(messages, message)
	}
	slices.SortFunc(messages, func(a, b reviewstore.Message) int {
		return a.CreatedAt.Compare(b.CreatedAt)
	})

	return messages, nil
}

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

	_, err = service.MarkReviewThreadRead(t.Context(), MarkReviewThreadReadParams{ID: otherAgentID, Author: human})
	assert.ErrorIs(t, err, ErrReviewThreadNotFound)
}

// TestReleasingAReviewCostsTheMailboxOneNotice pins the mailbox price of letting go of a review:
// the comments are in a file, so a review of any size is one piece of mail and the rest of the
// mailbox is left for what other agents have to say.
func TestReleasingAReviewCostsTheMailboxOneNotice(t *testing.T) {
	for _, comments := range []int{1, 21, 140} {
		t.Run(fmt.Sprintf("%d comments", comments), func(t *testing.T) {
			reviews := newFakeReviewStore()
			reviews.comments = comments
			_, err := reviews.OpenReview(t.Context(), reviewstore.OpenReviewParams{
				AgentID: reviewedAgentID, RoundID: reviewRoundID, AuthorKind: reviewstore.AuthorHuman,
			})
			require.NoError(t, err)

			mailbox := &countingMailbox{}
			service := reviewServiceWithRealMail(t, reviews, mailbox)

			result, err := service.ReleaseReview(t.Context(), ReleaseReviewParams{
				AgentID: reviewedAgentID,
				Verdict: reviewstore.VerdictChangesRequested,
				Author:  humanAuthor(),
			})
			require.NoError(t, err, "a review is let go of whole however many comments it holds")
			require.NotNil(t, result)

			assert.Equal(t, comments, result.Comments)
			assert.Equal(t, 1, mailbox.pendingCount(), "one notice however much the review says")
		})
	}
}

// TestAReleasedReviewLeavesRoomForPeerMail pins that a release does not crowd out the mail
// other agents send, since read_mail hands over a bounded batch and doorbells filling it
// would bury an answer an agent is waiting on.
func TestAReleasedReviewLeavesRoomForPeerMail(t *testing.T) {
	reviews := newFakeReviewStore()
	reviews.comments = 30
	_, err := reviews.OpenReview(t.Context(), reviewstore.OpenReviewParams{
		AgentID: reviewedAgentID, RoundID: reviewRoundID, AuthorKind: reviewstore.AuthorHuman,
	})
	require.NoError(t, err)

	mailbox := &countingMailbox{}
	service := reviewServiceWithRealMail(t, reviews, mailbox)

	_, err = service.ReleaseReview(t.Context(), ReleaseReviewParams{
		AgentID: reviewedAgentID,
		Verdict: reviewstore.VerdictChangesRequested,
		Author:  humanAuthor(),
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
		Threads:   reviews,
		Messages:  reviews,
		Viewed:    reviews,
		Rounds:    reviews,
		Reviews:   reviews,
		Snapshots: &fakeSnapshotter{sha: "snapshot-sha", base: "base-sha"},
		Workflows: fakeWorkflowGetter{},
		StateDir:  t.TempDir(),
		Agents:    fakeAgentReaderFunc(findLiveAgent),
		Mailer:    mailService(mailbox),
	})
}

func mailService(mailbox *countingMailbox) *MailService {
	return NewMailService(NewMailServiceParams{
		MailReadWriter: mailbox,
		Agents:         fakeAgentReaderFunc(findLiveAgent),
		Config:         fakeMailConfig{maxExchange: config.DefaultMaxExchange},
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

func (f *countingMailbox) GetPendingOfKind(_ context.Context, toAgentID string, kind mailstore.Kind, _ *string) (*mailstore.Mail, error) {
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

func (f *countingMailbox) ListSentPage(context.Context, string, int32, *mailstore.Cursor) ([]mailstore.Mail, error) {
	return f.held, nil
}

func (f *countingMailbox) ListByID(context.Context, []string) ([]mailstore.Mail, error) {
	return f.held, nil
}

func (f *countingMailbox) Delete(context.Context, []string) error { return nil }

func (f *countingMailbox) ListUnreadMailboxes(context.Context) ([]string, error) { return nil, nil }

func (f *countingMailbox) CountExchanged(_ context.Context, one, other string) (int64, error) {
	var exchanged int64
	for _, mail := range f.held {
		if mail.Kind != mailstore.KindPlain || mail.FromAgentID == nil {
			continue
		}

		from, to := *mail.FromAgentID, mail.ToAgentID
		if (from == one && to == other) || (from == other && to == one) {
			exchanged++
		}
	}

	return exchanged, nil
}
