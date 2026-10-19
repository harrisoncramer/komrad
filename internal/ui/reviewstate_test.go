package ui

import (
	"testing"

	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
)

// TestIsReviewHistory covers which ranges end before the round new comments go on.
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
			name: "a range ending on the newest round",
			spec: diffSpec{from: "r1"},
		},
		{
			name: "a range ending on an older round",
			spec: diffSpec{from: "r1", to: "r2"},
			want: true,
		},
		{
			name: "a comment's origin on an older round",
			spec: originSpec(&reviewOrigin{RoundID: "r2"}),
			want: true,
		},
		{
			name: "an answer reaching the newest round",
			spec: answerSpec(&reviewAnswer{ToRound: "r3"}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff := reviewDiff{
				spec:   tt.spec,
				newest: &agentv1.ReviewRound{Id: "r3", Ordinal: 3},
			}

			assert.Equal(t, tt.want, diff.isHistory())
		})
	}
}
