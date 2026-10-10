package panel

import "time"

// Task is one task as the service listed it.
type Task struct {
	ID         string
	WorkflowID string
	Slug       string
	Title      string
	Goal       string
	CreatedAt  time.Time
	ArchivedAt *time.Time
}
