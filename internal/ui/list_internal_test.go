package ui

import (
	"slices"
	"testing"

	"komrad/internal/ui/checks"
	"komrad/internal/ui/documents"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/logs"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/review"
	"komrad/internal/ui/schedules"
	"komrad/internal/ui/screen"
	"komrad/internal/ui/worktrees"
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

// TestActionKeysGoThroughTheMenu counts the arms that act straight off their key rather than from the menu.
func TestActionKeysGoThroughTheMenu(t *testing.T) {
	want := map[screen.ID]int{
		screen.Agents:    5,
		screen.Schedules: 1,
		screen.Review:    3,
	}

	for _, s := range []screen.ID{screen.Agents, screen.Schedules, screen.Review, screen.Worktrees, screen.Documents, screen.Logs, screen.StatusChecks} {
		direct := 0

		for _, arm := range armsOf(t, s) {
			assert.NotEqual(t, arm.moves, arm.acts, "%s: an arm moves or acts directly, never both or neither", s.String())

			if arm.acts {
				assert.NotEmpty(t, arm.reason, "%s: a direct arm says why", s.String())
				direct++
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
		case keys.Spec[*schedules.Screen]:
			arms = append(arms, specArms(spec)...)
		case keys.Spec[schedules.Archive]:
			arms = append(arms, specArms(spec)...)
		case keys.Spec[*review.Screen]:
			arms = append(arms, specArms(spec)...)
		case keys.Spec[*worktrees.Screen]:
			arms = append(arms, specArms(spec)...)
		case keys.Spec[*documents.Screen]:
			arms = append(arms, specArms(spec)...)
		case keys.Spec[*logs.Screen]:
			arms = append(arms, specArms(spec)...)
		case keys.Spec[*checks.Screen]:
			arms = append(arms, specArms(spec)...)
		case keys.Spec[checks.Logs]:
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
