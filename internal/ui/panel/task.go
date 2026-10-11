package panel

import (
	"time"

	agentv1 "komrad/proto/gen/agent/v1"
	taskv1 "komrad/proto/gen/task/v1"
)

// Task is one task as the server listed it, with the pull requests on its branch and the comments
// on its work.
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
	// Sequence is what git has part-way through its checkout and waiting on somebody.
	Sequence Sequence
	// MovedSinceRound is the newest round's snapshot while the checkout no longer hashes to it.
	MovedSinceRound string
	// DiffStat is the lines its checkout adds and removes since its base, nil where there is none.
	DiffStat *taskv1.DiffStat
	// Hidden is whether a person hid the task from the sidebar.
	Hidden bool
	// FailedRun is whether a script run on the task failed and its logs have not been opened since.
	FailedRun bool
	// Documents is how many documents agents have written for the task.
	Documents int32
	// PullRequest is where the pull request opened from its branch stands.
	PullRequest PullRequestStanding
	// PullRequests is every pull request opened from its branch, the current one first.
	PullRequests []PullRequest
	// ReviewerComments counts the comments reviewer agents left on its work, nil while there are
	// none to show.
	ReviewerComments *agentv1.CommentCounts
	// PersonComments counts the comments the person left on its work, nil while there are none to
	// show.
	PersonComments *agentv1.CommentCounts
}
