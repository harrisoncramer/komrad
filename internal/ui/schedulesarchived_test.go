package ui_test

import (
	"slices"
	"testing"
	"time"

	"komrad/internal/ui/paneltest"
	"komrad/internal/ui/screen"
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
		paneltest.Workflow("w1", "backend"),
		newArchivedAgent("a1", "api-refactor", "w1"),
		newSchedule("s1", "nightly-live"),
		newArchivedSchedule("weekly-gone"),
	}

	tests := []struct {
		name       string
		press      []string
		want       []string
		wantAbsent []string
		gone       *watchv1.WatchEvent
		wantSent   []paneltest.Request
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
			wantSent: []paneltest.Request{{
				Procedure: schedulev1connect.ScheduleServiceRestoreScheduleProcedure,
				Msg:       &schedulev1.RestoreScheduleRequest{Id: "s2"},
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
			gone:       paneltest.Deleted(watchv1.Resource_RESOURCE_SCHEDULE, "s2"),
			wantAbsent: []string{deletePrompt},
			wantSent: []paneltest.Request{{
				Procedure: schedulev1connect.ScheduleServiceDeleteScheduleProcedure,
				Msg:       &schedulev1.DeleteScheduleRequest{Id: "s2"},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openArchivedSchedules(t, rows...)

			harness.Press(tt.press...)
			if tt.gone != nil {
				require.Contains(t, harness.Frame(), deletePrompt, "the prompt waits for the schedule to leave")
				harness.Watch(tt.gone)
			}
			harness.Press(widenListPresses...)

			frame := ansi.Strip(harness.Frame())
			assert.Equal(t, screen.Schedules, paneltest.Screen(harness.Frame()))
			for _, want := range tt.want {
				assert.Contains(t, frame, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, frame, absent)
			}
			harness.AssertSent(tt.wantSent...)
		})
	}
}

const (
	archivedAt   = "2026-07-30T09:00:00Z"
	deletePrompt = "Permanently delete"
	deleteTitle  = "Delete "
)

var widenListPresses = slices.Repeat([]string{"ctrl+space", "l"}, 10)

func newArchivedSchedule(name string) *schedulev1.Schedule {
	schedule := newSchedule("s2", name)
	schedule.ArchivedAt = timestamppb.New(time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC))

	return schedule
}

func openArchivedSchedules(t *testing.T, rows ...proto.Message) *paneltest.Panel {
	t.Helper()

	harness := openSchedules(t, rows...)
	harness.Press("F", "tab", "esc")

	return harness
}
