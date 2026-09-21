package schedule

import (
	"time"

	"weave/internal/rpc/render"
	"weave/internal/schedules"
	schedulestore "weave/internal/store/schedule"
	"weave/lib/xstr"
	schedulev1 "weave/proto/gen/schedule/v1"
	viewv1 "weave/proto/gen/view/v1"

	"google.golang.org/protobuf/types/known/timestamppb"
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
func (o *scheduleOptions) SetPromptHead(limit int) { o.promptHead = limit }

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

	prompt, promptTruncated := s.Prompt, false
	if options.promptHead > 0 {
		prompt, promptTruncated = xstr.Head(s.Prompt, options.promptHead)
	}

	rendered := &schedulev1.Schedule{
		Id:                s.ID,
		Name:              s.Name,
		Cron:              s.Cron,
		Timezone:          s.Timezone,
		Enabled:           s.Enabled,
		HideAgents:        s.HideAgents,
		Prompt:            prompt,
		PromptTruncated:   promptTruncated,
		WorkflowId:        s.WorkflowID,
		LastResultStatus:  string(s.LastResultStatus),
		LastResultMessage: s.LastResultMessage,
	}

	if next := schedules.CalculateNextRun(s, now); !next.IsZero() {
		rendered.NextRunAt = timestamppb.New(next)
	}

	if s.LastResultAt != nil {
		rendered.LastResultAt = timestamppb.New(*s.LastResultAt)
	}

	return schedule{
		Schedule: rendered,
		cursor:   schedulestore.Cursor{ID: s.ID},
	}
}
