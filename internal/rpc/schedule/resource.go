package schedule

import (
	"time"

	"komrad/internal/rpc/render"
	schedulestore "komrad/internal/store/schedule"
	schedulev1 "komrad/proto/gen/schedule/v1"
	viewv1 "komrad/proto/gen/view/v1"
)

type schedule struct {
	*schedulev1.Schedule
	cursor schedulestore.Cursor
}

// Cursor returns the keyset cursor locating this schedule.
func (s schedule) Cursor() (schedulestore.Cursor, error) {
	return s.cursor, nil
}

// scheduleViewOptions turns the view a request asked for into rendering options.
func scheduleViewOptions(view viewv1.View) render.ScheduleOptions {
	if view == viewv1.View_VIEW_FULL {
		return render.ScheduleOptions{}
	}

	return render.ScheduleOptions{PromptHead: render.SummaryHead}
}

func renderSchedule(s schedulestore.Schedule, now time.Time, options render.ScheduleOptions) schedule {
	return schedule{
		Schedule: render.Schedule(s, now, options),
		cursor:   schedulestore.Cursor{ID: s.ID},
	}
}
