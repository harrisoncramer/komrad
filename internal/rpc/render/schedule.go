package render

import (
	"time"

	"komrad/internal/schedules"
	"komrad/lib/xslice"
	"komrad/lib/xstr"

	schedulestore "komrad/internal/store/schedule"
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
		Id:                  s.ID,
		Name:                s.Name,
		Cron:                s.Cron,
		Timezone:            s.Timezone,
		Enabled:             s.Enabled,
		HideAgents:          s.HideAgents,
		Prompt:              prompt,
		PromptTruncated:     promptTruncated,
		WorkflowId:          s.WorkflowID,
		Kind:                scheduleKinds[s.Kind],
		OrchestratorAgentId: s.OrchestratorAgentID,
	}

	if next := schedules.CalculateNextRun(s, now); !next.IsZero() {
		rendered.NextRunAt = timestamppb.New(next)
	}

	if s.LatestRun != nil {
		rendered.LatestRun = ScheduleRun(*s.LatestRun)
	}

	rendered.RecentRuns = xslice.Map(s.RecentRuns, ScheduleRun)

	return rendered
}

var scheduleKinds = map[schedulestore.Kind]schedulev1.ScheduleKind{
	schedulestore.KindImplementer:  schedulev1.ScheduleKind_SCHEDULE_KIND_IMPLEMENTER,
	schedulestore.KindOrchestrator: schedulev1.ScheduleKind_SCHEDULE_KIND_ORCHESTRATOR,
}

var runStates = map[schedulestore.RunState]schedulev1.ScheduleRunState{
	schedulestore.RunStateRunning:   schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_RUNNING,
	schedulestore.RunStateRetryable: schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_RETRYABLE,
	schedulestore.RunStateCompleted: schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_COMPLETED,
	schedulestore.RunStateCancelled: schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_CANCELLED,
	schedulestore.RunStateDiscarded: schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_DISCARDED,
}

// ScheduleRun draws one firing as the wire carries it.
func ScheduleRun(run schedulestore.Run) *schedulev1.ScheduleRun {
	rendered := &schedulev1.ScheduleRun{
		Id:           run.ID,
		ScheduledFor: timestamppb.New(run.ScheduledFor),
		State:        runStates[run.State],
		Attempt:      int32(run.Attempt),
		MaxAttempts:  int32(run.MaxAttempts),
		Message:      run.Message,
	}

	if run.FinalizedAt != nil {
		rendered.FinalizedAt = timestamppb.New(*run.FinalizedAt)
	}

	return rendered
}
