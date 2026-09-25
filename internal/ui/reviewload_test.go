package ui

import (
	"strings"
	"testing"

	"komrad/internal/git"
	"komrad/internal/gittest"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
)

// TestChangedSinceLastReview covers the line drawn above a round saying how much of it the
// person has not seen.
func TestChangedSinceLastReview(t *testing.T) {
	tests := []struct {
		name    string
		earlier *agentv1.ReviewRound
		changed int
		refused bool
		want    string
	}{
		{
			name:    "no earlier review, so there is nothing to measure against",
			earlier: nil,
			changed: 3,
			want:    "",
		},
		{
			name:    "git cannot answer, so nothing is said rather than a wrong number",
			earlier: &agentv1.ReviewRound{SnapshotSha: "earlier"},
			refused: true,
			want:    "",
		},
		{
			name:    "nothing changed since that review",
			earlier: &agentv1.ReviewRound{SnapshotSha: "earlier"},
			changed: 0,
			want:    "",
		},
		{
			name:    "one file, named in the singular",
			earlier: &agentv1.ReviewRound{SnapshotSha: "earlier"},
			changed: 1,
			want:    "1 file changed since you last reviewed",
		},
		{
			name:    "several files",
			earlier: &agentv1.ReviewRound{SnapshotSha: "earlier"},
			changed: 4,
			want:    "4 files changed since you last reviewed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &gittest.Repo{Patches: map[string]string{}}
			if !tt.refused {
				repo.Patches[gittest.NamePair("earlier", "current")] = strings.Repeat("diff --git a/f.go b/f.go\n", tt.changed)
			}

			got := changedSinceLastReview(t.Context(), changedSinceLastReviewParams{
				Git:         repo,
				Dir:         "/repo",
				Exclude:     []string{"proto/gen"},
				Earlier:     tt.earlier,
				SnapshotSHA: "current",
			})

			assert.Equal(t, tt.want, got)
		})
	}
}

// TestChangedSinceLastReviewMeasuresBetweenTheTwoRounds pins which pair of commits the count
// is taken across.
func TestChangedSinceLastReviewMeasuresBetweenTheTwoRounds(t *testing.T) {
	repo := &gittest.Repo{}

	changedSinceLastReview(t.Context(), changedSinceLastReviewParams{
		Git:         repo,
		Dir:         "/repo",
		Exclude:     []string{"proto/gen"},
		Earlier:     &agentv1.ReviewRound{SnapshotSha: "earlier"},
		SnapshotSHA: "current",
	})

	assert.Equal(t, []git.CountChangedFilesParams{{
		Dir:     "/repo",
		FromSHA: "earlier",
		ToSHA:   "current",
		Exclude: []string{"proto/gen"},
	}}, repo.CountsAsked())
}
