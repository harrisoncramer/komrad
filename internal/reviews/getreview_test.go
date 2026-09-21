package reviews

import (
	"fmt"
	"testing"

	mailstore "weave/internal/store/mail"
	reviewstore "weave/internal/store/review"
	"weave/lib/xptr"
	"weave/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAReleasedReviewIsReadWholeOverTheAPI pins that the agent gets, in one call, what the file
// carries: the verdict and body, the round, and each thread with the snapshot around it.
func TestAReleasedReviewIsReadWholeOverTheAPI(t *testing.T) {
	source := make([]string, 0, 20)
	for line := 1; line <= 20; line++ {
		source = append(source, fmt.Sprintf("line %d of greet.go", line))
	}

	reviews := newFakeReviewStore()
	reviews.thread.NewStart, reviews.thread.NewEnd = 12, 13
	reviews.comments = 1
	notices := &fakeNoticeSender{answer: &mailstore.Mail{ID: "mail-1"}}
	service := reviewServiceHolding(t, reviews, notices, map[string][]string{"greet.go": source})

	_, err := reviews.OpenReview(t.Context(), reviewstore.OpenReviewParams{
		AgentID:    reviewedAgentID,
		RoundID:    reviewRoundID,
		AuthorKind: reviewstore.AuthorHuman,
	})
	require.NoError(t, err)

	result, err := service.ReleaseReview(t.Context(), ReleaseReviewParams{
		AgentID: reviewedAgentID,
		Verdict: reviewstore.VerdictChangesRequested,
		Body:    "two things before it goes",
		Author:  humanAuthor(),
	})
	require.NoError(t, err)

	whole, err := service.GetReview(t.Context(), result.Review.ID, agentAuthor(reviewedAgentID))
	require.NoError(t, err)

	require.NotNil(t, whole.Review.Verdict)
	assert.Equal(t, reviewstore.VerdictChangesRequested, *whole.Review.Verdict)
	assert.Equal(t, "two things before it goes", whole.Review.Body)
	assert.Empty(t, whole.Review.Path, "the agent is not told where the file is")
	assert.Equal(t, "snapshot-sha", whole.Round.SnapshotSHA)
	assert.Equal(t, "worker", whole.Agent.Name)
	assert.Empty(t, whole.Author, "a person gave it")

	require.Len(t, whole.Threads, 1)
	thread := whole.Threads[0]
	assert.Equal(t, reviewThreadID, thread.Thread.ID)
	assert.Contains(t, thread.Context, ">> 12  line 12 of greet.go", "the commented lines are marked")
	assert.Contains(t, thread.Context, "    6  line 6 of greet.go", "six lines of context above")
	assert.Contains(t, thread.Context, "   19  line 19 of greet.go", "and six below")
	assert.Len(t, thread.Context, 14)

	bodies := xslice.Map(thread.Messages, func(message reviewstore.Message) string { return message.Body })
	assert.Equal(t, []string{"this needs a nil check"}, bodies, "only what went out with this review")
	assert.Nil(t, thread.Messages[0].SentAt, "and none of the panel's record of when")
	assert.Equal(t, reviewstore.DeliveryNone, thread.Messages[0].Delivery)
}

// TestAReviewWhoseSnapshotCannotBeReadHasNoContext pins that a file the round cannot hand back
// leaves the thread with its quoted lines rather than failing the read.
func TestAReviewWhoseSnapshotCannotBeReadHasNoContext(t *testing.T) {
	reviews := newFakeReviewStore()
	reviews.thread.FilePath = "gone.go"
	reviews.comments = 1
	notices := &fakeNoticeSender{answer: &mailstore.Mail{ID: "mail-1"}}
	service := reviewServiceHolding(t, reviews, notices, nil)

	_, err := reviews.OpenReview(t.Context(), reviewstore.OpenReviewParams{
		AgentID:    reviewedAgentID,
		RoundID:    reviewRoundID,
		AuthorKind: reviewstore.AuthorHuman,
	})
	require.NoError(t, err)

	result, err := service.ReleaseReview(t.Context(), ReleaseReviewParams{
		AgentID: reviewedAgentID,
		Verdict: reviewstore.VerdictChangesRequested,
		Author:  humanAuthor(),
	})
	require.NoError(t, err)

	whole, err := service.GetReview(t.Context(), result.Review.ID, agentAuthor(reviewedAgentID))
	require.NoError(t, err)
	require.Len(t, whole.Threads, 1)
	assert.Empty(t, whole.Threads[0].Context)
	assert.Equal(t, []string{"+\tgreet(nil)"}, whole.Threads[0].Thread.QuotedLines)
}

// TestAnUnreleasedReviewIsNotFoundForAnyoneButItsAuthor pins that a review still being written
// is the reviewer's own until it lets go, and that a review id is not a way round that.
func TestAnUnreleasedReviewIsNotFoundForAnyoneButItsAuthor(t *testing.T) {
	reviews := newFakeReviewStore()
	reviews.comments = 1
	service := reviewService(t, reviews, &fakeNoticeSender{answer: &mailstore.Mail{ID: "mail-1"}})

	open, err := reviews.OpenReview(t.Context(), reviewstore.OpenReviewParams{
		AgentID:       reviewedAgentID,
		RoundID:       reviewRoundID,
		AuthorKind:    reviewstore.AuthorAgent,
		AuthorAgentID: xptr.Ptr(reviewerAgentID),
	})
	require.NoError(t, err)

	_, err = service.GetReview(t.Context(), open.ID, agentAuthor(reviewedAgentID))
	require.ErrorIs(t, err, ErrReviewNotFound, "the agent under review is not told it exists")

	forAuthor, err := service.GetReview(t.Context(), open.ID, agentAuthor(reviewerAgentID))
	require.NoError(t, err, "but the reviewer reads its own draft")
	assert.Equal(t, "reviewer", forAuthor.Author)

	_, err = service.GetReview(t.Context(), open.ID, humanAuthor())
	require.NoError(t, err, "and so does the person")

	_, err = service.GetReview(t.Context(), "no-such-review", humanAuthor())
	require.ErrorIs(t, err, ErrReviewNotFound)
}

// TestOnlyTheReviewedAgentAndItsReviewerReadAReview pins the same rule list_reviews applies.
func TestOnlyTheReviewedAgentAndItsReviewerReadAReview(t *testing.T) {
	reviews := newFakeReviewStore()
	reviews.comments = 1
	service := reviewService(t, reviews, &fakeNoticeSender{answer: &mailstore.Mail{ID: "mail-1"}})

	_, err := reviews.OpenReview(t.Context(), reviewstore.OpenReviewParams{
		AgentID:    reviewedAgentID,
		RoundID:    reviewRoundID,
		AuthorKind: reviewstore.AuthorHuman,
	})
	require.NoError(t, err)

	result, err := service.ReleaseReview(t.Context(), ReleaseReviewParams{
		AgentID: reviewedAgentID,
		Verdict: reviewstore.VerdictApproved,
		Author:  humanAuthor(),
	})
	require.NoError(t, err)

	_, err = service.GetReview(t.Context(), result.Review.ID, agentAuthor(reviewerAgentID))
	require.NoError(t, err, "the child reviewing it reads it")

	_, err = service.GetReview(t.Context(), result.Review.ID, agentAuthor(otherAgentID))
	require.ErrorIs(t, err, ErrReviewNotEntitled)
}
