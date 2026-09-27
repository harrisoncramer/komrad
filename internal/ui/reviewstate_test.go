package ui

import (
	"testing"

	"komrad/internal/diff"
	"komrad/internal/ui/diffview"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
)

// TestIsReviewHistory covers a comment being refused on a diff drawn to an older snapshot than its round.
func TestIsReviewHistory(t *testing.T) {
	tests := []struct {
		name string
		diff reviewDiff
		want bool
	}{
		{
			name: "the chapter list was recut under an index that stood still",
			diff: reviewDiff{
				chapters: []reviewChapter{{ToSHA: "s3", Ordinal: 3}},
				head:     "s2",
			},
			want: true,
		},
		{
			name: "the base and head were cleared under a diff still drawn to an older round",
			diff: reviewDiff{
				chapter: -1,
				Pane:    diffview.NewPane(diffview.NewView(&diff.Patch{BaseSHA: "s1", SnapshotSHA: "s2"}, false)),
			},
			want: true,
		},
		{
			name: "the diff stands on the round asked for",
			diff: reviewDiff{
				chapters: []reviewChapter{{ToSHA: "s3", Ordinal: 3}},
				head:     "s3",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			screen := reviewScreen{
				round: &agentv1.ReviewRound{Id: "r3", SnapshotSha: "s3"},
				diff:  tt.diff,
			}

			said, history := screen.isHistory()

			assert.Equal(t, tt.want, history)
			assert.Equal(t, tt.want, said != "")
		})
	}
}
