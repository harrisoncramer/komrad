package panel

import (
	"errors"
	"time"

	schedulev1 "komrad/proto/gen/schedule/v1"
)

// Schedule is one recurring launch as the server listed it.
type Schedule struct {
	ID             string
	Name           string
	Cron           string
	Timezone       string
	Enabled        bool
	HideAgents     bool
	Prompt         string
	WorkflowID     string
	Kind           schedulev1.ScheduleKind
	OrchestratorID string
	LatestRun      *ScheduleRun
	RecentRuns     []ScheduleRun
	NextRun        time.Time
	ArchivedAt     *time.Time
}

// ScheduleRun is one firing of a schedule as the launch job recorded it.
type ScheduleRun struct {
	ID           int64
	ScheduledFor time.Time
	State        schedulev1.ScheduleRunState
	Attempt      int
	MaxAttempts  int
	FinalizedAt  *time.Time
	Message      string
}

// ScheduleConfig is every field of a schedule but its id, which is what a form fills.
type ScheduleConfig struct {
	Name           string
	Cron           string
	Timezone       string
	Enabled        bool
	HideAgents     bool
	Prompt         string
	WorkflowID     string
	Kind           schedulev1.ScheduleKind
	OrchestratorID string
}

// ErrUnknownScheduleKind refuses a schedule whose kind this build does not know.
var ErrUnknownScheduleKind = errors.New("this build of komrad does not know the schedule's kind, so update it first")

var ErrNoWorkflow = errors.New("every agent belongs to a workflow, so create one first")

// IsKnownScheduleKind reports whether this build knows what a schedule of this kind targets.
func IsKnownScheduleKind(kind schedulev1.ScheduleKind) bool {
	switch kind {
	case schedulev1.ScheduleKind_SCHEDULE_KIND_UNSPECIFIED,
		schedulev1.ScheduleKind_SCHEDULE_KIND_TASK,
		schedulev1.ScheduleKind_SCHEDULE_KIND_ORCHESTRATOR:
		return true
	}

	return false
}
