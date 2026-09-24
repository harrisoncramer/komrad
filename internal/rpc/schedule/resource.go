package schedule

import (
	"time"

	"komrade/internal/rpc/render"
	schedulestore "komrade/internal/store/schedule"
	schedulev1 "komrade/proto/gen/schedule/v1"
	viewv1 "komrade/proto/gen/view/v1"
)

type schedule struct {
	*schedulev1.Schedule
	cursor schedulestore.Cursor
}

// Cursor returns the keyset cursor locating this schedule.
func (s schedule) Cursor() (schedulestore.Cursor, error) {
	return s.cursor, nil
}

// scheduleOptions is how much of a schedule is drawn.
type scheduleOptions struct {
	promptHead int
}

type scheduleOption = render.LoadOption[scheduleOptions]

// SetPromptHead sets how much of the prompt is drawn.
func (o *scheduleOptions) SetPromptHead(limit int) {
	o.promptHead = limit
}

// scheduleViewOptions turns the view a request asked for into rendering options.
func scheduleViewOptions(view viewv1.View) []scheduleOption {
	if view == viewv1.View_VIEW_FULL {
		return nil
	}

	return []scheduleOption{render.WithPromptHead[scheduleOptions](render.SummaryHead)}
}

func renderSchedule(s schedulestore.Schedule, now time.Time, opts ...scheduleOption) schedule {
	options := scheduleOptions{}
	for _, opt := range opts {
		opt(&options)
	}

	return schedule{
		Schedule: render.Schedule(s, now, render.ScheduleOptions{PromptHead: options.promptHead}),
		cursor:   schedulestore.Cursor{ID: s.ID},
	}
}
