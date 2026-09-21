package render

import (
	"time"

	"weave/internal/schedules"
	"weave/lib/xstr"

	schedulestore "weave/internal/store/schedule"
	schedulev1 "weave/proto/gen/schedule/v1"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// ScheduleOptions is how much of a schedule is drawn.
type ScheduleOptions struct {
	// PromptHead draws the opening of the prompt rather than the whole of it when set.
	PromptHead int
}

// Schedule draws one schedule as the wire carries it, its next run worked out from now.
func Schedule(s schedulestore.Schedule, now time.Time, options ScheduleOptions) *schedulev1.Schedule {
	prompt, promptTruncated := s.Prompt, false
	if options.PromptHead > 0 {
		prompt, promptTruncated = xstr.Head(s.Prompt, options.PromptHead)
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

	return rendered
}
