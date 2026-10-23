package schedule

import (
	"fmt"

	"komrad/db/models"
	schedulev1 "komrad/proto/gen/schedule/v1"
)

// fromServiceToDBScheduleKind is the stored value of a schedule kind.
func fromServiceToDBScheduleKind(kind schedulev1.ScheduleKind) (models.ScheduleKind, error) {
	switch kind {
	case schedulev1.ScheduleKind_SCHEDULE_KIND_TASK:
		return models.ScheduleKindTask, nil
	case schedulev1.ScheduleKind_SCHEDULE_KIND_ORCHESTRATOR:
		return models.ScheduleKindOrchestrator, nil
	case schedulev1.ScheduleKind_SCHEDULE_KIND_UNSPECIFIED:
	}

	return "", fmt.Errorf("a schedule kind %v has no stored value", kind)
}

// fromDBToServiceScheduleKind is the schedule kind a stored value names.
func fromDBToServiceScheduleKind(kind models.ScheduleKind) (schedulev1.ScheduleKind, error) {
	switch kind {
	case models.ScheduleKindTask:
		return schedulev1.ScheduleKind_SCHEDULE_KIND_TASK, nil
	case models.ScheduleKindOrchestrator:
		return schedulev1.ScheduleKind_SCHEDULE_KIND_ORCHESTRATOR, nil
	}

	return schedulev1.ScheduleKind_SCHEDULE_KIND_UNSPECIFIED, fmt.Errorf("stored schedule kind %q has no service value", kind)
}

// fromServiceToDBScheduleRunState is the stored text of a schedule run state.
func fromServiceToDBScheduleRunState(state schedulev1.ScheduleRunState) (string, error) {
	switch state {
	case schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_AVAILABLE:
		return "available", nil
	case schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_PENDING:
		return "pending", nil
	case schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_SCHEDULED:
		return "scheduled", nil
	case schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_RUNNING:
		return "running", nil
	case schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_RETRYABLE:
		return "retryable", nil
	case schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_COMPLETED:
		return "completed", nil
	case schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_CANCELLED:
		return "cancelled", nil
	case schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_DISCARDED:
		return "discarded", nil
	case schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_UNSPECIFIED:
	}

	return "", fmt.Errorf("a schedule run state %v has no stored value", state)
}

// fromDBToServiceScheduleRunState is the schedule run state stored text names.
func fromDBToServiceScheduleRunState(state string) (schedulev1.ScheduleRunState, error) {
	switch state {
	case "available":
		return schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_AVAILABLE, nil
	case "pending":
		return schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_PENDING, nil
	case "scheduled":
		return schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_SCHEDULED, nil
	case "running":
		return schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_RUNNING, nil
	case "retryable":
		return schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_RETRYABLE, nil
	case "completed":
		return schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_COMPLETED, nil
	case "cancelled":
		return schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_CANCELLED, nil
	case "discarded":
		return schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_DISCARDED, nil
	}

	return schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_UNSPECIFIED, fmt.Errorf("stored schedule run state %q has no service value", state)
}
