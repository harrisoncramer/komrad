package render

import (
	"time"

	"komrad/internal/schedules"
	schedulestore "komrad/internal/store/schedule"
	"komrad/lib/xslice"
	"komrad/lib/xstr"
	schedulev1 "komrad/proto/gen/schedule/v1"

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
		Id:                  s.ID.String(),
		Name:                s.Name,
		Cron:                s.Cron,
		Timezone:            s.Timezone,
		Enabled:             s.Enabled,
		HideAgents:          s.HideAgents,
		Prompt:              prompt,
		PromptTruncated:     promptTruncated,
		WorkflowId:          s.WorkflowID.String(),
		Kind:                s.Kind,
		OrchestratorAgentId: formatOptionalID(s.OrchestratorAgentID),
	}

	if s.ArchivedAt != nil {
		rendered.ArchivedAt = timestamppb.New(*s.ArchivedAt)
	} else if next := schedules.CalculateNextRun(s, now); !next.IsZero() {
		rendered.NextRunAt = timestamppb.New(next)
	}

	if s.LatestRun != nil {
		rendered.LatestRun = ScheduleRun(*s.LatestRun)
	}

	rendered.RecentRuns = xslice.Map(s.RecentRuns, ScheduleRun)

	return rendered
}

// ScheduleRun draws one firing as the wire carries it.
func ScheduleRun(run schedulestore.Run) *schedulev1.ScheduleRun {
	rendered := &schedulev1.ScheduleRun{
		Id:           run.ID,
		ScheduledFor: timestamppb.New(run.ScheduledFor),
		State:        run.State,
		Attempt:      int32(run.Attempt),
		MaxAttempts:  int32(run.MaxAttempts),
		Message:      run.Message,
	}

	if run.FinalizedAt != nil {
		rendered.FinalizedAt = timestamppb.New(*run.FinalizedAt)
	}

	return rendered
}
