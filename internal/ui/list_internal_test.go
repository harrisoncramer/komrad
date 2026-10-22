package ui

import (
	"slices"
	"testing"

	"komrad/internal/ui/keys"
	"komrad/internal/ui/logs"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/screen"
	"komrad/lib/xslice"

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
	want := map[screen.ID][]keys.ID{
		screen.Agents: {
			actionWorkflowRename,
			actionAgentOpen, actionAgentRename, actionTaskRename,
			actionArchiveRename,
		},
		screen.Schedules: {actionScheduleRename},
		screen.Review: {
			actionReviewBack, actionReviewBack, actionReviewBack,
		},
	}

	for _, s := range []screen.ID{screen.Agents, screen.Schedules, screen.Review, screen.Worktrees, screen.Documents, screen.Logs, screen.StatusChecks} {
		var direct []keys.ID

		for _, arm := range armsOf(t, s) {
			assert.NotEqual(t, arm.moves, arm.acts, "%s: an arm moves or acts directly, never both or neither", s.String())

			if arm.acts {
				assert.NotEmpty(t, arm.reason, "%s: a direct arm says why", s.String())
				direct = append(direct, arm.id)
			}
		}

		assert.Equal(t, want[s], direct, s.String())
	}
}

type specArm struct {
	id     keys.ID
	moves  bool
	acts   bool
	reason string
	named  bool
}

func armsOf(t *testing.T, s screen.ID) []specArm {
	t.Helper()

	var arms []specArm
	for _, table := range actionsOf(s) {
		switch spec := table.(type) {
		case keys.Spec[*model]:
			arms = append(arms, specArms(spec)...)
		case keys.Spec[*agentsScreen]:
			arms = append(arms, specArms(spec)...)
		case keys.Spec[*archivedAgents]:
			arms = append(arms, specArms(spec)...)
		case keys.Spec[*schedulesScreen]:
			arms = append(arms, specArms(spec)...)
		case keys.Spec[*archivedSchedules]:
			arms = append(arms, specArms(spec)...)
		case keys.Spec[*reviewScreen]:
			arms = append(arms, specArms(spec)...)
		case keys.Spec[*worktreesScreen]:
			arms = append(arms, specArms(spec)...)
		case keys.Spec[*documentsScreen]:
			arms = append(arms, specArms(spec)...)
		case keys.Spec[*logs.Screen]:
			arms = append(arms, specArms(spec)...)
		case keys.Spec[*statusChecksScreen]:
			arms = append(arms, specArms(spec)...)
		case keys.Spec[statusCheckLogsScreen]:
			arms = append(arms, specArms(spec)...)
		default:
			t.Fatalf("%s dispatches through a %T no case here reads", s, table)
		}
	}

	return arms
}

func specArms[S any](spec keys.Spec[S]) []specArm {
	return xslice.Map(spec.Table, func(action keys.Action[S]) specArm {
		arm := specArm{
			id:    action.ID,
			moves: action.Nav != nil,
			acts:  action.Direct != nil,
			named: slices.ContainsFunc(spec.Legend, func(row keys.LegendRow[S]) bool { return slices.Contains(row.Of, action.ID) }),
		}
		if action.Direct != nil {
			arm.reason = action.Direct.Reason
		}

		return arm
	})
}
