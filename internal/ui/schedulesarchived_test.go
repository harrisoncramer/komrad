package ui_test

import (
	"slices"
	"testing"
	"time"

	schedulev1 "komrad/proto/gen/schedule/v1"
	"komrad/proto/gen/schedule/v1/schedulev1connect"
	watchv1 "komrad/proto/gen/watch/v1"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestArchivedSchedules covers the schedules view switched to its archived kind.
func TestArchivedSchedules(t *testing.T) {
	rows := []proto.Message{
		newLiveWorkflow("w1", "backend"),
		newArchivedAgent("a1", "api-refactor", "w1"),
		newSchedule("s1", "nightly-live"),
		newArchivedSchedule("s2", "weekly-gone"),
	}

	tests := []struct {
		name       string
		press      []string
		want       []string
		wantAbsent []string
		gone       *watchv1.WatchEvent
		wantSent   []request
	}{
		{
			name:       "the archived kind lists the archived schedules and none of the live ones",
			want:       []string{"weekly-gone", "archived — kept until deleted"},
			wantAbsent: []string{"nightly-live", "api-refactor", "r rename"},
		},
		{
			name:  "restore puts the hovered schedule back",
			press: []string{"space", "a", "r"},
			want:  []string{"weekly-gone"},
			wantSent: []request{{
				procedure: schedulev1connect.ScheduleServiceRestoreScheduleProcedure,
				msg:       &schedulev1.RestoreScheduleRequest{Id: "s2"},
			}},
		},
		{
			name:  "delete asks first",
			press: []string{"space", "a", "d"},
			want:  []string{`Permanently delete schedule "weekly-gone"?`},
		},
		{
			name:       "declining the delete destroys nothing",
			press:      []string{"space", "a", "d", "n"},
			wantAbsent: []string{deletePrompt},
		},
		{
			name:       "confirming the delete destroys the schedule",
			press:      []string{"space", "a", "d", "y"},
			gone:       buildDeleted(watchv1.Resource_RESOURCE_SCHEDULE, "s2"),
			wantAbsent: []string{deletePrompt},
			wantSent: []request{{
				procedure: schedulev1connect.ScheduleServiceDeleteScheduleProcedure,
				msg:       &schedulev1.DeleteScheduleRequest{Id: "s2"},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openArchivedSchedules(t, rows...)

			harness.press(tt.press...)
			if tt.gone != nil {
				require.Contains(t, harness.readFrame(), deletePrompt, "the prompt waits for the schedule to leave")
				harness.watch(tt.gone)
			}
			harness.press(widenListPresses...)

			frame := ansi.Strip(harness.readFrame())
			assert.Equal(t, schedulesScreen, harness.readScreen())
			for _, want := range tt.want {
				assert.Contains(t, frame, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, frame, absent)
			}
			harness.assertSent(tt.wantSent...)
		})
	}
}

const (
	archivedAt          = "2026-07-30T09:00:00Z"
	agentsScreen        = "the agents list"
	deletePrompt        = "Permanently delete"
	deleteRecordsPrompt = "Deleting the records"
)

var widenListPresses = slices.Repeat([]string{"}"}, 10)

func newArchivedSchedule(id, name string) *schedulev1.Schedule {
	schedule := newSchedule(id, name)
	schedule.ArchivedAt = timestamppb.New(time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC))

	return schedule
}

func openArchivedSchedules(t *testing.T, rows ...proto.Message) *panelHarness {
	t.Helper()

	harness := openSchedules(t, rows...)
	harness.press("F", "tab", "esc")

	return harness
}
