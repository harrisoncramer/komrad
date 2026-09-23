package panel

import (
	"time"

	schedulev1 "weave/proto/gen/schedule/v1"
)

// Schedule is one recurring launch as the service listed it.
type Schedule struct {
	ID         string
	Name       string
	Cron       string
	Timezone   string
	Enabled    bool
	HideAgents bool
	Prompt     string
	WorkflowID string
	LatestRun  *ScheduleRun
}

// ScheduleRun is one firing of a schedule as River's job for it stands.
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
	Name       string
	Cron       string
	Timezone   string
	Enabled    bool
	HideAgents bool
	Prompt     string
	WorkflowID string
}
