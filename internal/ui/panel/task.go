package panel

import (
	"time"

	taskv1 "komrad/proto/gen/task/v1"
)

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
	Branch          *string
	WorktreeRemoved bool
	// DiffStat is the lines its checkout adds and removes since its base, nil where there is none.
	DiffStat *taskv1.DiffStat
	// Hidden is whether a person hid the task from the sidebar.
	Hidden bool
	// FailedRun is whether a script run on the task failed and its logs have not been opened since.
	FailedRun bool
}
