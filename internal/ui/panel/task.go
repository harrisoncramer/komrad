package panel

import "time"

// Task is one task as the service listed it.
type Task struct {
	ID              string
	WorkflowID      string
	Slug            string
	Title           string
	Goal            string
	Notes           string
	NotesUpdatedAt  *string
	CreatedAt       time.Time
	ArchivedAt      *time.Time
	Cwd             string
	WorktreeRemoved bool
	// FailedRun is whether a script run on the task failed and its logs have not been opened since.
	FailedRun bool
}
