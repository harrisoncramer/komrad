package history

import (
	"strings"
	"testing"

	"komrad/internal/git"
	"komrad/internal/gittest"

	"github.com/stretchr/testify/assert"
)

// TestCountChangedSince covers how much of a snapshot the person has not seen.
func TestCountChangedSince(t *testing.T) {
	tests := []struct {
		name    string
		earlier *Snapshot
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
			earlier: &Snapshot{SHA: "earlier"},
			refused: true,
			want:    0,
		},
		{
			name:    "nothing changed since that review",
			earlier: &Snapshot{SHA: "earlier"},
			changed: 0,
			want:    0,
		},
		{
			name:    "several files",
			earlier: &Snapshot{SHA: "earlier"},
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

// TestCountChangedSinceMeasuresBetweenTheTwoSnapshots pins which pair of commits the count is taken
// across.
func TestCountChangedSinceMeasuresBetweenTheTwoSnapshots(t *testing.T) {
	repo := &gittest.Repo{}

	countChangedSince(t.Context(), countChangedSinceParams{
		Git:     repo,
		Dir:     "/repo",
		Exclude: []string{"proto/gen"},
		Earlier: &Snapshot{SHA: "earlier"},
		ToSHA:   "current",
	})

	assert.Equal(t, []git.CountChangedFilesParams{{
		Dir:     "/repo",
		FromSHA: "earlier",
		ToSHA:   "current",
		Exclude: []string{"proto/gen"},
	}}, repo.CountsAsked())
}
