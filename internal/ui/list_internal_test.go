package ui

import (
	"testing"

	"komrad/internal/ui/keys"
	"komrad/internal/ui/paint"

	"github.com/stretchr/testify/assert"
)

// TestFitLines covers the chrome coming off the pane and a pane the chrome fills leaving the
// list nothing.
func TestFitLines(t *testing.T) {
	const chrome = 6

	header := "Archive\n\n"
	help := "\nrestore\ndelete\nback\n"

	tests := []struct {
		name   string
		height int
		want   int
	}{
		{
			name:   "an unmeasured pane is unbounded",
			height: 0,
			want:   paint.Unbounded,
		},
		{
			name:   "a pane shorter than its chrome keeps no rows",
			height: chrome - 1,
			want:   0,
		},
		{
			name:   "a pane the chrome fills keeps no rows",
			height: chrome,
			want:   0,
		},
		{
			name:   "a pane one line taller than its chrome keeps that line",
			height: chrome + 1,
			want:   1,
		},
		{
			name:   "the pane less the chrome around the list",
			height: 20,
			want:   20 - chrome,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, fitLines(tt.height, header, help))
		})
	}
}

// TestActionKeysGoThroughTheMenu pins the arms that act straight off their key rather than from the menu.
func TestActionKeysGoThroughTheMenu(t *testing.T) {
	want := map[screen][]keys.ID{
		screenAgents: {
			actionWorkflowRename,
			actionAgentOpen, actionAgentRename, actionTaskRename,
			actionArchiveRename,
		},
		screenSchedules: {actionScheduleRename},
		screenReview: {
			actionReviewBack, actionReviewBack,
		},
	}

	for _, s := range []screen{screenAgents, screenSchedules, screenReview, screenWorktrees, screenDocuments, screenLogs, screenStatusChecks} {
		var direct []keys.ID

		for _, spec := range specsOf(s) {
			for _, arm := range spec.Table {
				assert.NotEqual(t, arm.Nav == nil, arm.Direct == nil, "%s: an arm moves or acts directly, never both or neither", screenNames[s])

				if arm.Direct != nil {
					assert.NotEmpty(t, arm.Direct.Reason, "%s: a direct arm says why", screenNames[s])
					direct = append(direct, arm.ID)
				}
			}
		}

		assert.Equal(t, want[s], direct, screenNames[s])
	}
}
