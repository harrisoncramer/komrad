package reviews

import (
	"testing"

	agentstore "komrade/internal/store/agent"
	reviewstore "komrade/internal/store/review"

	"github.com/stretchr/testify/assert"
)

// TestRenderReviewHeadsEachThread pins which side of the diff a thread's heading counts its lines on.
func TestRenderReviewHeadsEachThread(t *testing.T) {
	tests := []struct {
		name        string
		thread      reviewstore.Thread
		wantHeading string
		wantPair    string
	}{
		{
			name: "a line removed since an earlier round is counted in that round's snapshot",
			thread: reviewstore.Thread{
				BaseSHA:  "earlier-snapshot-sha",
				FilePath: "greet.go",
				OldStart: 41,
				OldEnd:   42,
			},
			wantHeading: "## greet.go:41-42 (removed)",
			wantPair:    "`git diff earlier-snapshot-sha snapshot-sha`",
		},
		{
			name: "a line removed since the round's own base is counted there",
			thread: reviewstore.Thread{
				BaseSHA:  "base-sha",
				FilePath: "greet.go",
				OldStart: 41,
				OldEnd:   41,
			},
			wantHeading: "## greet.go:41 (removed)",
			wantPair:    "`git diff base-sha snapshot-sha`",
		},
		{
			name: "a removed line on a thread that recorded no base is counted in the round's",
			thread: reviewstore.Thread{
				FilePath: "greet.go",
				OldStart: 41,
				OldEnd:   41,
			},
			wantHeading: "## greet.go:41 (removed)",
			wantPair:    "`git diff base-sha snapshot-sha`",
		},
		{
			name: "a line the snapshot holds is counted on the new side whatever base was recorded",
			thread: reviewstore.Thread{
				BaseSHA:  "earlier-snapshot-sha",
				FilePath: "greet.go",
				OldStart: 40,
				OldEnd:   40,
				NewStart: 41,
				NewEnd:   41,
			},
			wantHeading: "## greet.go:41",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := renderReview(renderReviewParams{
				Agent: agentstore.Agent{
					ID:   "01a0d224-0000-7000-8000-000000000001",
					Name: "worker",
				},
				Round: reviewstore.Round{
					Ordinal:     1,
					BaseSHA:     "base-sha",
					SnapshotSHA: "snapshot-sha",
				},
				Verdict: reviewstore.VerdictChangesRequested,
				Threads: []ReviewThreadInContext{{Thread: tt.thread}},
			})

			assert.Contains(t, body, tt.wantHeading+"\n")
			if tt.wantPair == "" {
				assert.NotContains(t, body, "on the old side")

				return
			}

			assert.Contains(t, body, "on the old side")
			assert.Contains(t, body, tt.wantPair)
		})
	}
}
