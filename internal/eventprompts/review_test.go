package eventprompts

import (
	"testing"

	agentv1 "komrad/proto/gen/agent/v1"
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
			mail: ReviewReleased{Verdict: agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED},
			want: eventv1.Event_EVENT_REVIEW_APPROVED,
		},
		{
			name: "a request for changes",
			mail: ReviewReleased{Verdict: agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED},
			want: eventv1.Event_EVENT_REVIEW_CHANGES_REQUESTED,
		},
		{
			name: "a reviewer only commenting",
			mail: ReviewReleased{Verdict: agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED},
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
