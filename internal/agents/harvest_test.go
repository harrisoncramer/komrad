package agents

import (
	"context"
	"testing"

	"weave/internal/review"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// harvestedFile is the one file the round holds in these tests.
const harvestedFile = "greet.go"

// harvestRun is one edited run anchored the way the panel anchors it before sending.
func harvestRun(start, end int, lines ...string) review.Run {
	return review.Run{
		FilePath:    harvestedFile,
		OldStart:    start,
		OldEnd:      end,
		NewStart:    start,
		NewEnd:      end,
		QuotedLines: []string{" was"},
		Lines:       lines,
	}
}

func harvestService(t *testing.T) (*ReviewService, *fakeReviewStore) {
	t.Helper()

	reviews := newFakeReviewStore()
	reviews.thread.ID = ""

	return reviewServiceHolding(t, reviews, &fakeNoticeSender{}, map[string][]string{
		harvestedFile: {"one", "two", "three", "four"},
	}), reviews
}

func harvest(t *testing.T, service *ReviewService, runs ...review.Run) *HarvestResult {
	t.Helper()

	result, err := service.HarvestRoundEdits(context.Background(), HarvestRoundEditsParams{
		AgentID: reviewedAgentID,
		RoundID: reviewRoundID,
		Runs:    runs,
	})
	require.NoError(t, err)

	return result
}

// TestAHarvestOpensAThreadCarryingTheEdit is the whole point: the reviewer types in the checkout
// and a comment exists without them pressing anything else.
func TestAHarvestOpensAThreadCarryingTheEdit(t *testing.T) {
	t.Parallel()

	service, reviews := harvestService(t)

	result := harvest(t, service, harvestRun(2, 2, "fixed"))

	assert.Equal(t, 1, result.Opened)
	require.Len(t, reviews.threads, 1)
	assert.True(t, reviews.threads[0].Harvested)
	assert.Equal(t, reviewRoundID, reviews.threads[0].RoundID)

	var suggestion []string
	for _, message := range reviews.messages {
		if message.ThreadID == reviews.threads[0].ID {
			suggestion = message.Suggestion
			assert.Equal(t, HarvestedBody, message.Body)
		}
	}
	assert.Equal(t, []string{"fixed"}, suggestion)
}

// TestHarvestingTheSameEditTwiceOpensOneThread is what lets harvest fire from the editor closing,
// from leaving the screen, and from checking out a newer round, without multiplying comments.
func TestHarvestingTheSameEditTwiceOpensOneThread(t *testing.T) {
	t.Parallel()

	service, reviews := harvestService(t)
	edit := harvestRun(2, 2, "fixed")

	harvest(t, service, edit)
	second := harvest(t, service, edit)

	assert.Equal(t, 0, second.Opened)
	assert.Equal(t, 0, second.Moved)
	assert.Equal(t, 0, second.Deleted)
	assert.Len(t, reviews.threads, 1)
}

// TestEditingAgainRewritesTheSameThread pins that a reviewer who keeps tweaking one line ends up
// with one comment holding their latest text.
func TestEditingAgainRewritesTheSameThread(t *testing.T) {
	t.Parallel()

	service, reviews := harvestService(t)

	harvest(t, service, harvestRun(2, 2, "first"))
	result := harvest(t, service, harvestRun(2, 2, "second"))

	assert.Equal(t, 1, result.Moved)
	assert.Equal(t, 0, result.Opened)
	require.Len(t, reviews.threads, 1)

	for _, message := range reviews.messages {
		if message.ThreadID == reviews.threads[0].ID {
			assert.Equal(t, []string{"second"}, message.Suggestion)
		}
	}
}

// TestUndoingTheEditTakesTheCommentWithIt is how a harvested comment is retracted, since there is
// no key that turns one down any more.
func TestUndoingTheEditTakesTheCommentWithIt(t *testing.T) {
	t.Parallel()

	service, reviews := harvestService(t)

	harvest(t, service, harvestRun(2, 2, "fixed"))
	result := harvest(t, service)

	assert.Equal(t, 1, result.Deleted)
	assert.Empty(t, reviews.threads)
}

// TestAHarvestWillNotRewriteAReleasedComment is the rule that keeps weave off words the agent has
// already been sent.
func TestAHarvestWillNotRewriteAReleasedComment(t *testing.T) {
	t.Parallel()

	reviews := newFakeReviewStore()
	reviews.thread.NewStart = 2
	reviews.thread.NewEnd = 2
	service := reviewServiceHolding(t, reviews, &fakeNoticeSender{}, map[string][]string{
		harvestedFile: {"one", "two", "three", "four"},
	})

	result := harvest(t, service, harvestRun(2, 2, "mine"))

	assert.Equal(t, 0, result.Opened)
	assert.Len(t, result.Skipped, 1)
	assert.Empty(t, reviews.threads)
}

// TestAHarvestBigEnoughToBeAFormatterIsRefused is the guard on an editor that reformats the whole
// file on save, which would otherwise open a comment per run without the reviewer meaning any.
func TestAHarvestBigEnoughToBeAFormatterIsRefused(t *testing.T) {
	t.Parallel()

	service, reviews := harvestService(t)

	runs := make([]review.Run, 0, harvestRunCeiling+1)
	for i := range harvestRunCeiling + 1 {
		runs = append(runs, harvestRun(i+1, i+1, "reformatted"))
	}

	_, err := service.HarvestRoundEdits(context.Background(), HarvestRoundEditsParams{
		AgentID: reviewedAgentID,
		RoundID: reviewRoundID,
		Runs:    runs,
	})

	require.ErrorIs(t, err, ErrHarvestTooLarge)
	assert.Empty(t, reviews.threads, "nothing should be written by a refused harvest")
}

// TestARefusedHarvestCanBeForced is the reviewer answering for a large edit they meant.
func TestARefusedHarvestCanBeForced(t *testing.T) {
	t.Parallel()

	service, reviews := harvestService(t)

	runs := make([]review.Run, 0, harvestRunCeiling+1)
	for i := range harvestRunCeiling + 1 {
		runs = append(runs, harvestRun(i+1, i+1, "reformatted"))
	}

	_, err := service.HarvestRoundEdits(context.Background(), HarvestRoundEditsParams{
		AgentID: reviewedAgentID,
		RoundID: reviewRoundID,
		Runs:    runs,
		Force:   true,
	})

	require.NoError(t, err)
	assert.Len(t, reviews.threads, harvestRunCeiling+1)
}

// TestAHarvestOfAnotherAgentsRoundIsRefused stops a round id naming somebody else's work.
func TestAHarvestOfAnotherAgentsRoundIsRefused(t *testing.T) {
	t.Parallel()

	service, _ := harvestService(t)

	_, err := service.HarvestRoundEdits(context.Background(), HarvestRoundEditsParams{
		AgentID: otherAgentID,
		RoundID: reviewRoundID,
		Runs:    []review.Run{harvestRun(2, 2, "fixed")},
	})

	require.Error(t, err)
}
