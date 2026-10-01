package ui

import (
	"testing"

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
	want := map[screen][]actionID{
		screenAgents: {
			actionWorkflowRename,
			actionAgentOpen, actionAgentRename,
		},
		screenSchedules: {actionScheduleNew, actionScheduleEdit, actionScheduleRename},
		screenArchive:   {actionArchiveRename},
		screenWorktrees: {actionDeleteSelected, actionWorktreesDelete, actionWorktreesClear},
		screenReview: {
			actionReviewBack, actionReviewDismiss, actionReviewEdit, actionReviewResolve, actionReviewViewed,
			actionReviewCompose, actionReviewSend, actionReviewViewed,
		},
	}

	for _, s := range []screen{screenAgents, screenSchedules, screenArchive, screenReview, screenWorktrees, screenDocuments, screenLogs, screenSearch, screenDocumentTypes} {
		var direct []actionID

		for _, table := range actionsOf(s) {
			spec, ok := table.(screenActionSpec)
			if !ok {
				continue
			}

			for _, arm := range spec.table {
				assert.NotEqual(t, arm.nav == nil, arm.direct == nil, "%s: an arm moves or acts directly, never both or neither", screenNames[s])

				if arm.direct != nil {
					assert.NotEmpty(t, arm.direct.reason, "%s: a direct arm says why", screenNames[s])
					direct = append(direct, arm.id)
				}
			}
		}

		assert.Equal(t, want[s], direct, screenNames[s])
	}
}
