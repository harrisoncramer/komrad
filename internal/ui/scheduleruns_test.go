package ui

import (
	"testing"

	"weave/config"
	"weave/internal/ui/panel"
	schedulev1 "weave/proto/gen/schedule/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadScheduleRuns covers the hovered schedule's runs being asked for once, again when its
// newest firing moves, and an answer the cursor has left behind being dropped.
func TestReadScheduleRuns(t *testing.T) {
	tests := []struct {
		name      string
		act       func(t *testing.T, m *model)
		wantReads int
		wantRunID int64
	}{
		{
			name: "another screen asks for nothing",
			act: func(t *testing.T, m *model) {
				m.screen = screenAgents
				assert.Nil(t, m.readScheduleRunsCmd())
			},
		},
		{
			name: "an answer for a schedule the cursor has left is dropped",
			act: func(t *testing.T, m *model) {
				answer := runScheduleRunsCmd(t, m)
				m.scheduleRuns = scheduleRunsRead{scheduleID: "s2", pending: true}
				m.applyScheduleRunsRead(answer)
			},
			wantReads: 1,
		},
		{
			name: "asking again with nothing moved asks nothing",
			act: func(t *testing.T, m *model) {
				m.applyScheduleRunsRead(runScheduleRunsCmd(t, m))
				assert.Nil(t, m.readScheduleRunsCmd())
			},
			wantReads: 1,
			wantRunID: 1,
		},
		{
			name: "a newer firing on the listing asks again",
			act: func(t *testing.T, m *model) {
				m.applyScheduleRunsRead(runScheduleRunsCmd(t, m))
				m.listing.schedules[0].schedule.LatestRun = &panel.ScheduleRun{
					ID:    9,
					State: schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_CANCELLED,
				}
				m.applyScheduleRunsRead(runScheduleRunsCmd(t, m))
			},
			wantReads: 2,
			wantRunID: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeSchedules{}
			m := scheduleModel(client, config.DefaultKeybindings())

			tt.act(t, &m)

			assert.Equal(t, tt.wantReads, client.runReads)
			if tt.wantRunID == 0 {
				assert.Empty(t, m.scheduleRuns.runs, "nothing answered for the hovered schedule stands")

				return
			}

			require.Len(t, m.scheduleRuns.runs, 1)
			assert.Equal(t, tt.wantRunID, m.scheduleRuns.runs[0].ID)
		})
	}
}

func runScheduleRunsCmd(t *testing.T, m *model) scheduleRunsReadMsg {
	t.Helper()

	cmd := m.readScheduleRunsCmd()
	require.NotNil(t, cmd)

	msg, ok := cmd().(scheduleRunsReadMsg)
	require.True(t, ok)

	return msg
}

// TestSchedulesWaitToStage covers the cursor coming to rest on a schedule, which is what asks for
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
