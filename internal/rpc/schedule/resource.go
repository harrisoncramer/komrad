package schedule

import (
	"time"

	"weave/internal/agents"
	schedulestore "weave/internal/store/schedule"
	schedulev1 "weave/proto/gen/schedule/v1"

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

func renderSchedule(s schedulestore.Schedule, now time.Time) schedule {
	rendered := &schedulev1.Schedule{
		Id:                s.ID,
		Name:              s.Name,
		Cron:              s.Cron,
		Timezone:          s.Timezone,
		Enabled:           s.Enabled,
		HideAgents:        s.HideAgents,
		Prompt:            s.Prompt,
		WorkflowId:        s.WorkflowID,
		LastResultStatus:  string(s.LastResultStatus),
		LastResultMessage: s.LastResultMessage,
	}

	if next := agents.CalculateNextRun(s, now); !next.IsZero() {
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
