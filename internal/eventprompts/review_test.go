package eventprompts

import (
	"testing"

	reviewstore "komrad/internal/store/review"
	eventv1 "komrad/proto/gen/event/v1"

	"github.com/stretchr/testify/assert"
)

// TestReleasedReviewStartsTheEventForItsVerdict pins the event each release starts.
func TestReleasedReviewStartsTheEventForItsVerdict(t *testing.T) {
	tests := []struct {
		name string
		mail ReviewReleased
		want eventv1.Event
	}{
		{
			name: "an approval",
			mail: ReviewReleased{Verdict: reviewstore.VerdictApproved},
			want: eventv1.Event_EVENT_REVIEW_APPROVED,
		},
		{
			name: "a request for changes",
			mail: ReviewReleased{Verdict: reviewstore.VerdictChangesRequested},
			want: eventv1.Event_EVENT_REVIEW_CHANGES_REQUESTED,
		},
		{
			name: "a reviewer only commenting",
			mail: ReviewReleased{Verdict: reviewstore.VerdictCommented},
			want: eventv1.Event_EVENT_REVIEW_COMMENTED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, []eventv1.Event{tt.want}, tt.mail.Events())
		})
	}
}

// TestReviewCommentStartsThePersonCommentedEvent pins the event a person's comment starts.
func TestReviewCommentStartsThePersonCommentedEvent(t *testing.T) {
	assert.Equal(t, []eventv1.Event{eventv1.Event_EVENT_REVIEW_COMMENTED_BY_PERSON}, ReviewComment{}.Events())
}
