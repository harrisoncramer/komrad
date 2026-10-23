package panel

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	taskv1 "komrad/proto/gen/task/v1"
)

// Task is one task as the server listed it, with the pull requests on its branch and the comments
// on its work.
type Task struct {
	ID             string
	WorkflowID     string
	Slug           string
	Title          string
	Goal           string
	Notes          string
	NotesUpdatedAt *time.Time
	CreatedAt      time.Time
	ArchivedAt     *time.Time
	Cwd            string
	Branch         *string
	// WorktreeRemovalReason is why its worktree is no longer on disk, empty while it is.
	WorktreeRemovalReason WorktreeRemovalReason
	// Sequence is what git has part-way through its checkout and waiting on somebody.
	Sequence Sequence
	// MovedSinceSnapshot is the newest snapshot's commit while the checkout no longer hashes to it.
	MovedSinceSnapshot string
	// DiffStat is the lines its checkout adds and removes since its base, nil where there is none.
	DiffStat *taskv1.DiffStat
	// Parent is the task this one depends on, nil for a task on the workflow's base branch.
	Parent *taskv1.Parent
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
	// Snapshots is how many snapshots komrad has taken of its checkout.
	Snapshots int32
}

// WorktreeRemovalReason is why a task's worktree is no longer on disk.
type WorktreeRemovalReason string

const (
	// worktreeOnDisk is a worktree still on disk, or a task without one.
	worktreeOnDisk WorktreeRemovalReason = ""
	// WorktreeDeleted is a worktree a delete removed.
	WorktreeDeleted WorktreeRemovalReason = "deleted"
	// WorktreeGone is a worktree komrad found gone from disk without a delete.
	WorktreeGone WorktreeRemovalReason = "gone"
)

// IsWorktreeRemoved reports whether the task's worktree is no longer on disk.
func (t Task) IsWorktreeRemoved() bool {
	return t.WorktreeRemovalReason != worktreeOnDisk
}

// Tasks is the panel's tasks keyed by id.
type Tasks map[string]Task

// FindForAgent is the agent's task, or one spelled from the agent when none has its id.
func (t Tasks) FindForAgent(agent Agent) Task {
	if task, ok := t[agent.TaskID]; ok {
		return task
	}

	return Task{
		ID:         agent.TaskID,
		WorkflowID: agent.WorkflowID,
	}
}

// IsHidden reports whether the agent works on a hidden task.
func (t Tasks) IsHidden(agent Agent) bool {
	return t[agent.TaskID].Hidden
}

// PossibleParents is the workflow's live tasks with a branch a new task can depend on, by slug.
func (t Tasks) PossibleParents(workflowID string) []Task {
	var tasks []Task
	for _, task := range t {
		if task.WorkflowID == workflowID && task.ArchivedAt == nil && task.Branch != nil && !task.IsWorktreeRemoved() {
			tasks = append(tasks, task)
		}
	}
	slices.SortFunc(tasks, func(a, b Task) int { return strings.Compare(a.Slug, b.Slug) })

	return tasks
}

// AncestorsOf is a task's parent, that task's parent and so on, the bottom of the stack first.
func (t Tasks) AncestorsOf(task Task) []Task {
	var ancestors []Task
	seen := map[string]bool{task.ID: true}
	for id := task.Parent.GetTaskId(); id != "" && !seen[id]; {
		parent, ok := t[id]
		if !ok {
			break
		}

		seen[id] = true
		ancestors = append(ancestors, parent)
		id = parent.Parent.GetTaskId()
	}

	slices.Reverse(ancestors)

	return ancestors
}

// ChildrenOf is a task's live children, oldest first.
func (t Tasks) ChildrenOf(id string) []Task {
	var children []Task
	for _, task := range t {
		if task.Parent.GetTaskId() == id && task.ArchivedAt == nil {
			children = append(children, task)
		}
	}

	slices.SortFunc(children, func(a, b Task) int { return a.CreatedAt.Compare(b.CreatedAt) })

	return children
}

// TaskIDs is the id of each task, in the order the caller gave them.
func TaskIDs(tasks []Task) []string {
	return xslice.Map(tasks, func(task Task) string { return task.ID })
}

// TaskDirs is the directories of the tasks that have one still on disk.
func TaskDirs(tasks []Task) []string {
	return xslice.Map(xslice.Filter(tasks, func(task Task) bool { return task.Cwd != "" && !task.IsWorktreeRemoved() }), func(task Task) string { return task.Cwd })
}

// DescribeTasks names the tasks the way a message about them does.
func DescribeTasks(tasks []Task) string {
	if len(tasks) == 1 {
		return fmt.Sprintf("task %q", tasks[0].Slug)
	}

	return fmt.Sprintf("%d tasks", len(tasks))
}

// CascadeBlockSaid is what blocks a child's cascade on its parent in words, empty while nothing
// blocks it.
func (t Tasks) CascadeBlockSaid(parent *taskv1.Parent) string {
	slug := t[parent.GetTaskId()].Slug
	switch parent.GetBlocked() {
	case taskv1.CascadeBlock_CASCADE_BLOCK_CONFLICTED:
		return "behind " + slug + ", conflicts"
	case taskv1.CascadeBlock_CASCADE_BLOCK_ORPHANED:
		return "orphaned from " + slug
	case taskv1.CascadeBlock_CASCADE_BLOCK_PARENT_CLOSED:
		return "on " + slug + ", closed unmerged"
	case taskv1.CascadeBlock_CASCADE_BLOCK_UNSPECIFIED:
	}

	return ""
}
