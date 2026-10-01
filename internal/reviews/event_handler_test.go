package reviews

import (
	"testing"

	"komrad/internal/eventprompts"
	reviewstore "komrad/internal/store/review"

	"github.com/stretchr/testify/assert"
)

// TestReleasedEvent pins the event each release starts.
func TestReleasedEvent(t *testing.T) {
	tests := []struct {
		name     string
		released Released
		want     string
	}{
		{
			name:     "an approval",
			released: Released{Verdict: reviewstore.VerdictApproved},
			want:     eventprompts.ReviewApproved,
		},
		{
			name:     "a request for changes",
			released: Released{Verdict: reviewstore.VerdictChangesRequested},
			want:     eventprompts.ReviewChangesRequested,
		},
		{
			name:     "a reviewer only commenting",
			released: Released{Verdict: reviewstore.VerdictCommented},
			want:     eventprompts.ReviewCommented,
		},
		{
			name:     "a person commenting",
			released: Released{Verdict: reviewstore.VerdictCommented, ByPerson: true},
			want:     eventprompts.ReviewCommentedByPerson,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.released.event())
		})
	}
}
