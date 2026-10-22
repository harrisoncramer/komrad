package ui

import (
	"slices"
	"testing"

	"komrad/internal/ui/keys"
	"komrad/internal/ui/screen"

	"github.com/stretchr/testify/assert"
)

// TestEveryActionHasALegendRow covers each table a screen dispatches through naming every action it
// answers in its own legend.
func TestEveryActionHasALegendRow(t *testing.T) {
	const quit = "quit is not worth a row on any screen"
	const wheel = "the mouse wheel presses it, and paging is the only scroll the block names"

	tests := []struct {
		screen  screen.ID
		excused map[keys.ID]string
	}{
		{
			screen: screen.Agents,
			excused: map[keys.ID]string{
				keys.Quit:        quit,
				actionAgentsBack: "on the root list this is the quit key by another name",
			},
		},
		{
			screen:  screen.Schedules,
			excused: map[keys.ID]string{keys.Quit: quit},
		},
		{
			screen: screen.Review,
			excused: map[keys.ID]string{
				keys.Quit:         quit,
				keys.ColumnScroll: wheel,
			},
		},
		{
			screen:  screen.Worktrees,
			excused: map[keys.ID]string{keys.Quit: quit},
		},
		{
			screen: screen.Documents,
			excused: map[keys.ID]string{
				keys.Quit:         quit,
				keys.ColumnScroll: wheel,
			},
		},
		{
			screen: screen.Logs,
			excused: map[keys.ID]string{
				keys.Quit:         quit,
				keys.ColumnScroll: wheel,
			},
		},
		{
			screen:  screen.StatusChecks,
			excused: map[keys.ID]string{keys.Quit: quit},
		},
		{
			screen:  screen.StatusCheckLogs,
			excused: map[keys.ID]string{keys.Quit: quit},
		},
	}

	for _, tt := range tests {
		var unnamed []keys.ID
		for _, arm := range armsOf(t, tt.screen) {
			if !arm.named && !slices.Contains(unnamed, arm.id) {
				unnamed = append(unnamed, arm.id)
			}
		}

		t.Run(tt.screen.String()+"'s legend names every action it answers", func(t *testing.T) {
			for _, id := range unnamed {
				_, isExcused := tt.excused[id]
				assert.True(t, isExcused, "%s answers action %d and its legend does not name it", tt.screen, id)
			}
		})

		t.Run("every action "+tt.screen.String()+"'s legend is excused from is still unnamed", func(t *testing.T) {
			for id, reason := range tt.excused {
				assert.Contains(t, unnamed, id, "%s names or no longer answers action %d, so its excuse can go: %s", tt.screen, id, reason)
			}
		})
	}
}
