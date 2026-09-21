package panel

import "time"

// Schedule is one recurring launch as the service listed it.
type Schedule struct {
	ID                string
	Name              string
	Cron              string
	Timezone          string
	Enabled           bool
	HideAgents        bool
	Prompt            string
	WorkflowID        string
	LastResultStatus  string
	LastResultMessage string
	LastResultAt      *time.Time
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
