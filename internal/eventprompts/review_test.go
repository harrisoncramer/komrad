package eventprompts

import (
	"testing"

	reviewstore "komrad/internal/store/review"

	"github.com/stretchr/testify/assert"
)

// TestReleasedReviewStartsTheEventForItsVerdict pins the event each release starts.
func TestReleasedReviewStartsTheEventForItsVerdict(t *testing.T) {
	tests := []struct {
		name string
		mail ReviewReleased
		want string
	}{
		{
			name: "an approval",
			mail: ReviewReleased{Verdict: reviewstore.VerdictApproved},
			want: ReviewApproved,
		},
		{
			name: "a request for changes",
			mail: ReviewReleased{Verdict: reviewstore.VerdictChangesRequested},
			want: ReviewChangesRequested,
		},
		{
			name: "a reviewer only commenting",
			mail: ReviewReleased{Verdict: reviewstore.VerdictCommented},
			want: reviewCommented,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, []string{tt.want}, tt.mail.Events())
		})
	}
}

// TestReviewCommentStartsThePersonCommentedEvent pins the event a person's comment starts.
func TestReviewCommentStartsThePersonCommentedEvent(t *testing.T) {
	assert.Equal(t, []string{ReviewCommentedByPerson}, ReviewComment{}.Events())
}
