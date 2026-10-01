package reviews

import (
	"testing"

	"komrad/internal/notices"
	reviewstore "komrad/internal/store/review"

	"github.com/stretchr/testify/assert"
)

// TestReleasedNotice pins the notice each release starts.
func TestReleasedNotice(t *testing.T) {
	tests := []struct {
		name     string
		released Released
		want     string
	}{
		{
			name:     "an approval",
			released: Released{Verdict: reviewstore.VerdictApproved},
			want:     notices.ReviewApproved,
		},
		{
			name:     "a request for changes",
			released: Released{Verdict: reviewstore.VerdictChangesRequested},
			want:     notices.ReviewChangesRequested,
		},
		{
			name:     "a reviewer only commenting",
			released: Released{Verdict: reviewstore.VerdictCommented},
			want:     notices.ReviewCommented,
		},
		{
			name:     "a person commenting",
			released: Released{Verdict: reviewstore.VerdictCommented, ByPerson: true},
			want:     notices.ReviewCommentedByPerson,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.released.notice())
		})
	}
}
