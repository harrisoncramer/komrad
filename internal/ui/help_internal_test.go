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
	const wheel = "the mouse wheel presses it, and paging is the only scroll the block names"
	const filterGap = "a gap: the legend has no rows for the filter keys yet"

	tests := []struct {
		screen  screen.ID
		excused map[keys.ID]string
	}{
		{
			screen: screen.Agents,
			excused: map[keys.ID]string{
				keys.Quit:        "quit is not worth a row on any screen",
				actionAgentsBack: "on the root list this is the quit key by another name",
			},
		},
		{
			screen:  screen.Schedules,
			excused: map[keys.ID]string{keys.Quit: "quit is not worth a row on any screen"},
		},
		{
			screen: screen.Review,
			excused: map[keys.ID]string{
				keys.Quit:         "quit is not worth a row on any screen",
				keys.ColumnScroll: wheel,
			},
		},
		{
			screen:  screen.Worktrees,
			excused: map[keys.ID]string{keys.Quit: "quit is not worth a row on any screen"},
		},
		{
			screen: screen.Documents,
			excused: map[keys.ID]string{
				keys.Quit:         "quit is not worth a row on any screen",
				keys.ColumnScroll: wheel,
			},
		},
		{
			screen: screen.Logs,
			excused: map[keys.ID]string{
				keys.Quit:         "quit is not worth a row on any screen",
				keys.ColumnScroll: wheel,
			},
		},
		{
			screen: screen.StatusChecks,
			excused: map[keys.ID]string{
				keys.Quit:        "quit is not worth a row on any screen",
				keys.FilterOpen:  filterGap,
				keys.FilterClear: filterGap,
				keys.FilterMatch: filterGap,
			},
		},
		{
			screen: screen.StatusCheckLogs,
			excused: map[keys.ID]string{
				keys.Quit:        "quit is not worth a row on any screen",
				keys.FilterOpen:  filterGap,
				keys.FilterClear: filterGap,
				keys.FilterMatch: filterGap,
			},
		},
	}

	for _, tt := range tests {
		var unnamed []keys.ID
		for _, spec := range specsOf(tt.screen) {
			for _, action := range spec.Table {
				isNamed := slices.ContainsFunc(spec.Legend, func(row keys.LegendRow[*model]) bool { return slices.Contains(row.Of, action.ID) })
				if !isNamed && !slices.Contains(unnamed, action.ID) {
					unnamed = append(unnamed, action.ID)
				}
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
