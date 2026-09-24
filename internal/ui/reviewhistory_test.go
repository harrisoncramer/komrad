package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"

	agentv1 "komrade/proto/gen/agent/v1"
)

// TestOnlyTheLastReleasedReviewIsOwedAnAnswer pins which review the screen reads and what it
// reads off it.
func TestOnlyTheLastReleasedReviewIsOwedAnAnswer(t *testing.T) {
	review := func(released string, open int32, kind agentv1.ReviewAuthorKind) *agentv1.Review {
		return &agentv1.Review{
			ReleasedAt:  released,
			OpenThreads: open,
			AuthorKind:  kind,
		}
	}
	human := agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN
	agent := agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT

	tests := []struct {
		name    string
		reviews []*agentv1.Review
		wants   bool
	}{
		{
			name:    "nothing reviewed yet",
			reviews: nil,
			wants:   false,
		},
		{
			name:    "still being written, so nobody has been asked anything",
			reviews: []*agentv1.Review{review("", 2, human)},
			wants:   false,
		},
		{
			name:    "released and settled",
			reviews: []*agentv1.Review{review("2026-09-13T10:00:00Z", 0, human)},
			wants:   false,
		},
		{
			name:    "released with a comment nobody has settled",
			reviews: []*agentv1.Review{review("2026-09-13T10:00:00Z", 1, human)},
			wants:   true,
		},
		{
			name:    "an agent's own review is not the person's to be owed",
			reviews: []*agentv1.Review{review("2026-09-13T10:00:00Z", 3, agent)},
			wants:   false,
		},
		{
			name: "an older review left open, which the service no longer reads",
			reviews: []*agentv1.Review{
				review("2026-09-13T10:00:00Z", 1, human),
				review("2026-09-13T11:00:00Z", 0, human),
			},
			wants: false,
		},
		{
			name: "an hour later across the clocks going back, which is not a later string",
			reviews: []*agentv1.Review{
				review("2026-11-01T01:30:00-04:00", 1, human),
				review("2026-11-01T01:15:00-05:00", 0, human),
			},
			wants: false,
		},
		{
			name: "the newest one left open, whatever came before it",
			reviews: []*agentv1.Review{
				review("2026-09-13T11:00:00Z", 2, human),
				review("2026-09-13T10:00:00Z", 0, human),
			},
			wants: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wants, findLastOwedReview(tt.reviews) != nil)
		})
	}
}

// TestFindPendingRound covers which round an open batch of the reader's comments holds the diff to.
func TestFindPendingRound(t *testing.T) {
	rounds := []*agentv1.ReviewRound{
		{
			Id:          "r1",
			Ordinal:     1,
			SnapshotSha: "s1",
		},
		{
			Id:          "r5",
			Ordinal:     5,
			SnapshotSha: "s5",
		},
	}

	tests := []struct {
		name    string
		reviews []*agentv1.Review
		want    string
	}{
		{
			name: "a released review holds nothing open",
			reviews: []*agentv1.Review{
				{
					Id:         "v1",
					AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
					RoundId:    "r1",
					ReleasedAt: "2026-09-08T13:24:00-04:00",
				},
			},
		},
		{
			name: "an agent's open review pins nothing",
			reviews: []*agentv1.Review{
				{
					Id:         "v1",
					AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT,
					RoundId:    "r1",
				},
			},
		},
		{
			name: "the open batch pins its own round rather than an older thread's",
			reviews: []*agentv1.Review{
				{
					Id:         "v1",
					AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
					RoundId:    "r1",
					ReleasedAt: "2026-09-08T13:24:00-04:00",
				},
				{
					Id:         "v2",
					AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
					RoundId:    "r5",
				},
			},
			want: "s5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			round, pinned := findPendingRound(rounds, openBatchRoundID(tt.reviews))

			assert.Equal(t, tt.want != "", pinned)
			assert.Equal(t, tt.want, round.GetSnapshotSha())
		})
	}
}
