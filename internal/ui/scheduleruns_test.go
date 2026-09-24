package ui

import (
	"testing"

	"weave/config"
	"weave/internal/ui/panel"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSchedulesWaitToStage covers the cursor coming to rest on a schedule, which is what stages
// its runs, both when the screen opens and when the cursor moves.
func TestSchedulesWaitToStage(t *testing.T) {
	tests := []struct {
		name   string
		screen screen
		key    string
	}{
		{
			name:   "opening the screen waits to stage",
			screen: screenAgents,
			key:    "S",
		},
		{
			name:   "moving the cursor waits to stage",
			screen: screenSchedules,
			key:    "j",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := scheduleModel(&fakeSchedules{}, config.DefaultKeybindings())
			m.listing.schedules = append(m.listing.schedules, scheduleItem{schedule: panel.Schedule{ID: "s2", Name: "nightly"}})
			m.screen = tt.screen

			updated, cmd := m.routeScreenMessage(keyMsg(tt.key))
			got, ok := updated.(model)
			require.True(t, ok)
			require.NotNil(t, cmd)

			assert.Positive(t, got.staging, "a rest is owed to the cursor")
		})
	}
}
