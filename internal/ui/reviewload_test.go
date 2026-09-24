package ui

import (
	"context"
	"testing"

	"weave/internal/git"
	"weave/internal/gittest"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
)

// countingGit answers with a fixed number of changed files, and records what it was asked.
type countingGit struct {
	gittest.NoRepo
	changed int
	err     error
	asked   git.CountChangedFilesParams
}

func (c *countingGit) CountChangedFiles(_ context.Context, params git.CountChangedFilesParams) (int, error) {
	c.asked = params

	return c.changed, c.err
}

// TestChangedSinceLastReview covers the line drawn above a round saying how much of it the
// person has not seen.
func TestChangedSinceLastReview(t *testing.T) {
	tests := []struct {
		name    string
		earlier *agentv1.ReviewRound
		changed int
		err     error
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
			err:     gittest.ErrNoGit,
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
			counter := &countingGit{changed: tt.changed, err: tt.err}

			got := changedSinceLastReview(t.Context(), changedSinceLastReviewParams{
				Git:         counter,
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
	counter := &countingGit{changed: 2}

	changedSinceLastReview(t.Context(), changedSinceLastReviewParams{
		Git:         counter,
		Dir:         "/repo",
		Exclude:     []string{"proto/gen"},
		Earlier:     &agentv1.ReviewRound{SnapshotSha: "earlier"},
		SnapshotSHA: "current",
	})

	assert.Equal(t, git.CountChangedFilesParams{
		Dir:     "/repo",
		FromSHA: "earlier",
		ToSHA:   "current",
		Exclude: []string{"proto/gen"},
	}, counter.asked)
}
