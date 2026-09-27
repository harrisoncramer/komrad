package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"

	agentv1 "komrad/proto/gen/agent/v1"
)

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
