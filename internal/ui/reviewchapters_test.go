package ui

import (
	"testing"

	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestThreadsByChapter covers a chapter keeping the threads opened by its round, with every reply.
func TestThreadsByChapter(t *testing.T) {
	rounds := buildChapterRounds()
	reviews := []*agentv1.Review{
		{
			AuthorKind:   agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
			RoundOrdinal: 1,
			ReleasedAt:   "2026-09-08T13:30:00-04:00",
		},
		{
			AuthorKind:   agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
			RoundOrdinal: 2,
			ReleasedAt:   "2026-09-08T14:30:00-04:00",
		},
	}
	chapters := cutChapters(cutChaptersParams{
		Rounds:   rounds,
		Reviews:  reviews,
		OnScreen: rounds[2],
	})
	require.Len(t, chapters, 3)

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
				CreatedAt: "2026-09-08T15:27:00-04:00",
			},
		},
	}
	opened := &agentv1.ReviewThread{
		Id:           "t2",
		RoundOrdinal: 2,
		Messages:     []*agentv1.ReviewMessage{{Id: "m5", CreatedAt: "2026-09-08T14:10:00-04:00"}},
	}
	threads := []*agentv1.ReviewThread{thread, opened}

	drawn := threadsByChapter(threadsByChapterParams{
		Threads:  threads,
		Chapters: chapters,
		At:       0,
	})
	require.Len(t, drawn, 1, "a thread opened on round 2 is not drawn on round 1")
	assert.Len(t, drawn[0].GetMessages(), 4, "the replies written after round 1 are kept")

	drawn = threadsByChapter(threadsByChapterParams{
		Threads:  threads,
		Chapters: chapters,
		At:       1,
	})
	assert.Len(t, drawn, 2, "round 2 draws both")

	whole := threadsByChapter(threadsByChapterParams{
		Threads:  threads,
		Chapters: chapters,
		At:       -1,
	})
	assert.Len(t, whole, 2, "the whole change carries the whole conversation")
}

// TestFindLaterStamp covers where a thread's later replies are split from what was said first.
func TestFindLaterStamp(t *testing.T) {
	tests := []struct {
		name    string
		ordinal int32
		first   string
		want    string
	}{
		{
			name:    "a thread on round 1 splits when round 2 was taken",
			ordinal: 1,
			first:   "2026-09-08T13:10:00-04:00",
			want:    "2026-09-08T14:00:00-04:00",
		},
		{
			name:    "a thread written into round 1 after round 2 existed splits at round 3",
			ordinal: 1,
			first:   "2026-09-08T14:10:00-04:00",
			want:    "2026-09-08T15:00:00-04:00",
		},
		{
			name:    "a thread opened after round 2 was taken splits at round 3",
			ordinal: 2,
			first:   "2026-09-08T14:10:00-04:00",
			want:    "2026-09-08T15:00:00-04:00",
		},
		{
			name:    "a thread on the newest round has nothing after it",
			ordinal: 3,
			first:   "2026-09-08T15:10:00-04:00",
			want:    "",
		},
		{
			name:    "a thread written after every round has nothing after it",
			ordinal: 1,
			first:   "2026-09-08T16:00:00-04:00",
			want:    "",
		},
		{
			name:    "a first message whose stamp will not parse splits nowhere",
			ordinal: 1,
			first:   "not a stamp",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			thread := &agentv1.ReviewThread{
				Id:           "t1",
				RoundOrdinal: tt.ordinal,
				Messages:     []*agentv1.ReviewMessage{{Id: "m1", CreatedAt: tt.first}},
			}

			assert.Equal(t, tt.want, findLaterStamp(thread, buildChapterRounds()))
		})
	}
}

func buildChapterRounds() []*agentv1.ReviewRound {
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
