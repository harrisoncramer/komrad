package history

import (
	"strings"
	"testing"

	"komrad/internal/git"
	"komrad/internal/gittest"

	"github.com/stretchr/testify/assert"
)

// TestCountChangedSince covers how much of a round the person has not seen.
func TestCountChangedSince(t *testing.T) {
	tests := []struct {
		name    string
		earlier *Round
		changed int
		refused bool
		want    int
	}{
		{
			name:    "no earlier review, so there is nothing to measure against",
			earlier: nil,
			changed: 3,
			want:    0,
		},
		{
			name:    "git cannot answer, so nothing is said rather than a wrong number",
			earlier: &Round{SnapshotSHA: "earlier"},
			refused: true,
			want:    0,
		},
		{
			name:    "nothing changed since that review",
			earlier: &Round{SnapshotSHA: "earlier"},
			changed: 0,
			want:    0,
		},
		{
			name:    "several files",
			earlier: &Round{SnapshotSHA: "earlier"},
			changed: 4,
			want:    4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &gittest.Repo{Patches: map[string]string{}}
			if !tt.refused {
				repo.Patches[gittest.NamePair("earlier", "current")] = strings.Repeat("diff --git a/f.go b/f.go\n", tt.changed)
			}

			got := countChangedSince(t.Context(), countChangedSinceParams{
				Git:     repo,
				Dir:     "/repo",
				Exclude: []string{"proto/gen"},
				Earlier: tt.earlier,
				ToSHA:   "current",
			})

			assert.Equal(t, tt.want, got)
		})
	}
}

// TestCountChangedSinceMeasuresBetweenTheTwoRounds pins which pair of commits the count is taken
// across.
func TestCountChangedSinceMeasuresBetweenTheTwoRounds(t *testing.T) {
	repo := &gittest.Repo{}

	countChangedSince(t.Context(), countChangedSinceParams{
		Git:     repo,
		Dir:     "/repo",
		Exclude: []string{"proto/gen"},
		Earlier: &Round{SnapshotSHA: "earlier"},
		ToSHA:   "current",
	})

	assert.Equal(t, []git.CountChangedFilesParams{{
		Dir:     "/repo",
		FromSHA: "earlier",
		ToSHA:   "current",
		Exclude: []string{"proto/gen"},
	}}, repo.CountsAsked())
}
