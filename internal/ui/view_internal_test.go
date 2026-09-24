package ui

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestSubmittingNoticeCountsUp covers the submitting notice counting once a write is slow.
func TestSubmittingNoticeCountsUp(t *testing.T) {
	since := time.Now()

	tests := []struct {
		name  string
		since time.Time
		now   time.Time
		want  string
	}{
		{
			name:  "a write with no start stands still",
			since: time.Time{},
			now:   since,
			want:  "  submitting…",
		},
		{
			name:  "a quick write stands still",
			since: since,
			now:   since.Add(time.Second),
			want:  "  submitting…",
		},
		{
			name:  "a slow write counts its seconds",
			since: since,
			now:   since.Add(12 * time.Second),
			want:  "  submitting… 12s · esc to keep working",
		},
		{
			name:  "a very slow write counts its minutes",
			since: since,
			now:   since.Add(400 * time.Second),
			want:  "  submitting… 6m40s · esc to keep working",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, submittingNotice(tt.since, tt.now))
		})
	}
}
