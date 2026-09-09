package ui

import (
	"testing"

	agentv1 "weave/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChapterFilterCutsLaterReplies covers a thread from an earlier round carrying replies
// written after that round closed, which read as though they had been said about its code.
func TestChapterFilterCutsLaterReplies(t *testing.T) {
	rounds := []*agentv1.ReviewRound{
		{
			Ordinal:     1,
			BaseSha:     "b",
			SnapshotSha: "s1",
			CreatedAt:   "2026-09-08T13:17:00-04:00",
		},
		{
			Ordinal:     2,
			BaseSha:     "b",
			SnapshotSha: "s2",
			CreatedAt:   "2026-09-08T13:26:00-04:00",
		},
		{
			Ordinal:     3,
			BaseSha:     "b",
			SnapshotSha: "s3",
			CreatedAt:   "2026-09-08T14:26:00-04:00",
		},
	}
	reviews := []*agentv1.Review{
		{
			AuthorKind:   agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
			RoundOrdinal: 1,
			ReleasedAt:   "2026-09-08T13:24:00-04:00",
		},
		{
			AuthorKind:   agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
			RoundOrdinal: 2,
			ReleasedAt:   "2026-09-08T14:25:00-04:00",
		},
	}
	chapters := cutChapters(cutChaptersParams{
		Rounds:   rounds,
		Reviews:  reviews,
		OnScreen: rounds[2],
	})
	require.Len(t, chapters, 3)
	assert.Equal(t, "2026-09-08T13:26:00-04:00", chapters[0].Until, "round 1 closed when round 2 was taken")

	thread := &agentv1.ReviewThread{
		Id:           "t1",
		RoundOrdinal: 1,
		FilePath:     "greet.go",
		Messages: []*agentv1.ReviewMessage{
			{
				Id:        "m1",
				Body:      "two places",
				CreatedAt: "2026-09-08T13:20:00-04:00",
			},
			{
				Id:        "m2",
				Body:      "pulled into nameOr",
				CreatedAt: "2026-09-08T13:24:00-04:00",
			},
			{
				Id:        "m3",
				Body:      "sentence case?",
				CreatedAt: "2026-09-08T14:25:00-04:00",
			},
			{
				Id:        "m4",
				Body:      "Hello now returns",
				CreatedAt: "2026-09-08T14:27:00-04:00",
			},
		},
	}

	drawn := threadsByChapter(threadsByChapterParams{
		Threads:  []*agentv1.ReviewThread{thread},
		Chapters: chapters,
		At:       0,
	})
	require.Len(t, drawn, 1)
	assert.Len(t, drawn[0].GetMessages(), 2, "only what was said while round 1 was the work in hand")
	assert.Len(t, thread.GetMessages(), 4, "and the model's own thread is untouched")

	whole := threadsByChapter(threadsByChapterParams{
		Threads:  []*agentv1.ReviewThread{thread},
		Chapters: chapters,
		At:       -1,
	})
	assert.Len(t, whole[0].GetMessages(), 4, "the whole change carries the whole conversation")
}

// TestThePinFollowsTheBatchNotTheThread covers a reply written into a thread from an earlier
// round: the unsent message sits on that old thread, and reading the round off it dragged the
// whole diff back to whenever that conversation started, which hid the compose key and emptied
// the walk.
func TestThePinFollowsTheBatchNotTheThread(t *testing.T) {
	agents := &fakeReviewAgents{
		rounds: []*agentv1.ReviewRound{
			{
				Id:          "r1",
				Ordinal:     1,
				BaseSha:     "b",
				SnapshotSha: "s1",
			},
			{
				Id:          "r5",
				Ordinal:     5,
				BaseSha:     "b",
				SnapshotSha: "s5",
			},
		},
		reviews: []*agentv1.Review{
			{
				Id:           "v1",
				AuthorKind:   agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
				RoundId:      "r1",
				RoundOrdinal: 1,
				ReleasedAt:   "2026-09-08T13:24:00-04:00",
			},
			{
				Id:           "v2",
				AuthorKind:   agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
				RoundId:      "r5",
				RoundOrdinal: 5,
			},
		},
	}

	reviews, err := listReviews(t.Context(), agents, "a1")
	require.NoError(t, err)

	rounds, err := listReviewRounds(t.Context(), agents, "a1")
	require.NoError(t, err)

	round, pinned := findPendingRound(rounds, openBatchRoundID(reviews))
	require.NotEqual(t, "r1", round.GetId(), "not the round the replied-to thread belongs to")
	require.True(t, pinned, "an open batch pins the diff")
	require.NotNil(t, round)
	assert.Equal(t, "r5", round.GetId(), "to the round the batch was opened against")
	assert.Equal(t, "s5", round.GetSnapshotSha())
}

// TestNothingOpenLeavesTheDiffAlone covers the ordinary case, where the newest round is taken
// rather than an old one held open.
func TestNothingOpenLeavesTheDiffAlone(t *testing.T) {
	agents := &fakeReviewAgents{
		rounds: []*agentv1.ReviewRound{{
			Id:          "r1",
			Ordinal:     1,
			SnapshotSha: "s1",
		}},
		reviews: []*agentv1.Review{{
			Id:         "v1",
			AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
			RoundId:    "r1",
			ReleasedAt: "2026-09-08T13:24:00-04:00",
		}},
	}

	reviews, err := listReviews(t.Context(), agents, "a1")
	require.NoError(t, err)

	rounds, err := listReviewRounds(t.Context(), agents, "a1")
	require.NoError(t, err)

	round, pinned := findPendingRound(rounds, openBatchRoundID(reviews))
	assert.False(t, pinned, "a released review holds nothing open")
	assert.Nil(t, round)
}

// TestOneLoadReadsTheReviewsAndRoundsOnce covers the load asking the server for the same two
// lists once per question it had, which was nine calls for six answers and let the answers
// describe different moments.
func TestOneLoadReadsTheReviewsAndRoundsOnce(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{
		rounds: []*agentv1.ReviewRound{{
			Id:          "r1",
			Ordinal:     1,
			SnapshotSha: "s1",
		}},
		reviews: []*agentv1.Review{{
			Id:           "v1",
			AuthorKind:   agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
			RoundId:      "r1",
			RoundOrdinal: 1,
			ReleasedAt:   "2026-09-08T13:24:00-04:00",
		}},
	}

	m, _ := reviewModel(t, agents, dir)
	openReview(t, m)

	assert.Equal(t, 1, agents.reviewReads, "the reviews are read once for the whole load")
	assert.Equal(t, 1, agents.roundReads, "and so are the rounds")
}
