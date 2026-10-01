package checkouts

import (
	"testing"

	"komrad/internal/eventprompts"

	"github.com/stretchr/testify/assert"
)

// TestRebaseOutcomeEvent pins the event each rebase outcome starts.
func TestRebaseOutcomeEvent(t *testing.T) {
	tests := []struct {
		name    string
		outcome rebaseOutcome
		want    string
	}{
		{
			name:    "a rebase that failed before it finished",
			outcome: rebaseOutcome{Failure: "the worktree is dirty"},
			want:    eventprompts.WorktreeRebaseFailed,
		},
		{
			name:    "a rebase stopped on conflicts",
			outcome: rebaseOutcome{Result: &RebaseResult{Conflicted: true}},
			want:    eventprompts.WorktreeRebaseConflicted,
		},
		{
			name:    "a branch whose work had all landed",
			outcome: rebaseOutcome{Result: &RebaseResult{RebasePlan: RebasePlan{Landed: 2}, Moved: true}},
			want:    eventprompts.WorktreeLanded,
		},
		{
			name:    "a replay",
			outcome: rebaseOutcome{Result: &RebaseResult{RebasePlan: RebasePlan{Replay: 1}}},
			want:    eventprompts.WorktreeRebased,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.outcome.event())
		})
	}
}
