package checkouts

import (
	"testing"

	"komrad/internal/notices"

	"github.com/stretchr/testify/assert"
)

// TestRebaseOutcomeNotice pins the notice each rebase outcome starts.
func TestRebaseOutcomeNotice(t *testing.T) {
	tests := []struct {
		name    string
		outcome rebaseOutcome
		want    string
	}{
		{
			name:    "a rebase that failed before it finished",
			outcome: rebaseOutcome{Failure: "the worktree is dirty"},
			want:    notices.WorktreeRebaseFailed,
		},
		{
			name:    "a rebase stopped on conflicts",
			outcome: rebaseOutcome{Result: &RebaseResult{Conflicted: true}},
			want:    notices.WorktreeRebaseConflicted,
		},
		{
			name:    "a branch whose work had all landed",
			outcome: rebaseOutcome{Result: &RebaseResult{RebasePlan: RebasePlan{Landed: 2}, Moved: true}},
			want:    notices.WorktreeLanded,
		},
		{
			name:    "a replay",
			outcome: rebaseOutcome{Result: &RebaseResult{RebasePlan: RebasePlan{Replay: 1}}},
			want:    notices.WorktreeRebased,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.outcome.notice())
		})
	}
}
