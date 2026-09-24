package ui

import (
	"testing"

	agentv1 "weave/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// chapterRounds is the shape the chapter tests share: rounds one to three, all off one base.
func chapterRounds() []*agentv1.ReviewRound {
	return []*agentv1.ReviewRound{
		{
			Id:          "r1",
			Ordinal:     1,
			BaseSha:     "b",
			SnapshotSha: "s1",
			CreatedAt:   "2026-09-08T13:00:00-04:00",
		},
		{
			Id:          "r2",
			Ordinal:     2,
			BaseSha:     "b",
			SnapshotSha: "s2",
			CreatedAt:   "2026-09-08T14:00:00-04:00",
		},
		{
			Id:          "r3",
			Ordinal:     3,
			BaseSha:     "b",
			SnapshotSha: "s3",
			CreatedAt:   "2026-09-08T15:00:00-04:00",
		},
	}
}

func releasedAgainst(ordinal int32, roundID, at string) *agentv1.Review {
	return &agentv1.Review{
		Id:           "v" + roundID,
		AuthorKind:   agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		RoundId:      roundID,
		RoundOrdinal: ordinal,
		ReleasedAt:   at,
	}
}

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

// TestTheNewestChapterHidesNothing covers a diff pinned to a round the agent has already moved
// past: the chapter that ends where the reader is looking must not be closed by a round they
// cannot see, or their own newest comments are cut out of the only view that draws them.
func TestTheNewestChapterHidesNothing(t *testing.T) {
	rounds := chapterRounds()
	reviews := []*agentv1.Review{releasedAgainst(1, "r1", "2026-09-08T13:30:00-04:00")}

	chapters := cutChapters(cutChaptersParams{
		Rounds:   rounds,
		Reviews:  reviews,
		OnScreen: rounds[1],
	})

	require.Len(t, chapters, 2)
	assert.Empty(t, chapters[1].Until, "the chapter the reader is standing on closes at nothing")
}

// TestAMessageWithAnUnreadableStampIsKept covers a stamp that will not parse: hiding the comment
// leaves a thread drawn with no text and no way to find out why, so an unreadable stamp has to
// mean show it.
func TestAMessageWithAnUnreadableStampIsKept(t *testing.T) {
	chapters := []reviewChapter{{
		ToSHA:   "s1",
		Ordinal: 1,
		Until:   "2026-09-08T14:00:00-04:00",
	}}

	thread := &agentv1.ReviewThread{
		Id:           "t1",
		RoundOrdinal: 1,
		Messages: []*agentv1.ReviewMessage{
			{Id: "m1", CreatedAt: "2026-09-08T13:10:00-04:00"},
			{Id: "m2", CreatedAt: "not a stamp"},
			{Id: "m3", CreatedAt: ""},
		},
	}

	drawn := threadsByChapter(threadsByChapterParams{
		Threads:  []*agentv1.ReviewThread{thread},
		Chapters: chapters,
		At:       0,
	})

	require.Len(t, drawn, 1)
	assert.Len(t, drawn[0].GetMessages(), 3, "a stamp nobody can read is not a reason to hide a comment")
}
