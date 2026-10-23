package review

import (
	"testing"

	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
)

// TestIsReviewHistory covers which ranges end before the snapshot new comments go on.
func TestIsReviewHistory(t *testing.T) {
	tests := []struct {
		name string
		spec diffSpec
		want bool
	}{
		{
			name: "the whole change",
			spec: wholeChange(),
		},
		{
			name: "a range ending on the newest snapshot",
			spec: diffSpec{from: "r1"},
		},
		{
			name: "a range ending on an older snapshot",
			spec: diffSpec{from: "r1", to: "r2"},
			want: true,
		},
		{
			name: "a comment's origin on an older snapshot",
			spec: originSpec(&commentOrigin{SnapshotID: "r2"}),
			want: true,
		},
		{
			name: "an answer reaching the newest snapshot",
			spec: answerSpec(&commentAnswer{ToSnapshot: "r3"}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff := diffPane{
				spec:   tt.spec,
				newest: &agentv1.ReviewSnapshot{Id: "r3", Ordinal: 3},
			}

			assert.Equal(t, tt.want, diff.isHistory())
		})
	}
}
