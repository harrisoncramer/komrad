package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"

	agentv1 "weave/proto/gen/agent/v1"
)

// Explainer: the service reads the newest released review and nothing else, so a panel that
// looked at any other one would refuse keys the service allows, and nothing the reader does on
// the round on screen would lift it.

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
			assert.Equal(t, tt.wants, lastOwedReview(tt.reviews) != nil)
		})
	}
}
