package reviews

import (
	"testing"

	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"

	"github.com/stretchr/testify/assert"
)

// TestAReviewSaysWhereARemovedLineWasCounted pins that a comment on a deleted line names the commit
// its numbers were read against.
func TestAReviewSaysWhereARemovedLineWasCounted(t *testing.T) {
	tests := []struct {
		name        string
		thread      reviewstore.Thread
		wantHeading string
		wantPair    string
	}{
		{
			name: "a line removed since an earlier round is counted in that round's snapshot",
			thread: reviewstore.Thread{
				ID:       reviewThreadID,
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
				ID:       reviewThreadID,
				BaseSHA:  "base-sha",
				FilePath: "greet.go",
				OldStart: 41,
				OldEnd:   41,
			},
			wantHeading: "## greet.go:41 (removed)",
			wantPair:    "`git diff base-sha snapshot-sha`",
		},
		{
			name: "a thread that recorded no base was counted in the round's",
			thread: reviewstore.Thread{
				ID:       reviewThreadID,
				FilePath: "greet.go",
				OldStart: 41,
				OldEnd:   41,
			},
			wantHeading: "## greet.go:41 (removed)",
			wantPair:    "`git diff base-sha snapshot-sha`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := renderReview(renderReviewParams{
				Agent: agentstore.Agent{ID: reviewedAgentID, Name: "worker"},
				Round: reviewstore.Round{
					Ordinal:     1,
					BaseSHA:     "base-sha",
					SnapshotSHA: "snapshot-sha",
				},
				Verdict: reviewstore.VerdictChangesRequested,
				Threads: []ReviewThreadWithMessages{{Thread: tt.thread}},
			})

			assert.Contains(t, body, tt.wantHeading+"\n", "the heading says which side the numbers are on")
			assert.Contains(t, body, "on the old side", "and the thread says so in words")
			assert.Contains(t, body, tt.wantPair, "and names the pair those numbers were read against")
		})
	}
}

// TestAReviewLeavesALineOnTheNewSideAlone pins that a comment on a line the snapshot holds is
// headed by its new-side number, whatever base the thread recorded.
func TestAReviewLeavesALineOnTheNewSideAlone(t *testing.T) {
	body := renderReview(renderReviewParams{
		Agent: agentstore.Agent{ID: reviewedAgentID, Name: "worker"},
		Round: reviewstore.Round{
			Ordinal:     1,
			BaseSHA:     "base-sha",
			SnapshotSHA: "snapshot-sha",
		},
		Verdict: reviewstore.VerdictChangesRequested,
		Threads: []ReviewThreadWithMessages{{Thread: reviewstore.Thread{
			ID:       reviewThreadID,
			BaseSHA:  "earlier-snapshot-sha",
			FilePath: "greet.go",
			OldStart: 40,
			OldEnd:   40,
			NewStart: 41,
			NewEnd:   41,
		}}},
	})

	assert.Contains(t, body, "## greet.go:41\n")
	assert.NotContains(t, body, "(removed)")
	assert.NotContains(t, body, "on the old side")
}
